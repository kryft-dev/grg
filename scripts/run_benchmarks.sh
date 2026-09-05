#!/usr/bin/env bash
# ==============================================================================
# scripts/run_benchmarks.sh
# Production Benchmark Automation Suite for grg
# Fault-Tolerant, Multi-Scenario with Graceful Error & State Recovery
# ==============================================================================

set -uo pipefail

# Color codes (ANSI-C quoted so they hold real escape bytes and render in heredocs too)
if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
  RED=$'\033[0;31m'
  GREEN=$'\033[0;32m'
  YELLOW=$'\033[1;33m'
  BLUE=$'\033[1;34m'
  CYAN=$'\033[0;36m'
  BOLD=$'\033[1m'
  NC=$'\033[0m' # No Color
else
  RED='' GREEN='' YELLOW='' BLUE='' CYAN='' BOLD='' NC=''
fi

# Configuration defaults
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUTPUT_FILE="${REPO_ROOT}/benchmark_results.txt"
BENCH_DIR="${REPO_ROOT}/_bench_repos"
BIN_DIR="${REPO_ROOT}/bin"
GRG_BIN="${BIN_DIR}/grg"

SKIP_CLONE=false
SKIP_SYNTHETIC=false
SKIP_REAL=false
SKIP_LINUX=false
AUTO_CONFIRM=false
RUN_BASELINE=true

# hyperfine run counts. Small/medium repos get enough runs for a stable mean;
# linux is capped because a single run can take minutes.
SMALL_MIN_RUNS=10
LARGE_MIN_RUNS=3
LARGE_MAX_RUNS=5
WARMUP_RUNS=1

# Metrics tracking
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# Rows for the generated README table, filled in as scenarios complete
declare -a README_ROWS=()

log()   { echo -e "${BLUE}[BENCHMARK]${NC} $*"; }
info()  { echo -e "${CYAN}[INFO]${NC} $*"; }
pass()  { echo -e "${GREEN}[PASS]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }

# Parse flags
while [[ $# -gt 0 ]]; do
  case "$1" in
    -y|--yes)
      AUTO_CONFIRM=true
      shift
      ;;
    --out-file)
      OUTPUT_FILE="$2"
      shift 2
      ;;
    --bench-dir)
      BENCH_DIR="$2"
      shift 2
      ;;
    --skip-clone)
      SKIP_CLONE=true
      shift
      ;;
    --skip-synthetic)
      SKIP_SYNTHETIC=true
      shift
      ;;
    --skip-real)
      SKIP_REAL=true
      shift
      ;;
    --skip-linux)
      SKIP_LINUX=true
      shift
      ;;
    --no-baseline)
      RUN_BASELINE=false
      shift
      ;;
    -h|--help)
      cat <<HELP
Usage: $0 [OPTIONS]

Options:
  -y, --yes           Bypass confirmation prompt (useful for non-interactive/automated runs)
  --out-file PATH     Output benchmark results file (default: ./benchmark_results.txt)
  --bench-dir PATH    Directory to clone real-world repositories (default: ./_bench_repos)
  --skip-clone        Reuse existing cloned repositories without repacking or re-cloning
  --skip-synthetic    Skip Go synthetic microbenchmarks
  --skip-real         Skip real-world repository benchmarks
  --skip-linux        Skip cloning and benchmarking torvalds/linux (~4GB)
  --no-baseline       Do not time the 'git log -p | grep' baseline alongside grg
  -h, --help          Show this help message

Requirements: go, git, jq, hyperfine (https://github.com/sharkdp/hyperfine)

The baseline comparison runs on ripgrep and go only. On linux a single
'git log -p' pass takes far too long to repeat under hyperfine.
HELP
      exit 0
      ;;
    *)
      error "Unknown option: $1"
      exit 1
      ;;
  esac
done

