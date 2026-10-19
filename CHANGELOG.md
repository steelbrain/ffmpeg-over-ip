# Changelog

All notable changes to this project are documented here.

Releases prior to `v5.0.0` predate this file; see the
[GitHub Releases](https://github.com/steelbrain/ffmpeg-over-ip/releases) page for
the `v1`–`v4` history.

## Unreleased

## 6.0.0 - 2026-10-19

v6 moves the bundled ffmpeg up a major version, from FFmpeg 7.1 to 8.1. The
ffmpeg-over-ip wire protocol, configuration, and install layout are unchanged,
and v5 clients and servers need no config changes. Command lines (including
server-side `rewrites`) that depend on options, codecs, or defaults FFmpeg 8
changed or removed may need updating.

### Changed

- **Breaking: bundled ffmpeg upgraded to jellyfin-ffmpeg 8.1.3** — the server's
  patched `ffmpeg`/`ffprobe` move from jellyfin-ffmpeg 7.1.3-3 (FFmpeg 7.1.3)
  to 8.1.3-2 (FFmpeg 8.1.3) on all platforms. The fio patches are rebased onto
  the new source; 8.1 introduces no new raw file syscalls that need tunneling.
  Minimum platforms are unchanged: glibc 2.28 on Linux, macOS 12.0.
- The experimental `sonic`/`sonicls` encoders are no longer included
  (jellyfin-ffmpeg now builds with `--disable-unstable`).

### Added

- **More hardware acceleration in the bundled ffmpeg** — the AMF hwaccel and
  Vulkan AV1/FFV1/ProRes encoders on Linux x86_64 and Windows; VAAPI on Linux
  arm64; Media Foundation and D3D12 encoders on Windows.

### Fixed

- `build-release.sh` now fails immediately for `win64` when the host has no
  `zip`, instead of after the full container build.

### CI / Testing

- `test-simple-transcode.sh` now actually checks ffprobe reading through fio.
  It passed `-pix_fmt`, which ffprobe rejects, and only passed by matching the
  minimal build's configure banner.

## 5.2.1 - 2026-06-14

### Performance

- **Read-ahead, async prefetch, and range cache for tunneled fio reads** (#24) —
  the server-side fio layer now batches reads, prefetches the next block while
  the current one is consumed, and caches recently-read ranges, cutting
  request/response round trips for readers like FFmpeg's AVIO layer that pull
  ~32 KiB at a time. Reduces over-IP overhead on sequential workloads from
  roughly 8–14× slower than local to under 2×. Tunable via the
  `FFOIP_READAHEAD_BYTES` and `FFOIP_RANGE_CACHE_BYTES` environment variables
  (set `FFOIP_READAHEAD_BYTES=0` to disable). No protocol or configuration
  changes are required.

### Fixed

- `build-release.sh` host scripts now run on macOS.

## 5.2.0 - 2026-05-20

### Added

- **Client fallback to local ffmpeg on dial failure** (#16, #18) — when the
  remote server is unreachable, the client can transparently run the host's
  local `ffmpeg`/`ffprobe` so transcoding (Jellyfin, etc.) keeps working. Opt-in
  via `fallbackToLocal` in client config or
  `FFMPEG_OVER_IP_CLIENT_FALLBACK_TO_LOCAL=true`.
  - Triggers only on dial failure; mid-session errors stay fatal.
  - Local binary resolved via `$PATH` with self-exclusion to prevent recursion
    when the client is installed as `ffmpeg` on `PATH`.
  - `FFMPEG_OVER_IP_*` env vars are stripped from the child so the auth secret
    can't leak via `/proc/<pid>/environ`.
  - Optional `fallbackRewrites` (shared rewrite logic), `debug` arg logging, and
    cross-platform exit-code mapping.

### Changed

- **Argv-aware rewrites** (#21) — the rewrite engine now matches whole argv
  elements (with multi-token support) instead of substring replacement within
  each element, enabling GPU-vendor translation such as
  `-hwaccel qsv` → `-hwaccel cuda -hwaccel_output_format cuda`.
  - **Breaking:** configs that relied on substring rewriting inside args (e.g.
    `["nvenc", "qsv"]` to turn `h264_nvenc` into `h264_qsv`) must be updated to
    whole-element form (`["h264_nvenc", "h264_qsv"]`).
- `config.SetupLogging` returns a cleanup function; the client and server now
  close the log file on shutdown instead of leaking the handle (also required
  for correct Windows behavior).

### Fixed

- **Install scripts: allow 32-bit PowerShell on 64-bit Windows** (#15, #9) —
  switched the arch check to `[Environment]::Is64BitOperatingSystem`, which is
  not fooled by WoW64.
- **Install scripts: handle both flat and wrapped zip layouts** so older nested
  and newer flat release archives both install correctly.
- `filehandler.mapErrno` now falls back to `errors.Is` against the `fs` sentinel
  errors, catching Windows error codes (e.g. `ERROR_FILE_NOT_FOUND`) that don't
  share numeric values with POSIX errnos. Unix behavior is unchanged.

### CI / Testing

- Release workflow auto-attaches per-platform build zips and an aggregated
  `SHA256SUMS` to GitHub Releases on `v*` tags.
- The test suite is now genuinely cross-platform and runs on `windows-latest`
  (#20); added an end-to-end integration test for the fallback path.

## 5.1.0 - 2026-05-04

### Added

- **One-line install scripts** for client and server (#9) — `curl | sh` on
  Linux/macOS and `irm | iex` on Windows. The scripts download the latest
  release, prompt for `address` and `authSecret`, and generate the config file.
  Idempotent re-runs (set `FOIP_FORCE=1` to re-download); macOS quarantine and
  Windows MOTW attributes are stripped automatically.
- **Environment-variable configuration** (#13) — both client and server can be
  configured entirely via `FFMPEG_OVER_IP_{CLIENT,SERVER}_*` variables
  (`ADDRESS`, `AUTH_SECRET`, `LOG`, and `SERVER_DEBUG`), with no config file
  needed. Useful for Docker and scripted deployments. (`rewrites` still requires
  a config file.)

### Changed

- Install-script prompts now show `[default=5050]` instead of `[5050]` so the
  Enter-to-accept value is unambiguous.

### Compatibility

- Fully backwards-compatible — existing config-file deployments work unchanged;
  the env-var mode is opt-in.

## 5.0.1 - 2026-04-21

### Fixed

- **`pipe:` / `fd:` protocol now works** (#8, #10) — ffmpeg's `pipe:0`/`pipe:1`
  handlers pass raw stdio fds through the fio layer, which previously rejected
  anything below `FIO_VFD_BASE` with `EBADF`. fio now passes real kernel fds
  straight through to the syscall, and stdio is tunneled back to the client as
  before.

## 5.0.0 - 2026-03-10

v5 completely replaces the shared-filesystem architecture from v4. The server
now runs a patched ffmpeg that tunnels all file I/O back to the client over a
single TCP connection — no NFS, no SMB, no shared mounts.

### Added

- **No shared filesystem** — file reads and writes are tunneled over the
  connection, eliminating NFS/SMB setup, path mapping, and mount maintenance.
- **Pre-built patched ffmpeg binaries** — releases include patched `ffmpeg` and
  `ffprobe` with broad hardware-acceleration support (NVENC, QSV, VAAPI, AMF,
  VideoToolbox, and more), built on
  [jellyfin-ffmpeg](https://github.com/jellyfin/jellyfin-ffmpeg) 7.1.3.
- **Cross-platform binaries** — Linux x86_64/arm64, macOS arm64/x86_64, and
  Windows x86_64 (Windows arm64 is client-only).
- **Unix domain sockets** — use `unix:/path` for same-machine setups without TCP
  overhead.
- **JSONC config** — `//` and `/* */` comments and trailing commas, with config
  search across 8 locations.
- **HMAC-SHA256 authentication** — every command is signed with a shared secret.

### Changed

- **Breaking:** complete rearchitecture from v4's shared-filesystem model. See
  the [upgrade guide](https://github.com/steelbrain/ffmpeg-over-ip/blob/main/docs/upgrading.md)
  for breaking changes and a migration checklist.
