# Changelog

All notable changes follow [Conventional Commits](https://www.conventionalcommits.org/).
Releases are tagged `vX.Y.Z`; the CI pipeline builds and publishes them automatically.

## [Unreleased]

## [v0.4.1] - 2026-08-04

### Fixed

- Graph cache now invalidates on any `mtime` change (including file restorations),
  not just when the timestamp increases.
- `ShortestPath` now respects the `max_hops` limit; path queries honor the hop bound.
- Propagate cleanup errors in incremental graph sync (`updateFileMeta`, `upsertNode`)
  and wiki export instead of silently ignoring them.
- `sv-memory update` now compares versions semantically, preventing accidental
  downgrades when the installed version is newer than the latest release.

### Changed

- Migrated `golangci-lint` to configuration v2 and pinned `v2.12.2` in CI.
- Raised the `gocyclo` threshold to 30 and marked monolithic extractor/export
  functions with justified `//nolint:gocyclo` comments.
- Removed the `only-new-issues` lint gate: the full lint now runs on every build.
- Aligned the release workflow Actions (`checkout@v7`, `setup-go@v7`) with CI.

### Added

- Behavioral tests for the TUI renderers/banner and CLI helpers (config,
  permissions, and updater), raising global statement coverage to 63%.

### Docs

- Synchronized `documentation/spect.md`, READMEs, and CHANGELOG with the current
  implementation.

## [v0.4.0] - 2026-08-03

### Added

- TUI brand banner with the SV Memory logo, dynamic width, and centered text.
- Configure wizard "shortcuts summary" step that documents the keyboard shortcuts,
  and removal of the redundant "Cancel configuration (exit)" sentinel option
  (cancellation is now `Ctrl+C` or the step-3 `No, cancel` confirmation).

### Changed

- Migrated the interactive TUI from `bufio` to `charmbracelet/huh`, aligning its
  banner color and theme with the `configure` wizard.
- Compacted `sv_mem_search` / `sv_mem_timeline` MCP output and lowered the default
  `max_chars` to save tokens.

### Fixed

- Use pure hex IDs and migrate graph tables during project consolidation.

### Performance

- Cache git branch/commit/author metadata for 30s in `sv_mem_save` to avoid up to 4
  git subprocess spawns per save.

## [v0.3.0] - 2026-08-02

### Added

- `sv-memory update` command: checks GitHub Releases for a newer version, asks for
  confirmation, downloads the platform binary, verifies its SHA-256 checksum against
  `checksums.txt`, and atomically replaces the running executable (on Windows it
  prints a manual `copy` command since the running .exe cannot be overwritten).

### Changed

- The `sv-memory configure` wizard now uses the SV Tech banner color (`#00B0C2`) for its
  borders, titles, descriptions, selectors, and buttons while keeping the green
  selection indicator. The Esc key now goes back to the previous step, and every screen
  shows its keyboard shortcuts (including `Ctrl+C` to exit).

## [v0.2.0] - 2026-08-02

### Added

- `sv-memory version` command that prints the build version, commit, and Go runtime.
  The version is injected at build time via `-ldflags`, so release binaries report the
  tag they were built from.
- Interactive `sv-memory configure` wizard built on `charmbracelet/huh`: arrow keys
  navigate, space toggles multi-select, Enter advances, and Esc goes back between
  steps. The banner now shows the real version instead of a hardcoded value.

## [v0.1.0] - 2026-08-02

### Added

- CI pipeline (`.github/workflows/ci.yml`): `go vet`, tests with the race detector, and a
  build check on `ubuntu-latest` + `macos-latest`.
- Release pipeline (`.github/workflows/release.yml`): cross-compiles macOS, Linux, and
  Windows binaries on a `v*` tag push and publishes them as GitHub Releases with checksums.
- `install.sh` (macOS/Linux): installs a prebuilt binary to `$HOME/.local/bin` without
  `sudo`, printing a PATH hint when the directory is not on the user's PATH.
- `install.ps1` (Windows): installs a prebuilt binary to `%LOCALAPPDATA%\sv-memory` and
  adds it to the user PATH.
- `make release` target to cross-compile release artifacts locally into `dist/`.
- `CHANGELOG.md` to track releases.

### Changed

- READMEs and the getting-started guide now document the one-line `curl`/`iwr` install
  commands and the new no-`sudo` install location.

### Security

- Generate memory/session/relation IDs with 64 bits of entropy (`newID()`) instead of a
  32-bit `uuid[:8]` prefix, preventing silent overwrites via `INSERT ... ON CONFLICT`.
- Harden FTS5 search so quotes-only queries return zero results instead of raising a
  syntax error, and move LIKE wildcard escaping into the memory layer with a length cap.
- Bound git helper commands with a 5s timeout so a stalled `git` process cannot block
  `sv-memory save`.

### Performance

- Make the memory conflict scan incremental (O(new memories × total) instead of O(N²)),
  cache tokenizations, and stop early once the insert budget is reached.
- De-N+1 `sv_mem_review` (single grouped relation-count query) and surface review-worthy
  memories first, bounded by `default_review_limit`.

[v0.1.0]: https://github.com/svtech-code/sv-memory/releases/tag/v0.1.0
[v0.2.0]: https://github.com/svtech-code/sv-memory/releases/tag/v0.2.0
[v0.3.0]: https://github.com/svtech-code/sv-memory/releases/tag/v0.3.0
[v0.4.0]: https://github.com/svtech-code/sv-memory/releases/tag/v0.4.0
[v0.4.1]: https://github.com/svtech-code/sv-memory/releases/tag/v0.4.1
[Unreleased]: https://github.com/svtech-code/sv-memory/compare/v0.4.1...main
