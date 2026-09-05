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

# Metrics tracking
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0
SKIPPED_TESTS=0

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
  -h, --help          Show this help message
HELP
      exit 0
      ;;
    *)
      error "Unknown option: $1"
      exit 1
      ;;
  esac
done

# Signal trap for clean termination
cleanup() {
  local exit_code=$?
  rm -f "${BENCH_DIR}"/temp_*.md 2>/dev/null || true
  if [[ $exit_code -ne 0 ]]; then
    echo -e "\n${RED}[ABORT] Benchmark script interrupted or exited with error code ${exit_code}.${NC}"
  fi
  exit "${exit_code}"
}
trap cleanup EXIT INT TERM

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
  • Estimated Runtime   : ${BOLD}5 to 20 minutes${NC} (depending on network and disk speed)
${YELLOW}################################################################################${NC}

BANNER

  # Check available disk space
  local available_kb
  available_kb=$(df -k "${REPO_ROOT}" | awk 'NR==2 {print $4}')
  local available_gb=$(( available_kb / 1024 / 1024 ))

  info "Available disk space in workspace: ${BOLD}${available_gb} GB${NC}"

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

# ------------------------------------------------------------------------------
# 1. HARDWARE ENVIRONMENT & MACHINE SPECIFICATIONS
# ------------------------------------------------------------------------------
log "Collecting hardware environment and machine specifications..."

CPU_MODEL=$(lscpu 2>/dev/null | grep "Model name:" | sed 's/Model name:[ \t]*//' || grep -m1 "model name" /proc/cpuinfo 2>/dev/null | cut -d: -f2 | xargs || echo "Generic x86_64 CPU")
CPU_CORES=$(nproc --all 2>/dev/null || echo "4")
TOTAL_MEM=$(free -h 2>/dev/null | awk '/^Mem:/ {print $2}' || echo "N/A")
OS_INFO=$(uname -s -r -m 2>/dev/null || echo "Linux")

log "Detected: ${CPU_MODEL} (${CPU_CORES} cores), ${TOTAL_MEM} RAM, ${OS_INFO}"

append_output "=============================================================================="
append_output "grg PRODUCTION BENCHMARK REPORT"
append_output "Generated at: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
append_output "=============================================================================="
append_output "System Environment:"
append_output "  - CPU Model:    ${CPU_MODEL}"
append_output "  - CPU Cores:    ${CPU_CORES} logical cores"
append_output "  - Memory:       ${TOTAL_MEM}"
append_output "  - Platform:     ${OS_INFO}"
append_output "=============================================================================="
append_output ""

# ------------------------------------------------------------------------------
# 2. PREREQUISITES & COMPILER VALIDATION
# ------------------------------------------------------------------------------
log "Validating Go compiler environment..."

if ! command -v go >/dev/null 2>&1; then
  error "Go compiler is missing from PATH. Cannot proceed."
  append_output "FATAL: Go compiler not found."
  exit 1
fi

GO_VERSION=$(go version 2>&1 || echo "Go version unknown")
log "Go Version: ${GO_VERSION}"
append_output "Compiler: ${GO_VERSION}"

# Hyperfine setup with fallback
HYPERFINE_AVAILABLE=false
if command -v hyperfine >/dev/null 2>&1; then
  HYPERFINE_AVAILABLE=true
  log "Using installed hyperfine: $(hyperfine --version)"
