# Changelog

All notable changes to this project are documented here.

Releases prior to `v5.0.0` predate this file; see the
[GitHub Releases](https://github.com/steelbrain/ffmpeg-over-ip/releases) page for
the `v1`–`v4` history.

## Unreleased

## 7.0.0 - 2026-11-15

v7 changes the wire protocol to version 0x07, so the client and server must be
upgraded together: a v7 client can't use a v6 server, and a v6 client can't
use a v7 server. The install scripts download the latest release, so running
one on either machine installs v7 there; upgrade the other machine too.
Configuration and the install layout are unchanged. The bundled ffmpeg is still
jellyfin-ffmpeg 8.1.3-2 on every platform, rebuilt with the fio changes below;
its codecs, formats, filters and hardware acceleration are the same as in
6.0.0.

### Changed

- **Breaking: wire protocol version 0x07.** The new handshake, stdin flow
  control and the Stat and ListDir requests change the protocol, so the client
  and server must run the same release. A 0x06 client gets `server error:
  invalid command: protocol version mismatch: client sent 0x06, server expects
  0x07; client and server must run the same ffmpeg-over-ip release`, and a new
  client talking to a 0x06 server gets `server error: expected command message
  (0x01), got 0x07 (the server runs a different ffmpeg-over-ip release; client
  and server must run the same release)`. Upgrade the client and server
  together.

### Security

- **The server proves it knows the secret.** Before, only the client proved
  it, so anything listening at the server's address could send the client file
  requests (read, write or delete any file the client's user can reach). The
  client now checks the server's proof before it acts on anything. The server
  sends its proof only after checking the client's, so connecting gives
  nobody a value to test guesses of the secret against. The handshake adds
  one network round trip before each ffmpeg starts.
- **A recorded command can't be replayed.** The command signature now covers a
  nonce the server picks for each connection. Before, a captured command could
  be sent again to run the same ffmpeg command line on the server.
- **Other local processes can't take over ffmpeg's tunnel.** ffmpeg's fio
  layer connected to an unauthenticated loopback port, so a local process that
  connected first received ffmpeg's file requests and could answer them. The
  server now gives each ffmpeg a random token that fio must send first, and
  closes any connection without it.
- **ffmpeg never falls back to the server's own files.** When fio could not
  connect to the server's loopback port, it silently used the server's disk
  for the paths on the client's command line: inputs were read, and outputs
  written or deleted, on the server. It now fails those operations with an
  I/O error.
- **Unauthenticated connections are limited.** Each handshake must finish
  within 10 s, its messages are limited to 1 MiB and buffered only as bytes
  arrive, and all unauthenticated connections together may buffer at most
  64 MiB, those from one IP address (one /64 for IPv6) at most 16 MiB. Before,
  a peer could announce a 100 MiB message and the server allocated it up
  front. A connection holds no buffer until its bytes arrive and at most
  16 KiB more than it has sent, so idle connections don't hold up real
  clients, and one host can't fill the buffer for everyone. A client turned
  away because a limit is reached gets `server busy`.

### Added

- **Subtitle files and fonts are read from the client.** The `subtitles` and
  `ass` filters read the subtitle file and every font in `fontsdir` through
  the tunnel. Before, libass opened them itself on the server's disk: the
  `ass` filter failed with `Invalid argument`, and `fontsdir` fonts were never
  loaded, so text rendered in a fallback font. Font loading follows libass's
  own rules for a fonts directory (no recursion, names starting with `.`
  skipped). Named pipes and devices in `fontsdir` are skipped, where libass
  would block reading them; a symbolic link to one is still opened, as libass
  does.
- **Path checks and directory listings run on the client.** New Stat and
  ListDir requests back `avio_check` and `avio_open_dir` for local paths, and
  image2's `-ts_from_file`. Before, these looked at the server's disk:
  image-sequence inputs failed with `Could find no file or sequence`, `-n`
  overwrote an existing output file on the client instead of refusing, and
  `-ts_from_file` failed. Tunneled modification times have one-second
  resolution.

### Performance

- **Requests from ffmpeg no longer wait on Nagle's algorithm.** fio sent each
  request as two writes on a socket with Nagle's algorithm on, so a request
  could wait for the server to acknowledge its first part, up to 40 ms on
  Linux. With the server on Linux, seeking 40 times into a 185 MB Matroska
  file on the client took 662–672 ms per seek before and 32–33 ms now, and
  reading 12 MiB from a named pipe on the client took 70 s and now takes
  0.5 s. Copying a 1 GiB file over localhost on macOS went from 1858–1926
  MiB/s to 2084–2148 MiB/s.

### Fixed

- **Stdin can no longer deadlock a session.** Stdin and the replies to
  ffmpeg's file requests share one connection. When ffmpeg wasn't reading
  stdin while waiting for a reply, the server blocked writing stdin and
  stopped relaying replies, and the transcode hung. The server now writes
  stdin from a separate queue, and the client keeps at most 4 MiB of stdin
  unacknowledged.
- **A peer that vanishes is detected even while a write is stuck.** When the
  other side disappeared without closing the connection (power loss, network
  gone), a write blocked on it also blocked the keepalive check, so the
  150 s timeout never fired and ffmpeg kept running. Both client and server
  now time out after 150 s, the client also when its own write to the server
  has been stuck that long. Ctrl-C on a client stuck that way now exits
  within 5 s.
- **Two-pass encoding works.** Pass 1 wrote ffmpeg's pass log with `fopen` on
  the server while pass 2 read it from the client, so `-pass 1` failed with
  `Cannot write log file`. The log is now written through the tunnel. libx264,
  libx265 and libvvenc write their own stats files from inside the encoder
  library; those files still go to the server's disk.
- **ffmpeg on the server stops when the client goes away.** A client that is
  killed, or whose connection is closed, now stops its ffmpeg at once; before,
  ffmpeg ran on until the 150 s keepalive timeout, which still applies to a
  connection that drops without closing. Stopping the server (SIGINT or
  SIGTERM) stops every running ffmpeg and waits for them, and the client gets
  ffmpeg's exit code. ffmpeg gets SIGTERM with its file access intact, so it
  can still finish its outputs (an MP4's index, for example). On Linux and Windows, ffmpeg is also killed if the
  server itself dies (a crash, `kill -9`, `taskkill /F`); Linux uses the
  parent-death signal, Windows a job object. On Windows, a stopped ffmpeg is
  killed right away instead of after the 5 s grace period meant for SIGTERM.
- **The client gets ffmpeg's exit code when ffmpeg stops reading stdin
  early.** The server closed the connection while stdin from the client was
  still unread, which resets the connection on Linux and could destroy the
  exit code before the client read it; the client then exited with 1. The
  server now closes its side and waits for the client to hang up.
- **The client no longer times out a healthy server while it is busy.** The
  150 s keepalive timeout also counted time the client spent writing to a
  slow stdout reader or waiting on a slow file; only time spent waiting for
  the server counts now.
- **Keepalive timeouts no longer follow the wall clock.** Client and server
  measured them with the system clock, so setting it forward by 150 s or
  more (an NTP correction, a VM resuming) dropped healthy connections. They
  use the monotonic clock now.
- **Windows: the client exits when its stdout reader goes away**, so the
  server stops ffmpeg instead of running it to the end. Unix clients already
  did, through SIGPIPE.
- **The server no longer floods its log when out of file descriptors.** It
  retried accepting in a tight loop, logging each failure; it now backs off.
  The same errors no longer stop ffmpeg's tunnel from being accepted.
- **The fallback ffmpeg no longer outlives the client on Linux and Windows.**
  When the client runs the local ffmpeg (`fallbackToLocal`) and is then
  killed (`kill -9`, `taskkill /F`, Task Manager, or a parent process killing
  it), that ffmpeg is now killed too.
- **Named pipes on the client are reported as pipes.** fio reported every
  tunneled file as a regular file, so ffmpeg treated a FIFO as seekable; it
  now sees a pipe, as it does locally. Listing a FIFO as a directory no
  longer hangs the client.
- **A write that fails partway (disk full) no longer misplaces later writes.**
  The client now reports the bytes it did write, and fio seeks before the next
  write.
- **More file errors keep their meaning.** `EXDEV`, `ENAMETOOLONG`,
  `ENOTEMPTY`, `ELOOP` and `EBUSY` from the client used to reach ffmpeg as a
  generic I/O error. On Windows clients, a full disk is reported as `ENOSPC`,
  a sharing violation as `EACCES`, and a directory with the read-only
  attribute as writable (Windows ignores that attribute on directories).
- **`pipe:` and `fd:` with descriptor numbers of 10000 or more work while
  tunneled.** fio took them for its own virtual descriptors.
- **fio no longer hangs or fails under load.**
  - A reply could be delivered to the wrong request once the 16-bit request id
    wrapped around while a prefetch was outstanding, leaving that request
    waiting forever.
  - With more than 64 files open, reads could fail with `Cannot allocate
    memory`: each open file can hold a prefetch, and there were only 64
    request slots.
  - A single read or write of about 100 MiB or more exceeded the message size
    limit and ended the session. fio now sends at most 16 MiB per request and
    returns a short count, which ffmpeg retries.
  - If the connection to the server closes, requests in flight fail with EIO
    instead of blocking, and later requests fail at once.
  - Once the 16-bit file id counter wrapped around, a new file could get the
    id of a file that was still open. The open-file table was also not safe
    across ffmpeg's threads.
- **`FFOIP_RANGE_CACHE_BYTES` bounds the whole ffmpeg process.** It was
  applied to each open file separately, so the cache could grow by that
  amount for every file ffmpeg had open.
- **The client fills each read of a regular file.** Storage that returns short
  reads mid-file no longer turns one tunneled read into several round trips.
- **fio no longer prefetches past the end of a file.** When the read cache
  already reached the known file size, the fio layer still sent a read for the
  full read-ahead window (512 KiB to 2 MiB by default) starting at EOF.
  Demuxers that read an index at the end of a file and then seek away (mp4
  `moov`, Matroska cues) paid for a READ round trip that could only return zero
  bytes. Files that grow while open still prefetch once a read has returned
  data past the size seen at open.
- **A short read no longer shrinks the file size fio reports.** fio treated any
  READ reply shorter than requested as end of file and lowered the cached file
  size to match, which `fstat` (ffmpeg's `AVSEEK_SIZE`) and `SEEK_END` then
  reported. Storage that returns short reads mid-file, such as some network or
  FUSE mounts, could make ffmpeg see a truncated file. Only an empty reply now
  marks end of file.

### CI / Testing

- `TestErrnoMapping` no longer expects `syscall.ENOTDIR` to map to `ENOTDIR`
  on Windows, where Go defines it as `ERROR_PATH_NOT_FOUND` (a missing parent
  directory, mapped to `ENOENT`). It failed in the Windows Go test job; Windows
  now checks `ERROR_DIRECTORY` → `ENOTDIR` instead.

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
