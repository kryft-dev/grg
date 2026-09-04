# grg (Git Ripgrep)

[![Go Reference](https://pkg.go.dev/badge/github.com/kryft-dev/grg.svg)](https://pkg.go.dev/github.com/kryft-dev/grg)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Tests](https://img.shields.io/badge/tests-passing-brightgreen.svg)]()
[![Go Report Card](https://goreportcard.com/badge/github.com/kryft-dev/grg)](https://goreportcard.com/report/github.com/kryft-dev/grg)

`grg` is a lightning-fast, standalone command-line search tool that brings **ripgrep** semantics directly to **Git repository history**.

Traditional Git search tools like `git log -S`, `git log -G`, or `git grep` either require checking out commits to disk, re-evaluating full commit diffs sequentially, or only searching a single commit at a time. `grg` reads Git packfiles and loose objects directly from disk, traverses the commit DAG without checking out files, deduplicates blobs by cryptographic Object ID (OID) across the entire history, and searches contents in parallel with zero runtime dependencies.

---

## Features & Core Capabilities

- **Direct Git Object Reading (No Checkouts):** Traverses and decodes Git commit trees, packfiles (`.pack`), and pack index files (`.idx` v2) directly from `.git/objects/`. Never modifies the working directory or writes temporary files to disk.
- **Cryptographic OID Deduplication:** If a file remains untouched across 10,000 commits, `grg` scans its underlying blob exactly once. Matches are then attributed back to the introducing commit, delivering massive speedups across deep repository histories.
- **Full DAG & Merge Traversal:** Correctly handles complex branching, multi-parent merge commits, revisions, revision ranges (`HEAD~10..HEAD`, `feature...main`), and branch specifications.
- **Deleted & Renamed File Discovery:** Searches content across every version of a file that ever existed in history, surfacing deleted modules, legacy configurations, and relocated packages instantly.
- **Ripgrep-Class Ergonomics:** Familiar flag set including `-i`, `-s`, `-S`, `-F`, `-w`, `-C`, `-A`, `-B`, `-g`, `-t`, `-l`, `-c`, `-q`, and `--color`.
- **Zero Cgo & Zero External Dependencies:** 100% pure Go binary. Does not depend on `git`, `libgit2`, Cgo, or any shared C libraries. Compiles cleanly for Linux, macOS, and Windows.
- **High-Throughput Vectorized Engine:** Powered by SIMD-accelerated zlib decompression and reusable memory buffer pools for delta decompression exceeding **28+ GB/s**.

---

## Installation

### Binary Releases
Download pre-compiled standalone binaries for Linux, macOS, and Windows from the [GitHub Releases](https://github.com/kryft-dev/grg/releases) page.

### Go Install
Install the latest release directly via the Go toolchain:
```bash
go install github.com/kryft-dev/grg/cmd/grg@latest
```

### Build from Source
Ensure Go 1.22 or newer is installed:
```bash
# Clone the repository
git clone https://github.com/kryft-dev/grg.git
cd grg

# Build the standalone binary
go build -ldflags="-s -w" -o grg ./cmd/grg

# Verify installation
./grg --version
```

---

## Usage & Examples

```text
Usage:
    grg [FLAGS] PATTERN [REV_RANGE] [-- PATHS...]
    grg [FLAGS] -e PATTERN... [REV_RANGE] [-- PATHS...]
```

### 1. Basic Search
Search for a pattern across all commits in `HEAD` ancestry:
```bash
grg "database_url"
```

Search across all reachable commits, branches, and tags:
```bash
grg --all "API_KEY"
```

### 2. Case Sensitivity & Regex Matching
Control pattern matching using standard ripgrep flags:
```bash
# Case-insensitive search
grg -i "handleerror"

# Case-sensitive search (default)
grg -s "HandleError"

# Smart case: case-insensitive unless uppercase letters are provided
grg -S "handler"    # matches Handler, handler, HANDLER
grg -S "Handler"    # matches only Handler

# Fixed literal string search (no regex parsing)
grg -F "fmt.Sprintf(\"%s\", name)"

# Whole-word regex match (matches word boundaries \b...\b)
grg -w "token"

# Invert match: select non-matching lines
grg -v "import"
```

### 3. Context Lines
Display lines before and after matches for better code understanding:
```bash
# Show 3 lines of context before and after each match
grg -C 3 "panic("

# Show 2 lines after each match
grg -A 2 "func Init("

# Show 2 lines before each match
grg -B 2 "return err"
```

### 4. File, Path, and Type Filtering
Filter targets by path prefixes, glob patterns, or file type aliases:
```bash
# Filter by directory path (after '--')
grg "config" -- internal/ config/

# Include or exclude files using glob patterns
grg -g "*.go" "type Config struct"
grg -g "!*_test.go" "Benchmark"

# Filter by predefined file types
grg -t py "def run_task"
grg -t go -t rust "fn main"
```

Supported built-in file types include: `go`, `py`/`python`, `rust`, `ts`/`typescript`, `js`/`javascript`, `c`, `cpp`, `css`, `html`, `json`, `yaml`, `toml`, `sql`, `sh`/`bash`, `docker`/`dockerfile`, `make`/`makefile`, `md`/`markdown`, `java`, `kotlin`, `swift`, `ruby`, `php`, `lua`, `perl`, `scala`, `xml`, and `zig`.

### 5. Git History & Revision Filtering
Target specific branches, commit ranges, dates, or commit authors:
```bash
# Search within a commit range (two-dot or three-dot)
grg "TODO" HEAD~10..HEAD
grg "bugfix" main..feature-branch

# Search only commits after a specific date
grg --since "2024-01-01" "AWS_SECRET"
grg --since "2 weeks ago" "deprecated"

# Search commits before a date
grg --until "2023-06-01" "legacy_auth"

# Filter by author or committer regex
grg --author "Alice" "refactor"
grg --committer "bot@company" "bump"

# Target specific branches
grg --branch main --branch release/v1.0 "hotfix"

# Follow only first parents (skip side branches in merges)
grg --first-parent "v1.2.0-rc"

# Include dirty working tree files and uncommitted index modifications
grg --uncommitted "temporary_debug"
```

### 6. File Summaries & Counts
Produce high-level overviews of where patterns exist:
```bash
# Print only files and commits containing matches (similar to rg -l)
grg -l "FIXME"

# Print total count of matching lines across files (similar to rg -c)
grg -c "TODO"

# Quiet exit: exit 0 if pattern exists in history, exit 1 if absent
grg -q "deprecatedMethod" && echo "Pattern exists in history!"
```

### 7. Scripting & Pipeline Formatting
Customize output structure for automated tooling and piping:
```bash
# Suppress headers; emit single lines formatted as <commit>:<path>:<line>:<text>
grg --no-heading "metrics"

# Disable ANSI color escapes when piping to other tools
grg --color=never "export" | awk -F: '{print $1, $2}'

# Search binary files as text
grg -a "magic_bytes"
```

---

## Output Model & Attribution

`grg` defaults to an intuitive hierarchical output that answers **what** matched, **where** it matched, and **when** it was introduced:

```text
internal/gitengine/pack.go
[8752dea 2024-09-04 Jane Doe] Add support for packfile v2 indexing
24:type PackReader struct {
25:    packPath string
26:    file     *os.File
```

### Introducing Commit Attribution
In Git history, a file often remains unchanged across dozens of merge or release commits. By default, `grg` identifies the **introducing commit** (the first commit in the revision ancestry that created or altered that specific blob version at that path). 

If you prefer to see every historical commit where the blob appeared, pass `--expand-commits`:
```bash
grg --expand-commits "constant_value"
```

---

## Ripgrep Flag Parity

`grg` is designed to feel immediately familiar to `ripgrep` (`rg`) users while optimizing for Git object history traversal.

| Flag / Option | Supported | Description |
| :--- | :---: | :--- |
| `-i, --ignore-case` | :white_check_mark: | Case-insensitive search |
| `-s, --case-sensitive` | :white_check_mark: | Case-sensitive search (default) |
| `-S, --smart-case` | :white_check_mark: | Case-insensitive unless pattern contains uppercase |
| `-F, --fixed-strings` | :white_check_mark: | Treat pattern as a literal string |
| `-w, --word-regexp` | :white_check_mark: | Match only whole words |
| `-v, --invert-match` | :white_check_mark: | Select non-matching lines |
| `-n, --line-number` | :white_check_mark: | Show line numbers (default: enabled) |
| `-N, --no-line-number` | :white_check_mark: | Suppress line numbers |
| `-l, --files-with-matches` | :white_check_mark: | Print only paths and commit SHAs with matches |
| `-c, --count` | :white_check_mark: | Print total count of matching lines |
| `-C, --context NUM` | :white_check_mark: | Show NUM lines before and after matches |
| `-A, --after-context NUM` | :white_check_mark: | Show NUM lines after matches |
| `-B, --before-context NUM` | :white_check_mark: | Show NUM lines before matches |
| `-m, --max-count NUM` | :white_check_mark: | Stop searching a blob after NUM matches |
| `-q, --quiet` | :white_check_mark: | Do not print matches; exit 0 if match found, 1 otherwise |
| `-a, --text` | :white_check_mark: | Search binary files as text |
| `-g, --glob GLOB` | :white_check_mark: | Include or exclude files by glob pattern |
| `-t, --type TYPE` | :white_check_mark: | Filter files by type alias (e.g. `-t go -t rust`) |
| `-e, --regexp PATTERN` | :white_check_mark: | Search pattern (can be specified multiple times) |
| `--color WHEN` | :white_check_mark: | Terminal colorization (`auto`, `always`, `never`, `ansi`) |
| `--heading` | :white_check_mark: | Print file and commit headers above matches (default) |
| `--no-heading` | :white_check_mark: | Print matches inline in single-line format |
| `--all` | *(Git)* | Search all reachable commits across all branches and tags |
| `--since / --until` | *(Git)* | Filter commits by timestamp or human-readable relative date |
| `--author / --committer` | *(Git)* | Filter commits by author or committer regex |
| `--branch` | *(Git)* | Target specific Git branches |
| `--first-parent` | *(Git)* | Follow only the first parent of merge commits |
| `--uncommitted` | *(Git)* | Search working tree modifications and staged changes |
| `--expand-commits` | *(Git)* | Attribute matches to all historical commits referencing the blob |
| `--unordered` | *(Git)* | Stream matches immediately without chronological sorting |

### Differences from ripgrep (`rg`)
1. **Target Medium:** `rg` searches working directory files on the filesystem. `grg` searches Git objects (`.git/objects/pack/` and loose objects) across commit history without touching the filesystem working tree.
2. **Attribution:** Matches in `grg` include commit provenance (commit SHA, date, author, summary), and repeated identical blobs are deduplicated by default.
3. **Filesystem Flags Excluded:** Flags specific to directory traversal (such as `--follow` for symlinks, `--max-depth`, `--hidden`, `.gitignore` filtering) are omitted because `grg` traverses Git tree structures directly.
4. **Exit Codes:**
   - `0`: Match found.
   - `1`: No match found.
   - `2`: CLI argument or regex syntax error.
   - `128`: Repository discovery or Git object read error.

---

## Performance & Benchmarks

`grg` was engineered from the ground up for maximum throughput on large codebases and long commit histories.

### 1. OID Blob Deduplication (7.2x+ Speedup)
In a real-world repository, most commits change only a handful of files. By deduplicating blobs by their SHA-1/SHA-256 OID before executing search workers:

| Scenario | Commits | Files | Blob Reuse | Throughput / Search Time | Speedup |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **Unique Blobs** | 150 | 30 | 0% | 613.8 ms | 1.0x |
| **Typical History (grg)** | 150 | 30 | 90% | **85.0 ms** | **7.2x faster** |

### 2. Microbenchmark: Delta Decompression Throughput
Git packfiles store object histories as delta instructions (`OBJ_OFS_DELTA` and `OBJ_REF_DELTA`). `grg` features a specialized delta decoder utilizing pooled scratch buffers:

```text
BenchmarkDeltaDecompression-4    554,282 ops    2,211 ns/op    29,637.99 MB/s (> 29.6 GB/s)
```

### 3. Scalability Benchmarks
End-to-end repository searches across packed synthetic repositories:

| Repository Scale | Commits | Packfile Objects | Execution Time |
| :--- | :---: | :---: | :---: |
| Small History | 100 commits | ~2,000 | ~157 ms |
| Medium History | 500 commits | ~25,000 | ~2.73 s |
| Large History | 1,000 commits | ~100,000 | ~8.73 s |

*(Benchmarks run on Intel Core i5-6300U CPU @ 2.40GHz, 4 logical cores, Linux 6.x)*

---

## Architecture

`grg` operates as a decoupled, multi-stage pipeline designed to minimize I/O and memory allocations:

```
┌────────────────┐      ┌─────────────────────────┐      ┌───────────────────────┐
│ Git Discovery  │ ───> │ Commit DAG Walker       │ ───> │ Tree Traversal        │
│ & Repo Reader  │      │ (revlist & date/author) │      │ (Path & Glob Filters) │
└────────────────┘      └─────────────────────────┘      └───────────────────────┘
                                                                     │
                                                                     ▼
┌────────────────┐      ┌─────────────────────────┐      ┌───────────────────────┐
│ Output Format  │ <─── │ Aggregator              │ <─── │ Concurrent Search     │
│ (TTY / Pipes)  │      │ (Provenance & Sorting)  │      │ (OID Deduplication)   │
└────────────────┘      └─────────────────────────┘      └───────────────────────┘
```

1. **Repository Discovery (`internal/gitengine/discovery.go`):**
   - Automatically climbs parent directories to locate `.git`.
   - Handles standard Git repositories, worktrees, submodules, bare repositories, and custom `GIT_DIR`/`GIT_WORK_TREE` environments.

2. **Packfile & Loose Object Reader (`internal/gitengine/`):**
   - **Pack Index (`.idx` v2):** Implements a 256-entry fanout table with binary search over object SHA tables, with full 64-bit offset support for packfiles exceeding 2GB.
   - **SIMD zlib Decompression:** Leverages `github.com/klauspost/compress/zlib` for hardware-accelerated vectorized decompression.
   - **Delta Reconstruction:** Recursively resolves `OBJ_OFS_DELTA` and `OBJ_REF_DELTA` chains up to 50 levels deep using `sync.Pool` memory buffer recycling.

3. **History Walker & Revspec Parser (`internal/gitengine/walker.go`, `revlist.go`):**
   - Supports single commit SHAs, references, two-dot ranges (`A..B`), and three-dot symmetric differences (`A...B`).
   - Filters commits by date (`--since`, `--until`) and author regex before tree expansion.
   - Performs rapid tree traversal, filtering paths via glob patterns and file extensions early to avoid resolving irrelevant blobs.

4. **Deduplicating Search Pipeline (`internal/search/pipeline.go`):**
   - Collects all candidate blob occurrences across history and coalesces identical blob OIDs into unique search tasks.
   - Spawns a concurrent worker pool (`runtime.NumCPU()`) to decompress and match each unique blob once.
   - Null-byte binary detection protects terminals from binary corruption unless `-a` / `--text` is supplied.

5. **Aggregation & Output Formatting (`internal/aggregator`, `internal/output`):**
   - Maps search matches back to their historical occurrences.
   - Collapses unchanged blobs to their introducing commit (unless `--expand-commits` is set).
   - Sorts results chronologically (newest commits first) and formats output with ANSI color highlights, file headers, line numbers, or single-line `--no-heading` records.

---

## License

`grg` is open source software released under the [MIT License](LICENSE).
