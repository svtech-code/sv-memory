# Changelog

All notable changes follow [Conventional Commits](https://www.conventionalcommits.org/).
Releases are tagged `vX.Y.Z`; the CI pipeline builds and publishes them automatically.

## [Unreleased]

### Added

- `sv-memory version` command that prints the build version, commit, and Go runtime.
  The version is injected at build time via `-ldflags`, so release binaries report the
  tag they were built from.
- Interactive `sv-memory configure` wizard built on `charmbracelet/huh`: arrow keys
  navigate, space toggles multi-select, Enter advances, and Esc goes back between
  steps. The banner now shows the real version instead of a hardcoded value.
- `sv-memory update` command: checks GitHub Releases for a newer version, asks for
  confirmation, downloads the platform binary, verifies its SHA-256 checksum against
  `checksums.txt`, and atomically replaces the running executable (on Windows it
  prints a manual `copy` command since the running .exe cannot be overwritten).

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
