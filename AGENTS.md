# AGENTS.md

Guidance for coding agents working in this repository.

## Project Shape

`ffmpeg-over-ip` lets a client machine run a local-looking `ffmpeg` or `ffprobe` command on a remote server that has GPU access. The server runs a patched FFmpeg binary; the patch redirects FFmpeg file operations through a C `fio` layer, and the Go client performs those file operations against the client's local filesystem.

High-level flow:

1. `cmd/client` loads client config, signs the command with HMAC, connects to the server, forwards stdin/stdout/stderr, and services tunneled file I/O requests.
2. `cmd/server` authenticates the command, applies configured argv rewrites, launches same-directory `ffmpeg` or `ffprobe`, and runs a session.
3. `internal/process` starts patched FFmpeg with `FFOIP_PORT` pointing at a local loopback listener.
4. `fio/` connects to that loopback port when `FFOIP_PORT` is set and sends file-operation messages.
5. `internal/session` bridges FFmpeg loopback I/O, stdio, cancellation, keepalives, and exit codes over the client/server connection.
6. `internal/filehandler` executes the file operations on the client filesystem.

## Key Directories

- `cmd/client/` - drop-in client binary for `ffmpeg` and `ffprobe`; includes local fallback behavior.
- `cmd/server/` - daemon that launches patched `ffmpeg`/`ffprobe`.
- `internal/protocol/` - wire protocol types, message envelopes, and encoders/decoders.
- `internal/session/` - connection/session multiplexing between TCP, process pipes, and fio loopback.
- `internal/process/` - child process lifecycle and `FFOIP_PORT` loopback setup.
- `internal/filehandler/` - local filesystem implementation of remote file requests.
- `internal/config/` - JSONC/env config loading, logging setup, address parsing.
- `internal/auth/` - HMAC-SHA256 command signing and verification.
- `internal/rewrite/` - ordered argv rewrite rules.
- `fio/` - C tunneling layer patched into FFmpeg. GPL v3.
- `patches/` - patches applied to Jellyfin FFmpeg. GPL v3.
- `third_party/jellyfin-ffmpeg/` - upstream Jellyfin FFmpeg submodule; avoid editing directly unless the task explicitly requires it.
- `scripts/` - build and install scripts.
- `tests/integration/` - end-to-end shell tests requiring patched FFmpeg.
- `tests/static-analysis/` - scan for raw file syscalls in patched FFmpeg.

## Build And Test Commands

Common checks:

```sh
go build ./...
go test ./internal/... ./cmd/... -race -count=1 -timeout 120s
```

C fio tests:

```sh
cd fio
make test
```

Build minimal patched FFmpeg for integration testing:

```sh
bash scripts/build-ffmpeg.sh --minimal
```

Run integration tests after patched FFmpeg exists at `build/ffmpeg/bin/ffmpeg`:

```sh
bash tests/integration/test-client-server.sh
for t in tests/integration/test-*.sh; do bash "$t"; done
```

Static syscall audit after patching/building FFmpeg:

```sh
python3 tests/static-analysis/scan-raw-syscalls.py
```

## Development Notes

- Preserve the protocol contract between Go and C. Message type values, field order, endian encoding, errno constants, and request/response payload layouts must stay in sync between `internal/protocol/wire.go` and `fio/fio.c`.
- If protocol layout changes, update Go protocol tests, C fio tests, integration tests, and any wire/vector helpers under `tests/wire`.
- The client must remain an invisible `ffmpeg`/`ffprobe` proxy. Avoid writing diagnostics to stdout/stderr paths that belong to the proxied process unless existing behavior already does so intentionally. Prefer configured logging.
- `ffprobe` mode is detected from the client executable name containing `ffprobe`.
- Server `ffmpeg` and `ffprobe` are resolved from the same directory as `ffmpeg-over-ip-server`.
- Fallback-to-local only happens on initial dial failure. Do not silently restart partially established sessions locally.
- Keep file I/O behavior cross-platform. The wire protocol uses canonical flags, whence values, and Linux-style errno constants; translate at boundaries.
- Be careful with raw file syscalls in FFmpeg patches. File operations that should follow client-local paths need `fio_*`; allowed raw syscall uses should be clearly justified and marked consistently with the static-analysis expectations.
- Avoid unnecessary churn in `third_party/jellyfin-ffmpeg/`. Build scripts copy or patch it as needed.
- Respect the split license: `fio/` and `patches/` are GPL v3; most Go code and docs are MIT.

## Config And Runtime

- Config files are JSONC.
- Config resolution supports explicit server `--config`, config env vars, individual address/auth env vars, and search paths documented in `docs/configuration.md`.
- Address strings are either TCP `host:port` or Unix sockets prefixed with `unix:`.
- Rewrites match whole argv elements, with multi-token find/replace strings split on whitespace.
- Env-var config does not support rewrite arrays.

## When Changing Behavior

- Start from the focused unit tests for the package being changed.
- Run broader tests when touching shared contracts: protocol, session, process lifecycle, config loading, filehandler, `fio/`, or FFmpeg patches.
- For anything involving the patched FFmpeg path, build minimal FFmpeg and run at least one integration test that exercises both reading and writing through fio.
- Keep changes scoped. This repository has many generated/vendor/upstream files; do not reformat or regenerate unrelated content.