else
  log "hyperfine not detected. Attempting automatic setup..."
  if command -v sudo >/dev/null 2>&1 && command -v apt-get >/dev/null 2>&1; then
    sudo apt-get update -qq && sudo apt-get install -y -qq hyperfine 2>/dev/null || true
  fi

  if command -v hyperfine >/dev/null 2>&1; then
    HYPERFINE_AVAILABLE=true
  else
    log "Attempting download of standalone static hyperfine binary..."
    mkdir -p "${BIN_DIR}"
    if curl -fsSL --connect-timeout 10 "https://github.com/sharkdp/hyperfine/releases/download/v1.19.0/hyperfine-v1.19.0-x86_64-unknown-linux-musl.tar.gz" 2>/dev/null | tar -xz -C "${BIN_DIR}" --strip-components=1 "hyperfine-v1.19.0-x86_64-unknown-linux-musl/hyperfine" 2>/dev/null; then
      chmod +x "${BIN_DIR}/hyperfine"
      export PATH="${BIN_DIR}:${PATH}"
      HYPERFINE_AVAILABLE=true
      log "Installed static hyperfine to ${BIN_DIR}/hyperfine"
    else
      warn "Unable to install hyperfine. Falling back to high-resolution bash timers."
    fi
  fi
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
  cd "${BENCH_DIR}"

  append_output "------------------------------------------------------------------------------"
  append_output "SECTION 2: REAL-WORLD REPOSITORY BENCHMARKS"
  append_output "------------------------------------------------------------------------------"

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
    TARGET_DIR="${BENCH_DIR}/${REPO_NAME}"

    log "Preparing repository: ${REPO_NAME} (${REPO_URL})..."

    # Unhappy path: Git clone with error handling & retry
    CLONE_SUCCESS=true
    if [[ ! -d "${TARGET_DIR}/.git" ]]; then
      log "Cloning ${REPO_NAME} (this may take several minutes)..."
      rm -rf "${TARGET_DIR}" 2>/dev/null || true

      if ! git clone "${REPO_URL}" "${TARGET_DIR}" 2>&1; then
        error "Failed to clone ${REPO_NAME} from ${REPO_URL}."
        rm -rf "${TARGET_DIR}" 2>/dev/null || true
        CLONE_SUCCESS=false
      fi
    else
      info "Repository ${REPO_NAME} already exists in ${TARGET_DIR}."
      if [[ "${SKIP_CLONE}" == false ]]; then
        info "Running git repack to optimize packfiles for benchmark accuracy..."
        (cd "${TARGET_DIR}" && git repack -a -d -q 2>/dev/null || true)
      fi
    fi

    if [[ "${CLONE_SUCCESS}" == false ]]; then
      error "Skipping benchmarks for ${REPO_NAME} due to clone failure."
      append_output "Repository: ${REPO_NAME} - CLONE FAILED"
      FAILED_TESTS=$((FAILED_TESTS + 4))
      continue
    fi

    # Extract repository stats safely
    COMMIT_COUNT=$(git -C "${TARGET_DIR}" rev-list --count HEAD 2>/dev/null || echo "N/A")
    PACK_SIZE=$(du -sh "${TARGET_DIR}/.git/objects/pack" 2>/dev/null | cut -f1 || echo "N/A")
    TOTAL_DISK=$(du -sh "${TARGET_DIR}" 2>/dev/null | cut -f1 || echo "N/A")

    log "${REPO_NAME}: ${COMMIT_COUNT} commits | Pack Size: ${PACK_SIZE} | Total On Disk: ${TOTAL_DISK}"

    append_output ""
    append_output "=============================================================================="
    append_output "Target Repository: ${REPO_NAME}"
    append_output "Source: ${REPO_URL}"
    append_output "Commits: ${COMMIT_COUNT} | Pack Size: ${PACK_SIZE} | Total Disk: ${TOTAL_DISK}"
    append_output "=============================================================================="

    # Scenario matrix per repository
    declare -a SCENARIOS
    case "${REPO_NAME}" in
      ripgrep)
        SCENARIOS=(
          "Common Term: 'TODO'" "--color=never -c TODO"
          "Rare Identifier: 'regex_syntax'" "--color=never -c regex_syntax"
          "Regex Pattern: 'fn\s+[a-z_]+'" "--color=never -c 'fn\s+[a-z_]+'"
          "Path-Filtered: 'unsafe' in '*.rs'" "--color=never -g '*.rs' -c unsafe"
        )
        ;;
      go)
        SCENARIOS=(
          "Common Term: 'TODO'" "--color=never -c TODO"
          "Rare Identifier: 'runtime.throw'" "--color=never -c runtime.throw"
          "Regex Pattern: 'func\s+[A-Z][a-zA-Z0-9_]*'" "--color=never -c 'func\s+[A-Z][a-zA-Z0-9_]*'"
          "Path-Filtered: 'sync.Mutex' in 'src/'" "--color=never -c sync.Mutex -- src/"
        )
        ;;
      linux)
        SCENARIOS=(
          "Common Term: 'TODO'" "--color=never -c TODO"
          "Rare Identifier: 'GFP_KERNEL'" "--color=never -c GFP_KERNEL"
          "Regex Pattern: 'static\s+int\s+__init'" "--color=never -c 'static\s+int\s+__init'"
          "Path-Filtered: 'EXPORT_SYMBOL' in 'kernel/'" "--color=never -c EXPORT_SYMBOL -- kernel/"
        )
        ;;
    esac

    for ((i=0; i<${#SCENARIOS[@]}; i+=2)); do
      SCENARIO_NAME="${SCENARIOS[$i]}"
      SCENARIO_ARGS="${SCENARIOS[$i+1]}"
      TOTAL_TESTS=$((TOTAL_TESTS + 1))

      log "Benchmarking [${REPO_NAME}] -> ${SCENARIO_NAME}..."
      append_output ">>> Scenario: ${SCENARIO_NAME}"
      append_output "Command: grg ${SCENARIO_ARGS}"

      # Pre-flight check: verify grg executes the query without crashing
      PREFLIGHT_ERR=""
      if ! PREFLIGHT_ERR=$(cd "${TARGET_DIR}" && eval "${GRG_BIN} ${SCENARIO_ARGS}" >/dev/null 2>&1); then
        # Note: Exit code 1 means no match found, which is a valid ripgrep result
        PREFLIGHT_EXIT=$?
        if [[ ${PREFLIGHT_EXIT} -ne 1 && ${PREFLIGHT_EXIT} -ne 0 ]]; then
          error "grg exited with unexpected code ${PREFLIGHT_EXIT} for scenario: ${SCENARIO_NAME}"
          FAILED_TESTS=$((FAILED_TESTS + 1))
          append_output "STATUS: FAILED (Exit code: ${PREFLIGHT_EXIT})"
          append_output "Error detail: ${PREFLIGHT_ERR}"
          append_output ""
          continue
        fi
      fi

      # Execute timing
      if [[ "${HYPERFINE_AVAILABLE}" == true ]]; then
        TEMP_MD="${BENCH_DIR}/temp_${REPO_NAME}_${i}.md"
        rm -f "${TEMP_MD}" 2>/dev/null || true

        if hyperfine --warmup 1 --runs 3 \
          --export-markdown "${TEMP_MD}" \
          "cd ${TARGET_DIR} && ${GRG_BIN} ${SCENARIO_ARGS}" 2>&1 | tee -a "${OUTPUT_FILE}"; then
          pass "Completed: ${SCENARIO_NAME}"
          PASSED_TESTS=$((PASSED_TESTS + 1))
          if [[ -f "${TEMP_MD}" ]]; then
            append_output ""
            cat "${TEMP_MD}" >> "${OUTPUT_FILE}"
            append_output ""
            rm -f "${TEMP_MD}"
          fi
        else
          warn "Hyperfine encountered a timing anomaly on scenario: ${SCENARIO_NAME}"
          FAILED_TESTS=$((FAILED_TESTS + 1))
          append_output "STATUS: FAILED DURING TIMING"
        fi
      else
        # Fallback to high-precision timestamp timing
        START_NS=$(date +%s%N)
        if (cd "${TARGET_DIR}" && eval "${GRG_BIN} ${SCENARIO_ARGS}" >/dev/null 2>&1); then
          END_NS=$(date +%s%N)
          DURATION_MS=$(( (END_NS - START_NS) / 1000000 ))
          pass "Completed: ${SCENARIO_NAME} in ${DURATION_MS} ms"
          PASSED_TESTS=$((PASSED_TESTS + 1))
          append_output "Execution Time: ${DURATION_MS} ms"
        else
          error "Execution failed for scenario: ${SCENARIO_NAME}"
          FAILED_TESTS=$((FAILED_TESTS + 1))
          append_output "STATUS: EXECUTION FAILED"
        fi
      fi
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
append_output "- **Platform**: GitHub Codespace (${CPU_MODEL}, ${CPU_CORES} logical cores, ${TOTAL_MEM} RAM)"
append_output "- **OS**: ${OS_INFO}"
append_output "- **Methodology**: Statistical mean over multiple runs via \`hyperfine\` (1 warmup, 3 iterations)"
append_output ""
append_output "### Real-World Repository Search Scalability"
append_output "| Repository | Scale | Commits | Packfile Size | Query Scenario | Search Time |"
append_output "| :--- | :---: | :---: | :---: | :--- | :---: |"
append_output "| **BurntSushi/ripgrep** | Small | ~3,000 | ~15 MB | Common (\`TODO\`) | *(See Section 2 above)* |"
append_output "| **BurntSushi/ripgrep** | Small | ~3,000 | ~15 MB | Filtered (\`unsafe\` in \`*.rs\`) | *(See Section 2 above)* |"
append_output "| **golang/go** | Medium | ~60,000 | ~400 MB | Common (\`TODO\`) | *(See Section 2 above)* |"
append_output "| **golang/go** | Medium | ~60,000 | ~400 MB | Regex (\`func\s+[A-Z]...\`) | *(See Section 2 above)* |"
if [[ "${SKIP_LINUX}" == false ]]; then
  append_output "| **torvalds/linux** | Large | ~1,200,000 | ~3.8 GB | Common (\`TODO\`) | *(See Section 2 above)* |"
  append_output "| **torvalds/linux** | Large | ~1,200,000 | ~3.8 GB | Filtered (\`EXPORT_SYMBOL\` in \`kernel/\`) | *(See Section 2 above)* |"
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
