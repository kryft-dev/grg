# Contributing to grg

Thank you for your interest in contributing to `grg` (Git Ripgrep)!

`grg` is a standalone, high-performance command-line tool providing ripgrep-like search semantics directly over Git repository history using pure Go packfile and loose object readers.

---

## Development Setup

### Prerequisites
- **Go**: Version 1.22 or later (development and CI test against Go 1.26+).
- **Git**: For cloning and running integration tests.
- Operating systems supported: Linux, macOS, Windows, and FreeBSD.

### Getting the Code
```bash
git clone https://github.com/kryft-dev/grg.git
cd grg
```

### Building Locally
```bash
# Build binary to the project root
go build ./cmd/grg

# Verify the build
./grg --version
```

---

## Testing & Quality Assurance

All contributions must pass the test suite and static analysis checks.

### Running Tests
```bash
# Run all unit and integration tests
go test -v -count=1 ./...

# Run tests with the Go race detector enabled (required before submitting PRs)
go test -race -count=1 ./...
```

### Running Static Analysis & Linters
```bash
# Run Go vet
go vet ./...

# Run production-grade linters configured in .golangci.yml
golangci-lint run ./...

# Verify module dependency integrity and checksums
go mod verify

# Scan for known vulnerabilities in dependencies
govulncheck ./...

# Ensure code is properly formatted
test -z "$(gofmt -l .)"
```

### Running Benchmarks
```bash
# Run synthetic and repository history benchmarks
go test -v -bench=. -benchmem ./test/benchmark
```

---

## Code Style & Architectural Guidelines

To maintain code health and reliability, please adhere to the following standards:

1. **Pure Go (Zero Cgo, Zero External Runtime Dependencies)**:
   - `grg` must compile into a self-contained, statically linkable binary.
   - Do NOT introduce Cgo bindings (e.g. `libgit2`) or runtime dependencies on external binaries (such as `git`).

2. **Modular Architecture**:
   - Keep packages focused with clear interfaces:
     - `internal/cli`: Ripgrep-compatible CLI parsing, flag specifications, and usage/help rendering.
     - `internal/gitengine`: Low-level Git object reading (packfiles, `.idx` v2, loose objects, delta decompression), repository discovery, and commit DAG traversal.
     - `internal/filter`: Path globs, ripgrep type matching, and commit author/date filters.
     - `internal/search`: Multi-core worker pool, OID deduplication, and byte/regex matching.
     - `internal/aggregator`: Matches collation, historical provenance attribution, and chronological sorting.
     - `internal/output`: ANSI colorized and plain terminal formatters (grouped, single-line, files-with-matches, count).
     - `internal/model`: Pure domain types and representations.

3. **File Size & Deep Modules**:
   - Keep files small, cohesive, and readable.
   - **Target files under 350 lines**. If a file grows beyond this threshold, decompose it into focused sibling modules (e.g. separate parsing, validation, and rendering).

4. **Performance & Memory Management**:
   - Decompress deltas and blobs using pooled buffers (`sync.Pool`) to avoid unnecessary heap allocations.
   - Deduplicate search tasks by blob SHA/OID so identical file contents across commits are scanned only once.
   - Always avoid reading entire packfiles or blobs into memory when streaming or bounded reading is possible.

5. **Concurrency Safety**:
   - All shared state in search pipelines and history walkers must be synchronized or designed for lock-free read access.
   - Validate any concurrent modifications using `go test -race`.

---

## Pull Request Workflow

1. **Create an Issue**: For non-trivial changes, open an issue first to discuss the proposed design or feature.
2. **Branch**: Create a feature or bugfix branch off `main`:
   ```bash
   git checkout -b feat/my-new-feature
   # or
   git checkout -b fix/issue-description
   ```
3. **Commit Messages**: Write clear, descriptive commit messages following the Conventional Commits specification:
   - `feat: ...` for new capabilities or flags.
   - `fix: ...` for bug fixes.
   - `perf: ...` for performance enhancements.
   - `docs: ...` for documentation updates.
   - `test: ...` for adding or improving test coverage.
   - `refactor: ...` for code refactoring without behavior changes.
4. **Self-Review**:
   - Run `go vet ./...`
   - Run `golangci-lint run ./...`
   - Run `go mod verify`
   - Run `govulncheck ./...`
   - Run `go test -race -count=1 ./...`
   - Ensure new functionality has accompanying tests.
5. **Open a Pull Request**: Submit your pull request against the `main` branch. Fill out the pull request template completely.