# Signal handling: INT/TERM convert to an exit status, EXIT does the cleanup
# exactly once (a combined trap would run cleanup twice on Ctrl-C).
cleanup() {
  local exit_code=$?
  rm -f "${BENCH_DIR}"/temp_*.md 2>/dev/null || true
  if [[ $exit_code -ne 0 ]]; then
    echo -e "\n${RED}[ABORT] Benchmark script interrupted or exited with error code ${exit_code}.${NC}" >&2
  fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# ------------------------------------------------------------------------------
# PREREQUISITES
# ------------------------------------------------------------------------------
# Checked before the banner so a missing tool fails fast instead of after a
# multi-gigabyte clone. Nothing is installed on the user's behalf.
MISSING_TOOLS=()
command -v go        >/dev/null 2>&1 || MISSING_TOOLS+=("go        (https://go.dev/dl/)")
command -v git       >/dev/null 2>&1 || MISSING_TOOLS+=("git")
if [[ "${SKIP_REAL}" == false ]]; then
  command -v jq        >/dev/null 2>&1 || MISSING_TOOLS+=("jq        (apt install jq | brew install jq)")
  command -v hyperfine >/dev/null 2>&1 || MISSING_TOOLS+=("hyperfine (apt install hyperfine | brew install hyperfine | cargo install hyperfine | https://github.com/sharkdp/hyperfine/releases)")
fi
if [[ ${#MISSING_TOOLS[@]} -gt 0 ]]; then
  error "Missing required tools:"
  for t in "${MISSING_TOOLS[@]}"; do echo "  - ${t}" >&2; done
  exit 1
fi

# ------------------------------------------------------------------------------
# MASSIVE PRE-RUN WARNING & DISK SPACE AUDIT
# ------------------------------------------------------------------------------
show_warning_banner() {
  cat <<BANNER

${YELLOW}${BOLD}################################################################################
#                                                                              #
#                      ⚠️   MASSIVE BENCHMARK SUITE WARNING   ⚠️                 #
#                                                                              #
################################################################################${NC}
${BOLD}This script will download and benchmark real-world Git repositories:${NC}
  • ${BOLD}BurntSushi/ripgrep${NC}  ~15 MB   (~3,000 commits)
  • ${BOLD}golang/go${NC}           ~400 MB  (~60,000 commits)
BANNER

  if [[ "${SKIP_LINUX}" == false ]]; then
    cat <<BANNER
  • ${BOLD}torvalds/linux${NC}      ${RED}${BOLD}~4.0 GB  (~1,200,000 commits)${NC}
BANNER
  else
    echo -e "  • ${YELLOW}torvalds/linux${NC}      [SKIPPED via --skip-linux]"
  fi

  cat <<BANNER

${BOLD}Resource Requirements:${NC}
  • Total Download Size : ${RED}${BOLD}~4.5 GB of Git Packfiles${NC}
  • Minimum Free Disk   : ${BOLD}12 GB recommended${NC}
  • Estimated Runtime   : ${BOLD}10 to 40 minutes${NC} (depending on network and disk speed)
  • Bench Directory     : ${BOLD}${BENCH_DIR}${NC}
${YELLOW}################################################################################${NC}

BANNER

  # Check available disk space where the clones will actually land
  mkdir -p "${BENCH_DIR}"
  local available_kb
  available_kb=$(df -k "${BENCH_DIR}" | awk 'NR==2 {print $4}')
  local available_gb=$(( available_kb / 1024 / 1024 ))

  info "Available disk space in bench directory: ${BOLD}${available_gb} GB${NC}"

  if [[ ${available_gb} -lt 8 && "${SKIP_LINUX}" == false ]]; then
    error "Less than 8 GB of free disk space remaining (${available_gb} GB detected)."
    error "Cloning torvalds/linux may cause disk exhaustion."
    warn "Consider running with --skip-linux to benchmark ripgrep and go only."
  fi

  if [[ "${AUTO_CONFIRM}" == false ]]; then
    echo -e "${CYAN}Do you want to proceed with cloning and benchmarking?${NC}"
    read -r -p "Enter 'y' to continue or 'n' to cancel [y/N]: " response
    case "${response}" in
      [yY][eE][sS]|[yY])
        info "Confirmation received. Proceeding with benchmarks..."
        ;;
      *)
        log "Execution cancelled by user."
        exit 0
        ;;
    esac
  else
    info "Auto-confirm enabled (-y/--yes). Proceeding with benchmarks..."
  fi
}

if [[ "${SKIP_REAL}" == false ]]; then
  show_warning_banner
fi

# ------------------------------------------------------------------------------
# INITIALIZE OUTPUT FILE & HELPERS
# ------------------------------------------------------------------------------
mkdir -p "$(dirname "${OUTPUT_FILE}")"
rm -f "${OUTPUT_FILE}"
touch "${OUTPUT_FILE}"

append_output() {
  echo "$@" >> "${OUTPUT_FILE}"
}

# 1234567 -> 1,234,567
with_commas() {
  echo "$1" | sed ':a;s/\B[0-9]\{3\}\>/,&/;ta'
}

# seconds (float) -> "123 ms" ; "N/A" if empty/null
fmt_ms() {
  local secs="$1"
  if [[ -z "${secs}" || "${secs}" == "null" ]]; then
    echo "N/A"
  else
    awk -v s="${secs}" 'BEGIN { printf "%.0f ms", s * 1000 }'
  fi
}

# ------------------------------------------------------------------------------
# 1. HARDWARE ENVIRONMENT & MACHINE SPECIFICATIONS
# ------------------------------------------------------------------------------
log "Collecting hardware environment and machine specifications..."

CPU_MODEL=$(lscpu 2>/dev/null | grep "Model name:" | sed 's/Model name:[ \t]*//' || grep -m1 "model name" /proc/cpuinfo 2>/dev/null | cut -d: -f2 | xargs || echo "Generic x86_64 CPU")
CPU_CORES=$(nproc 2>/dev/null || echo "4")
TOTAL_MEM=$(free -h 2>/dev/null | awk '/^Mem:/ {print $2}' || echo "N/A")
OS_INFO=$(uname -s -r -m 2>/dev/null || echo "Linux")
if [[ -n "${CODESPACES:-}" ]]; then
  PLATFORM_LABEL="GitHub Codespace"
elif [[ -n "${CI:-}" ]]; then
  PLATFORM_LABEL="CI runner"
else
  PLATFORM_LABEL="Local machine"
fi

log "Detected: ${CPU_MODEL} (${CPU_CORES} cores), ${TOTAL_MEM} RAM, ${OS_INFO}"

append_output "=============================================================================="
append_output "grg PRODUCTION BENCHMARK REPORT"
append_output "Generated at: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
append_output "=============================================================================="
append_output "System Environment:"
append_output "  - Platform:     ${PLATFORM_LABEL}"
append_output "  - CPU Model:    ${CPU_MODEL}"
append_output "  - CPU Cores:    ${CPU_CORES} logical cores"
append_output "  - Memory:       ${TOTAL_MEM}"
append_output "  - OS:           ${OS_INFO}"
append_output "=============================================================================="
append_output ""

# ------------------------------------------------------------------------------
# 2. COMPILER & TOOL VERSIONS
# ------------------------------------------------------------------------------
GO_VERSION=$(go version 2>&1 || echo "Go version unknown")
log "Go Version: ${GO_VERSION}"
append_output "Compiler: ${GO_VERSION}"

if [[ "${SKIP_REAL}" == false ]]; then
  HYPERFINE_VERSION=$(hyperfine --version 2>&1 | head -1)
  log "Timing tool: ${HYPERFINE_VERSION}"
  append_output "Timing tool: ${HYPERFINE_VERSION}"
fi

# ------------------------------------------------------------------------------
# 3. BUILD OPTIMIZED grg BINARY
# ------------------------------------------------------------------------------
log "Compiling grg release binary..."
mkdir -p "${BIN_DIR}"
cd "${REPO_ROOT}"

GIT_REV=$(git rev-parse --short HEAD 2>/dev/null || echo "release")
if ! go build -ldflags="-s -w -X github.com/kryft-dev/grg/internal/cli.AppVersion=bench-${GIT_REV}" -o "${GRG_BIN}" ./cmd/grg 2>&1; then
  error "Failed to build grg binary. Aborting benchmarks."
  append_output "FATAL: go build failed."
  exit 1
fi
chmod +x "${GRG_BIN}"

GRG_VER=$("${GRG_BIN}" --version 2>&1 || echo "grg bench")
pass "Successfully built: ${GRG_VER}"
append_output "Binary Under Test: ${GRG_VER}"
append_output ""

# ------------------------------------------------------------------------------
# 4. SYNTHETIC GO MICROBENCHMARKS
# ------------------------------------------------------------------------------
if [[ "${SKIP_SYNTHETIC}" == false ]]; then
  log "Executing synthetic Go microbenchmarks (test/benchmark/...)..."
  append_output "------------------------------------------------------------------------------"
  append_output "SECTION 1: SYNTHETIC GO MICROBENCHMARKS (test/benchmark/search_bench_test.go)"
  append_output "------------------------------------------------------------------------------"

  TOTAL_TESTS=$((TOTAL_TESTS + 1))
  cd "${REPO_ROOT}"

  if GO_BENCH_OUTPUT=$(go test -run=^$ -bench=. -benchmem -count=3 ./test/benchmark/... 2>&1); then
    pass "Synthetic benchmarks completed successfully."
    PASSED_TESTS=$((PASSED_TESTS + 1))
    echo "${GO_BENCH_OUTPUT}" | tee -a "${OUTPUT_FILE}"
  else
    error "Synthetic benchmarks encountered an error."
    FAILED_TESTS=$((FAILED_TESTS + 1))
    echo "${GO_BENCH_OUTPUT}" | tee -a "${OUTPUT_FILE}"
    append_output "STATUS: FAILED"
  fi
  append_output ""
else
  info "Skipping synthetic benchmarks (--skip-synthetic)."
  append_output "SECTION 1: SYNTHETIC BENCHMARKS SKIPPED"
  append_output ""
fi

# ------------------------------------------------------------------------------
# 5. REAL-WORLD REPOSITORY BENCHMARKS
# ------------------------------------------------------------------------------
if [[ "${SKIP_REAL}" == false ]]; then
  mkdir -p "${BENCH_DIR}"
  JSON_DIR="${BENCH_DIR}/hyperfine_json"
  mkdir -p "${JSON_DIR}"
  cd "${BENCH_DIR}"

  append_output "------------------------------------------------------------------------------"
  append_output "SECTION 2: REAL-WORLD REPOSITORY BENCHMARKS"
  append_output "------------------------------------------------------------------------------"
  append_output "Raw hyperfine JSON exports: ${JSON_DIR}"
  append_output ""

  # Repository definitions
  declare -A REPO_URLS
  REPO_URLS["ripgrep"]="https://github.com/BurntSushi/ripgrep.git"
  REPO_URLS["go"]="https://github.com/golang/go.git"
  if [[ "${SKIP_LINUX}" == false ]]; then
    REPO_URLS["linux"]="https://github.com/torvalds/linux.git"
  fi

  for REPO_NAME in "ripgrep" "go" "linux"; do
    if [[ "${REPO_NAME}" == "linux" && "${SKIP_LINUX}" == true ]]; then
      info "Skipping linux repository (--skip-linux)."
      continue
    fi

    REPO_URL="${REPO_URLS[$REPO_NAME]}"
    REPO_DISPLAY="${REPO_URL#https://github.com/}"
    REPO_DISPLAY="${REPO_DISPLAY%.git}"
    TARGET_DIR="${BENCH_DIR}/${REPO_NAME}"
    REPACK_MARKER="${TARGET_DIR}/.git/grg-bench-repacked"

    log "Preparing repository: ${REPO_NAME} (${REPO_URL})..."

    # Unhappy path: Git clone with error handling
    CLONE_SUCCESS=true
    if [[ ! -d "${TARGET_DIR}/.git" ]]; then
      log "Cloning ${REPO_NAME} (this may take several minutes)..."
      rm -rf "${TARGET_DIR}" 2>/dev/null || true

      if git clone "${REPO_URL}" "${TARGET_DIR}" 2>&1; then
        # A fresh clone already arrives as a single optimized pack.
        touch "${REPACK_MARKER}"
      else
        error "Failed to clone ${REPO_NAME} from ${REPO_URL}."
        rm -rf "${TARGET_DIR}" 2>/dev/null || true
        CLONE_SUCCESS=false
      fi
    else
      info "Repository ${REPO_NAME} already exists in ${TARGET_DIR}."
      if [[ "${SKIP_CLONE}" == true ]]; then
        info "Skipping repack (--skip-clone)."
      elif [[ -f "${REPACK_MARKER}" ]]; then
        info "Packfiles already optimized on a previous run; skipping repack."
      else
        info "Running git repack to optimize packfiles for benchmark accuracy (one-time)..."
        if (cd "${TARGET_DIR}" && git repack -a -d -q 2>/dev/null); then
          touch "${REPACK_MARKER}"
        else
          warn "git repack failed for ${REPO_NAME}; continuing with existing packfiles."
        fi
      fi
    fi

    if [[ "${CLONE_SUCCESS}" == false ]]; then
      error "Skipping benchmarks for ${REPO_NAME} due to clone failure."
      append_output "Repository: ${REPO_NAME} - CLONE FAILED"
      FAILED_TESTS=$((FAILED_TESTS + 5))
      TOTAL_TESTS=$((TOTAL_TESTS + 5))
      continue
    fi

    # Extract repository stats safely
    COMMIT_COUNT=$(git -C "${TARGET_DIR}" rev-list --count HEAD 2>/dev/null || echo "N/A")
    PACK_SIZE=$(du -sh "${TARGET_DIR}/.git/objects/pack" 2>/dev/null | cut -f1 || echo "N/A")
    TOTAL_DISK=$(du -sh "${TARGET_DIR}" 2>/dev/null | cut -f1 || echo "N/A")
    COMMIT_COUNT_FMT=$(with_commas "${COMMIT_COUNT}")

    log "${REPO_NAME}: ${COMMIT_COUNT} commits | Pack Size: ${PACK_SIZE} | Total On Disk: ${TOTAL_DISK}"

    append_output ""
    append_output "=============================================================================="
    append_output "Target Repository: ${REPO_NAME}"
    append_output "Source: ${REPO_URL}"
    append_output "Commits: ${COMMIT_COUNT} | Pack Size: ${PACK_SIZE} | Total Disk: ${TOTAL_DISK}"
    append_output "=============================================================================="

    # Run counts and baseline policy per repository size
    if [[ "${REPO_NAME}" == "linux" ]]; then
      RUN_FLAGS=(--warmup "${WARMUP_RUNS}" --min-runs "${LARGE_MIN_RUNS}" --max-runs "${LARGE_MAX_RUNS}")
      REPO_BASELINE=false
    else
      RUN_FLAGS=(--warmup "${WARMUP_RUNS}" --min-runs "${SMALL_MIN_RUNS}")
      REPO_BASELINE="${RUN_BASELINE}"
    fi

    # Scenario matrix per repository. Each entry is a '|'-separated record:
    #   name | grg arguments | ERE for the grep baseline | git pathspec | grep flags
    # The baseline is the conventional way to search history without grg:
    #   git log -p --format= [-- PATHSPEC] | grep <flags> 'ERE'
    # Count scenarios use grep -c; the full-output scenario prints matches.
    declare -a SCENARIOS
    case "${REPO_NAME}" in
      ripgrep)
        SCENARIOS=(
          "Common Term: 'TODO'|--color=never -c TODO|TODO||-cE"
          "Rare Identifier: 'regex_syntax'|--color=never -c regex_syntax|regex_syntax||-cE"
          "Regex Pattern: 'fn\s+[a-z_]+'|--color=never -c 'fn\s+[a-z_]+'|fn\s+[a-z_]+||-cE"
          "Path-Filtered: 'unsafe' in '*.rs'|--color=never -g '*.rs' -c unsafe|unsafe|*.rs|-cE"
          "Full Output: 'TODO' (formatted matches)|--color=never --no-heading TODO|TODO||-E"
        )
        ;;
      go)
        SCENARIOS=(
          "Common Term: 'TODO'|--color=never -c TODO|TODO||-cE"
          "Rare Identifier: 'runtime.throw'|--color=never -c runtime.throw|runtime.throw||-cE"
          "Regex Pattern: 'func\s+[A-Z][a-zA-Z0-9_]*'|--color=never -c 'func\s+[A-Z][a-zA-Z0-9_]*'|func\s+[A-Z][a-zA-Z0-9_]*||-cE"
          "Path-Filtered: 'sync.Mutex' in 'src/'|--color=never -c sync.Mutex -- src/|sync.Mutex|src/|-cE"
          "Full Output: 'TODO' (formatted matches)|--color=never --no-heading TODO|TODO||-E"
        )
        ;;
      linux)
        SCENARIOS=(
          "Common Term: 'TODO'|--color=never -c TODO|TODO||-cE"
          "Rare Identifier: 'GFP_KERNEL'|--color=never -c GFP_KERNEL|GFP_KERNEL||-cE"
          "Regex Pattern: 'static\s+int\s+__init'|--color=never -c 'static\s+int\s+__init'|static\s+int\s+__init||-cE"
          "Path-Filtered: 'EXPORT_SYMBOL' in 'kernel/'|--color=never -c EXPORT_SYMBOL -- kernel/|EXPORT_SYMBOL|kernel/|-cE"
          "Full Output: 'TODO' (formatted matches)|--color=never --no-heading TODO|TODO||-E"
        )
        ;;
    esac

    for ((i=0; i<${#SCENARIOS[@]}; i++)); do
      IFS='|' read -r SCENARIO_NAME SCENARIO_ARGS BASE_ERE BASE_PATHSPEC BASE_GREP_FLAGS <<<"${SCENARIOS[$i]}"
      TOTAL_TESTS=$((TOTAL_TESTS + 1))

      log "Benchmarking [${REPO_NAME}] -> ${SCENARIO_NAME}..."
      append_output ">>> Scenario: ${SCENARIO_NAME}"
      append_output "Command: grg ${SCENARIO_ARGS}"

      # Build the command strings hyperfine will hand to /bin/sh. hyperfine
      # discards command stdout by default, so the full-output scenario measures
      # formatting cost without terminal rendering cost.
      CD_PREFIX="cd $(printf '%q' "${TARGET_DIR}") &&"
      GRG_CMD="${CD_PREFIX} $(printf '%q' "${GRG_BIN}") ${SCENARIO_ARGS}"
      HF_NAMES=(-n "grg")
      HF_CMDS=("${GRG_CMD}")

      if [[ "${REPO_BASELINE}" == true ]]; then
        BASE_CMD="${CD_PREFIX} git log -p --format= --no-color"
        if [[ -n "${BASE_PATHSPEC}" ]]; then
          BASE_CMD+=" -- $(printf '%q' "${BASE_PATHSPEC}")"
        fi
        BASE_CMD+=" | grep ${BASE_GREP_FLAGS} $(printf '%q' "${BASE_ERE}")"
        HF_NAMES+=(-n "git log -p | grep")
        HF_CMDS+=("${BASE_CMD}")
        append_output "Baseline: ${BASE_CMD#"${CD_PREFIX} "}"
      fi

      JSON_FILE="${JSON_DIR}/${REPO_NAME}_${i}.json"
      rm -f "${JSON_FILE}" 2>/dev/null || true

      # --ignore-failure: grg (and grep -c) exit 1 when nothing matches, which is
      # a valid result. Real crashes are detected from the exported exit codes.
      if ! hyperfine "${RUN_FLAGS[@]}" --ignore-failure \
          --export-json "${JSON_FILE}" \
          "${HF_NAMES[@]}" "${HF_CMDS[@]}" 2>&1 | tee -a "${OUTPUT_FILE}"; then
        warn "hyperfine failed on scenario: ${SCENARIO_NAME}"
        FAILED_TESTS=$((FAILED_TESTS + 1))
        append_output "STATUS: FAILED DURING TIMING"
        README_ROWS+=("| **${REPO_DISPLAY}** | ${COMMIT_COUNT_FMT} | ${PACK_SIZE} | ${SCENARIO_NAME} | *failed* | *failed* | – |")
        append_output ""
        continue
      fi

      if [[ ! -s "${JSON_FILE}" ]]; then
        warn "hyperfine produced no JSON export for scenario: ${SCENARIO_NAME}"
        FAILED_TESTS=$((FAILED_TESTS + 1))
        append_output "STATUS: FAILED (no timing data)"
        README_ROWS+=("| **${REPO_DISPLAY}** | ${COMMIT_COUNT_FMT} | ${PACK_SIZE} | ${SCENARIO_NAME} | *failed* | *failed* | – |")
        append_output ""
        continue
      fi

      # Any grg exit code other than 0 (match) or 1 (no match) is a crash.
      BAD_EXITS=$(jq -r '[(.results[0].exit_codes // [])[] | select(. != 0 and . != 1)] | unique | join(",")' "${JSON_FILE}")
      GRG_MEAN=$(jq -r '.results[0].mean' "${JSON_FILE}")
      GRG_STDDEV=$(jq -r '.results[0].stddev // empty' "${JSON_FILE}")
      GRG_RUNS=$(jq -r '.results[0].times | length' "${JSON_FILE}")

      if [[ -n "${BAD_EXITS}" ]]; then
        error "grg exited with unexpected code(s) ${BAD_EXITS} for scenario: ${SCENARIO_NAME}"
        FAILED_TESTS=$((FAILED_TESTS + 1))
        append_output "STATUS: FAILED (grg exit codes: ${BAD_EXITS})"
        README_ROWS+=("| **${REPO_DISPLAY}** | ${COMMIT_COUNT_FMT} | ${PACK_SIZE} | ${SCENARIO_NAME} | *failed (exit ${BAD_EXITS})* | – | – |")
        append_output ""
        continue
      fi

      GRG_CELL="$(fmt_ms "${GRG_MEAN}") ± $(fmt_ms "${GRG_STDDEV}")"
      if [[ "${REPO_BASELINE}" == true ]]; then
        BASE_MEAN=$(jq -r '.results[1].mean' "${JSON_FILE}")
        BASE_STDDEV=$(jq -r '.results[1].stddev // empty' "${JSON_FILE}")
        BASE_CELL="$(fmt_ms "${BASE_MEAN}") ± $(fmt_ms "${BASE_STDDEV}")"
        SPEEDUP=$(awk -v b="${BASE_MEAN}" -v g="${GRG_MEAN}" 'BEGIN { if (g > 0) printf "%.1f×", b / g; else print "N/A" }')
      else
        BASE_CELL="*not run*"
        SPEEDUP="–"
      fi

      pass "Completed: ${SCENARIO_NAME} (grg ${GRG_CELL}, ${GRG_RUNS} runs)"
      PASSED_TESTS=$((PASSED_TESTS + 1))
      append_output "grg: mean ${GRG_CELL} over ${GRG_RUNS} runs"
      if [[ "${REPO_BASELINE}" == true ]]; then
        append_output "git log -p | grep: mean ${BASE_CELL} (grg ${SPEEDUP} faster)"
      fi
      README_ROWS+=("| **${REPO_DISPLAY}** | ${COMMIT_COUNT_FMT} | ${PACK_SIZE} | ${SCENARIO_NAME} | ${GRG_CELL} | ${BASE_CELL} | ${SPEEDUP} |")
      append_output ""
    done
  done
else
  info "Skipping real-world repository benchmarks (--skip-real)."
  append_output "SECTION 2: REAL-WORLD REPOSITORY BENCHMARKS SKIPPED"
  append_output ""
fi

# ------------------------------------------------------------------------------
# 6. READY-TO-USE README.MD PERFORMANCE TEMPLATE
# ------------------------------------------------------------------------------
append_output "=============================================================================="
append_output "SECTION 3: FORMATTED README.MD MARKDOWN REPLACEMENT"
append_output "=============================================================================="
append_output "Copy and paste the markdown below into README.md (replacing Performance section):"
append_output ""
append_output "---"
append_output ""
append_output "## Performance & Benchmarks"
append_output ""
append_output "\`grg\` is engineered from the ground up for maximum throughput across long Git histories without requiring working tree checkouts."
append_output ""
append_output "### Benchmark Environment"
append_output "- **Platform**: ${PLATFORM_LABEL}"
append_output "- **Hardware**: ${CPU_MODEL}, ${CPU_CORES} logical cores, ${TOTAL_MEM} RAM"
append_output "- **OS**: ${OS_INFO}"
append_output "- **Methodology**: Mean ± standard deviation via \`hyperfine\` (${WARMUP_RUNS} warmup run, at least ${SMALL_MIN_RUNS} timed runs; ${LARGE_MIN_RUNS} to ${LARGE_MAX_RUNS} runs on linux). Timings measured $(date -u +"%Y-%m-%d")."
append_output "- **Baseline**: \`git log -p --format= | grep\`, the conventional way to search history without \`grg\`. Note that the baseline only scans diff hunks, while \`grg\` scans full blob contents at every commit, so \`grg\` does strictly more work per commit."
append_output ""
append_output "### Real-World Repository Search Scalability"
append_output "| Repository | Commits | Packfile | Query Scenario | grg | git log -p \\| grep | Speedup |"
append_output "| :--- | ---: | ---: | :--- | ---: | ---: | ---: |"
if [[ ${#README_ROWS[@]} -gt 0 ]]; then
  for row in "${README_ROWS[@]}"; do
    append_output "${row}"
  done
else
  append_output "| *(no real-world scenarios were run)* | | | | | | |"
fi
append_output ""
append_output "---"

# ------------------------------------------------------------------------------
# 7. FINAL EXECUTION SUMMARY
# ------------------------------------------------------------------------------
echo ""
echo -e "${BOLD}==============================================================================${NC}"
echo -e "${BOLD}BENCHMARK EXECUTION SUMMARY${NC}"
echo -e "${BOLD}==============================================================================${NC}"
echo -e "Total Scenarios Executed : ${BOLD}${TOTAL_TESTS}${NC}"
echo -e "Passed Scenarios         : ${GREEN}${BOLD}${PASSED_TESTS}${NC}"
echo -e "Failed Scenarios         : $( [[ ${FAILED_TESTS} -gt 0 ]] && echo -e "${RED}${BOLD}${FAILED_TESTS}${NC}" || echo -e "${GREEN}0${NC}" )"
echo -e "Results File             : ${BOLD}${OUTPUT_FILE}${NC}"
echo -e "${BOLD}==============================================================================${NC}"
echo ""

if [[ ${FAILED_TESTS} -gt 0 ]]; then
  warn "Some benchmark scenarios encountered issues. Check ${OUTPUT_FILE} for detailed failure logs."
  exit 2
else
  pass "All executed benchmarks completed successfully!"
fi
