# Upgrading

This guide covers what changed for users in each release since v4, newest first. To skip releases, read every section between your version and the newest. You don't need to install the releases in between: install the newest one and follow the checklist in each of those sections.

| You run | Read |
|---|---|
| 6.0.0 | [v6 to v7](#v6-to-v7) |
| 5.2.0 or 5.2.1 | [v5 to v6](#v5-to-v6), then [v6 to v7](#v6-to-v7) |
| 5.0.x or 5.1.0 | [Within v5](#within-v5), [v5 to v6](#v5-to-v6), then [v6 to v7](#v6-to-v7) |
| v4 | [v4 to v5](#v4-to-v5), then every section above it |

The [CHANGELOG](../CHANGELOG.md) lists every change in each release.

## Installing a new release

Upgrading replaces binaries. Config files and environment variables keep working: no release since 5.0.0 has removed or renamed a setting, though 5.2.0 changed how `rewrites` match (see [Within v5](#within-v5)).

Upgrade the server while it has no transcodes running. Stop it before you replace its files and start it again afterwards; until it restarts, it keeps running the old version.

### With the install scripts

The install scripts always install the latest release, into the current directory. If the binary is already there, they skip the download unless `FOIP_FORCE` is set. Run them in the directory of the existing install.

Server, on Linux or macOS:

```sh
curl -fsSL https://ffmpeg-over-ip.com/install-server.sh | FOIP_FORCE=1 sh
```

Client, on Linux or macOS:

```sh
curl -fsSL https://ffmpeg-over-ip.com/install-client.sh | FOIP_FORCE=1 sh
```

Server, on Windows (PowerShell):

```powershell
$env:FOIP_FORCE = '1'
irm https://ffmpeg-over-ip.com/install-server.ps1 | iex
```

Client, on Windows (PowerShell):

```powershell
$env:FOIP_FORCE = '1'
irm https://ffmpeg-over-ip.com/install-client.ps1 | iex
```

With `FOIP_FORCE` set, the script overwrites the files in the release archive and leaves everything else in the directory alone:

| Archive | Files |
|---|---|
| Server | `ffmpeg-over-ip-server`, `ffmpeg`, `ffprobe`, `LICENSE`, `LICENSE.ffmpeg` |
| Client | `ffmpeg-over-ip-client`, `LICENSE`, `LICENSE.ffmpeg` |

On Windows the binaries end in `.exe`. After extracting, the script handles the config file and `ffprobe` like this:

- An existing `ffmpeg-over-ip.server.jsonc` or `ffmpeg-over-ip.client.jsonc` in the directory is kept. If there is none, because your config is elsewhere or in environment variables, the script asks for an address and secret and writes one. Upgrade by hand if you don't want that file.
- An existing `ffprobe` is kept. On Linux and macOS the client script created it as a symlink to `ffmpeg-over-ip-client`, so it runs the new client. On Windows it created `ffprobe.exe` as a copy of the client, and that copy stays at the old version: delete `ffprobe.exe` before you run the script, and the script makes a new copy.

### By hand

Download the archives for your platform from the [releases page](https://github.com/steelbrain/ffmpeg-over-ip/releases): `<platform>-ffmpeg-over-ip-server.zip` for the server and `<platform>-ffmpeg-over-ip-client.zip` for the client, where `<platform>` is `linux-amd64`, `linux-arm64`, `macos-arm64`, `macos-amd64` or `windows-amd64`. The release's `SHA256SUMS` file has their checksums.

- **Server**: replace `ffmpeg-over-ip-server`, `ffmpeg` and `ffprobe` with the three files from the new archive. All three must come from the same release and stay in the same directory.
- **Client**: replace `ffmpeg-over-ip-client` and every copy of it, such as an `ffprobe` or `ffprobe.exe` made with `cp` or `copy`, or a copy installed under the name `ffmpeg`. Symlinks to the client need no change.
- On macOS, downloaded binaries may be quarantined; remove the attribute as shown in [Troubleshooting](troubleshooting.md).
- If containers mount the binaries, as in [Docker Integration](docker.md), replace the files on the host and then restart the containers.

### Check the result

On each client machine, run:

```bash
./ffmpeg-over-ip-client -version
```

When the client and server work together, this prints the server's ffmpeg version and build info. Otherwise it prints an error, such as the version mismatch errors in [v6 to v7](#v6-to-v7).

## v6 to v7

7.0.0 changes the wire protocol from version 0x06 to 0x07. Config files, environment variables and the install layout are the same as in v6. The bundled ffmpeg is still jellyfin-ffmpeg 8.1.3-2, rebuilt with v7's changes to the fio layer.

### What breaks

A client and a server from different releases no longer work together, so upgrade the server and every client at the same time. Transcodes fail from the moment you upgrade the first side until you have upgraded the other.

A v5 or v6 client connecting to a v7 server prints:

```
server error: invalid command: protocol version mismatch: client sent 0x06, server expects 0x07; client and server must run the same ffmpeg-over-ip release
```

A v7 client connecting to a v5 or v6 server prints:

```
server error: expected command message (0x01), got 0x07 (the server runs a different ffmpeg-over-ip release; client and server must run the same release)
```

The client exits with code 1 in both cases. `fallbackToLocal` doesn't cover this: the client runs the local ffmpeg only when it can't connect to the server at all.

The server's `ffmpeg` and `ffprobe` must be the v7 builds too. The v7 server gives each ffmpeg it starts a random token, and only accepts ffmpeg's file tunnel after ffmpeg sends the token back. The v6 builds don't send it.

### What else changes

- Each ffmpeg or ffprobe run starts one network round trip later. The client and server now each prove they know the secret before the command runs.
- `FFOIP_RANGE_CACHE_BYTES` now limits the range cache of the whole ffmpeg process. In v6 the limit applied to each open file separately. If you set it, check the value against the new meaning. See [Performance Tuning](configuration.md#performance-tuning).
- Two-pass encoding works. ffmpeg's pass log is written to the client through the tunnel; in v6, `-pass 1` failed with `Cannot write log file`. The stats files that libx264, libx265 and libvvenc write themselves still go to the server's disk, as listed in [Files opened on the server](troubleshooting.md#files-opened-on-the-server).
- The `subtitles` and `ass` filters read the subtitle file and the fonts in `fontsdir` from the client. In v6 they were opened on the server's disk, so if you copied these files to the server, they now have to exist on the client at the path the command uses. System fonts that libass finds through fontconfig still come from the server.
- Path checks and directory listings run on the client. `-n` now refuses to overwrite an output file that exists on the client; in v6 it checked the server's disk and overwrote the client's file. Image-sequence inputs and image2's `-ts_from_file` now work.
- Killing a client, or closing its connection, stops its ffmpeg on the server at once. Stopping the server with SIGINT or SIGTERM stops every running ffmpeg and waits for them to exit.
- The server limits how much data connections that haven't finished the handshake can make it buffer. A client turned away by that limit gets `server busy`; see [Troubleshooting](troubleshooting.md).

### Use a long random secret

Since you are changing both sides anyway, this is a good time to replace a short or guessable secret. Someone who records a session can test guesses of the secret against it offline, so the [Security](../README.md#security) section recommends a long random secret, such as the output of:

```bash
openssl rand -hex 32
```

Put the same value in `authSecret`, or in `FFMPEG_OVER_IP_SERVER_AUTH_SECRET` and `FFMPEG_OVER_IP_CLIENT_AUTH_SECRET`, on the server and on every client. The install scripts don't change an existing config file, so edit it yourself.

### Checklist

1. Stop the server, replace `ffmpeg-over-ip-server`, `ffmpeg` and `ffprobe` with the v7 files, set a new secret if you are changing it, and start the server.
2. On every client machine, replace `ffmpeg-over-ip-client` and every copy of it (on Windows, including `ffprobe.exe`), and set the new secret if you changed it.
3. Run `ffmpeg-over-ip-client -version` on every client machine. It should print `ffmpeg version 8.1.3-Jellyfin` and the build info, not one of the errors above.
4. If you set `FFOIP_RANGE_CACHE_BYTES`, check that the value suits a limit for the whole ffmpeg process.

## v5 to v6

6.0.0 moves the bundled `ffmpeg` and `ffprobe` from jellyfin-ffmpeg 7.1.3-3 (FFmpeg 7.1.3) to 8.1.3-2 (FFmpeg 8.1.3) on every platform. Nothing else changed for users: the client and server code is the same as in 5.2.1, and v5 config files work unchanged. Minimum platforms are also unchanged: glibc 2.28 on Linux and macOS 12.0.

Every 5.x release and 6.0.0 use wire protocol version 0x06 with the same messages, so their clients and servers work with each other, and you can upgrade one machine at a time.

### What breaks

- Commands that depend on options, codecs or defaults that FFmpeg 8 changed or removed. This covers the commands your media server sends and the server's `rewrites`, which produce part of the final command line.
- The experimental `sonic` and `sonicls` encoders are no longer included, because jellyfin-ffmpeg now builds with `--disable-unstable`.

v6 also adds hardware acceleration to the bundled ffmpeg; the [CHANGELOG](../CHANGELOG.md) lists it.

### What to do

v6 only changes the server's ffmpeg, so test what your media server runs against it. Set `"log": "stdout"` and `"debug": true` in the server config to log each command's arguments before and after rewrites. ffmpeg's errors reach the client's stderr as usual, because the server forwards ffmpeg's output to the client.

### Checklist

1. Replace the server's `ffmpeg-over-ip-server`, `ffmpeg` and `ffprobe` with the v6 files, or with the v7 files if you are continuing to v7.
2. Remove `sonic` and `sonicls` from command lines and rewrites.
3. Run your usual transcodes with server debug logging on, and fix any command FFmpeg 8 rejects. For commands that come from your media server, a server `rewrites` rule can change the arguments.
4. Replace the client. A 5.x client works with a v6 server, so this can wait, but v7 needs both sides upgraded.

## Within v5

Of the 5.x releases, only 5.2.0 needs a config change: it changed how `rewrites` match. Every 5.x release uses wire protocol version 0x06, so the client and server can be upgraded separately.

### 5.2.0: rewrites match whole arguments

Up to 5.1.0, each `["find", "replace"]` pair replaced `find` wherever it appeared inside an argument, as a plain substring. From 5.2.0 on, `find` has to equal a whole argument. A `find` that contains spaces is split into words that must match consecutive arguments, and `replace` is split into separate arguments the same way.

A substring rule like `["nvenc", "qsv"]` no longer matches `h264_nvenc` or `hevc_nvenc`. Write one rule per full argument instead:

```jsonc
// Up to 5.1.0
"rewrites": [
  ["nvenc", "qsv"],
],

// 5.2.0 and later
"rewrites": [
  ["h264_nvenc", "h264_qsv"],
  ["hevc_nvenc", "hevc_qsv"],
],
```

Also check these cases:

- A rule whose `find` is already a whole argument, such as `["h264_nvenc", "h264_qsv"]`, still matches that argument, but it no longer changes longer arguments that contain it, such as a filter graph passed to `-vf` or `-filter_complex`. A rewrite can't change part of an argument.
- A `replace` that contains spaces now becomes several arguments. Up to 5.1.0 it was inserted as one argument with the spaces inside it.

Set `"debug": true` in the server config to log each command's arguments before and after rewrites. The client's `fallbackRewrites`, added in 5.2.0, follow the same rules. See [Rewrites](configuration.md#rewrites).

### Other 5.x changes

None of these need a config change. The last column says which side has to be upgraded to get the change.

| Release | Change | Side |
|---|---|---|
| 5.0.1 | ffmpeg's `pipe:` and `fd:` protocols work | server |
| 5.1.0 | One-line install scripts, and configuration through `FFMPEG_OVER_IP_CLIENT_*` and `FFMPEG_OVER_IP_SERVER_*` environment variables | both |
| 5.2.0 | Optional fallback to the client machine's own ffmpeg when the server is unreachable (`fallbackToLocal`) | client |
| 5.2.1 | Read-ahead, prefetch and a range cache for tunneled reads, on by default and tuned with `FFOIP_READAHEAD_BYTES` and `FFOIP_RANGE_CACHE_BYTES` (see [Performance Tuning](configuration.md#performance-tuning)) | both |

### Checklist

1. If the server config has `rewrites`, rewrite each rule in whole-argument form and check the debug log.
2. Replace the server's `ffmpeg-over-ip-server`, `ffmpeg` and `ffprobe`, and the client binary.

## v4 to v5

If you are on v4, follow this section, then the sections above it, starting with [Within v5](#within-v5).

### Architecture Change

v4 required a **shared filesystem** (Docker mount, NFS, or SMB) between client and server to transfer video data. The client and server coordinated commands over a lightweight protocol, but actual file I/O happened through the shared filesystem.

v5 **eliminates the shared filesystem entirely**. The server runs a patched ffmpeg that tunnels all file I/O (reads, writes, seeks, stat, etc.) back to the client over the TCP connection. Files are never stored on the server, and no shared storage is needed.

This means:
- No more NFS/SMB mount setup or maintenance
- No more path mapping between client and server filesystems
- Works across networks where shared storage isn't practical

### Breaking Config Changes

#### `ffmpegPath` removed

v4 required you to specify the path to ffmpeg on the server:

```jsonc
// v4 (remove this)
"ffmpegPath": "/usr/bin/ffmpeg"
```

v5 ships patched `ffmpeg` and `ffprobe` binaries in the release. The server finds them automatically in the same directory as its own binary. Place all three files together and remove `ffmpegPath` from your config.

#### Unix socket address format changed

v4 used bare paths for Unix sockets:

```jsonc
// v4
"address": "/tmp/ffmpeg-over-ip.sock"
```

v5 uses the `unix:` prefix (following the gRPC/Docker convention):

```jsonc
// v5
"address": "unix:/tmp/ffmpeg-over-ip.sock"
```

TCP addresses (`host:port`) are unchanged.

#### `--config` removed from client

v4 supported `--config` on both client and server. v5 only supports it on the server, since the client forwards all arguments to ffmpeg. Use environment variables or config file search paths instead:

```bash
FFMPEG_OVER_IP_CLIENT_CONFIG="/path/to/config.jsonc" ffmpeg-over-ip-client ...
```

Since 5.1.0 you can also set the address and secret in environment variables, with no config file:

```bash
FFMPEG_OVER_IP_CLIENT_ADDRESS=192.168.1.100:5050 \
FFMPEG_OVER_IP_CLIENT_AUTH_SECRET=your-secret \
ffmpeg-over-ip-client ...
```

See [Environment Variables](configuration.md#environment-variables) for the full list.

### New Features in v5

#### No shared filesystem required

The biggest change. See [How It Works](../README.md#how-it-works).

#### Pre-built ffmpeg binaries

Releases include patched `ffmpeg` and `ffprobe` with hardware acceleration support (NVENC, QSV, VAAPI, AMF, VideoToolbox, and more) built on [jellyfin-ffmpeg](https://github.com/jellyfin/jellyfin-ffmpeg). No need to install ffmpeg separately on the server.

#### Block comments

Config files now support `/* */` block comments in addition to `//` line comments.

### Checklist

1. Download the latest release (the server archive includes `ffmpeg-over-ip-server`, `ffmpeg`, and `ffprobe`)
2. Place all three server binaries in the same directory
3. Remove `ffmpegPath` from your server config
4. If using Unix sockets, add the `unix:` prefix to your address
5. Remove path rewrites from your server config (no longer needed without shared filesystems)
6. Remove any shared filesystem mounts that were only needed for ffmpeg-over-ip
7. If using `--config` on the client, switch to the `FFMPEG_OVER_IP_CLIENT_CONFIG` env var
