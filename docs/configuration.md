# Configuration

Config files use [JSONC](https://code.visualstudio.com/docs/languages/json#_json-with-comments) format (JSON with `//` comments, `/* */` block comments, and trailing commas).

## Config Resolution Order

Configuration is resolved in this order (first match wins):

1. **Explicit path** — `--config <path>` (server only)
2. **Config file env var** — `FFMPEG_OVER_IP_SERVER_CONFIG` / `FFMPEG_OVER_IP_CLIENT_CONFIG` pointing to a file
3. **Individual env vars** — if both `_ADDRESS` and `_AUTH_SECRET` are set (see [Environment Variables](#environment-variables) below)
4. **File search** — standard paths (see below)

## Environment Variables

If both `ADDRESS` and `AUTH_SECRET` env vars are set (and no `_CONFIG` env var is set), configuration is read entirely from environment variables — no config file is needed.

### Client

| Variable | Required | Description |
|---|---|---|
| `FFMPEG_OVER_IP_CLIENT_ADDRESS` | Yes | Server address (`host:port` or `unix:/path`). Comma-separated for [multiple servers](#multiple-servers) |
| `FFMPEG_OVER_IP_CLIENT_AUTH_SECRET` | Yes | HMAC auth secret (must match server) |
| `FFMPEG_OVER_IP_CLIENT_DIAL_TIMEOUT` | No | Per-attempt connect timeout, e.g. `5s`, `1500ms`. `0` defers to the OS. Default `5s` |
| `FFMPEG_OVER_IP_CLIENT_LOG` | No | Log destination: `stdout`, `stderr`, or file path |
| `FFMPEG_OVER_IP_CLIENT_FALLBACK_TO_LOCAL` | No | Run local ffmpeg if the server is unreachable (`true`, `1`, `yes`, `y` — case-insensitive) |
| `FFMPEG_OVER_IP_CLIENT_DEBUG` | No | Log original/rewritten args when fallback runs (`true`, `1`, `yes`, `y` — case-insensitive) |

### Server

| Variable | Required | Description |
|---|---|---|
| `FFMPEG_OVER_IP_SERVER_ADDRESS` | Yes | Listen address (`host:port` or `unix:/path`) |
| `FFMPEG_OVER_IP_SERVER_AUTH_SECRET` | Yes | HMAC auth secret (must match client) |
| `FFMPEG_OVER_IP_SERVER_LOG` | No | Log destination: `stdout`, `stderr`, or file path |
| `FFMPEG_OVER_IP_SERVER_LOCAL_PREFIXES` | No | Paths this server reads from its own filesystem — see [Shared Storage](#shared-storage). `:`-separated (`;` on Windows) |
| `FFMPEG_OVER_IP_SERVER_DEBUG` | No | Log original/rewritten args (`true`, `1`, `yes`, `y` — case-insensitive) |

Rewrites (server `rewrites` and client `fallbackRewrites`) are not supported via environment variables — use a config file if you need them.

## Shared Storage

By default the server's ffmpeg tunnels every file operation back to the client,
so no shared storage is needed anywhere. When the server *does* have the media
mounted at the same path the client sees, `localPrefixes` lets it read those
files directly:

```jsonc
"localPrefixes": ["/media"]
```

```bash
FFMPEG_OVER_IP_SERVER_LOCAL_PREFIXES=/media
```

This removes one leg of I/O. Instead of `storage -> client -> server`, source
bytes travel `storage -> server`, and the client is no longer a relay for data
it never looks at.

The rule is one sentence: **read-only opens under a declared prefix are served
from the server's own filesystem; everything else tunnels to the client.**

- **Paths must be identical on both hosts.** Nothing translates them. If the
  client asks for `/media/movies/a.mkv`, the server opens exactly that path.
- **Matching is on whole path components.** `/media` covers `/media/movies` but
  never `/mediafoo`. Paths containing a `..` component are refused and tunnel.
- **Writes are never local**, even to a declared prefix. The client is the only
  host whose filesystem the calling media server can read back, so a
  server-side write would land where nothing can serve it. This is enforced,
  not just documented — so listing an output directory by mistake cannot
  corrupt a transcode, it just does nothing.
- **Reads outside the list still tunnel**, which is what keeps client-only
  paths (subtitle attachments, fonts) working with no extra configuration.
- **A declaration is not a hint.** A missing file under a declared prefix fails
  with `ENOENT` naming the file; it is never retried over the tunnel. Silently
  falling back would turn a broken mount into an unexplained slowdown instead
  of an error.
- The server checks each prefix is a directory at startup and exits if not.
  That catches a typo or a never-mounted share at deploy time. It proves
  nothing about steady state — a share that goes stale later is not detected,
  by design. Refusing to start is safe: the client's dial then fails, which is
  exactly the path that triggers its `fallbackToLocal`.

Mixed fleets need no client configuration. A client cannot tell a
shared-storage server from a tunneling one, so endpoints with and without
local storage can be listed together.

## Multiple Servers

The client's `address` accepts a comma-separated list. Every entry is dialed at
the same time, the first connection to come up wins, and the rest are cancelled
and closed:

```jsonc
"address": "192.168.1.100:5050, 192.168.1.101:5050"
```

```bash
FFMPEG_OVER_IP_CLIENT_ADDRESS=192.168.1.100:5050,192.168.1.101:5050
```

Notes:

- Only the connection is raced. The signed command is sent after a winner is
  picked, so a transcode never starts on more than one server.
- Order is not priority. The winner is whichever completes its TCP handshake
  first, which on a LAN mostly means "whichever is up" — this is failover
  across interchangeable transcode nodes, not load balancing across busy ones.
- `fallbackToLocal` triggers only once *every* address has failed.
- Failover happens at connect time only. A server that dies mid-transcode is
  still fatal, same as with a single address.
- `dialTimeout` bounds each attempt. It matters when an endpoint is blackholed
  rather than refusing — without it, one powered-off node delays the local
  fallback by the OS SYN retry schedule (~2 minutes).
- Mixed transports are fine: `unix:/tmp/f.sock, 192.168.1.100:5050`.

### Example (Docker / scripted deployment)

```bash
docker run \
  -e FFMPEG_OVER_IP_CLIENT_ADDRESS=192.168.1.100:5050 \
  -e FFMPEG_OVER_IP_CLIENT_AUTH_SECRET=my-secret \
  -v ./ffmpeg-over-ip-client:/usr/bin/ffmpeg \
  your-image
```

## Config File Search Paths

If no explicit path or env var config is used, the first file found wins:

1. Next to the binary: `<exe-dir>/ffmpeg-over-ip.{server,client}.jsonc`
2. Next to the binary (hidden): `<exe-dir>/.ffmpeg-over-ip.{server,client}.jsonc`
3. `./ffmpeg-over-ip.{server,client}.jsonc`
4. `./.ffmpeg-over-ip.{server,client}.jsonc`
5. `~/.ffmpeg-over-ip.{server,client}.jsonc`
6. `~/.config/ffmpeg-over-ip.{server,client}.jsonc`
7. `/etc/ffmpeg-over-ip.{server,client}.jsonc`
8. `/usr/local/etc/ffmpeg-over-ip.{server,client}.jsonc`

The server also accepts `--config <path>`.

On Windows, `~` is your user directory (e.g., `C:\Users\<you>`). The `/etc/...` and `/usr/local/etc/...` paths are still searched but won't exist on a stock install — use the binary directory, current directory, or an environment variable instead.

To see which paths are searched on your system, run:

```bash
ffmpeg-over-ip-server --debug-print-search-paths
ffmpeg-over-ip-client --debug-print-search-paths
```

## Server Config

```jsonc
{
  // Required: listen address (host:port or unix:/path/to/socket)
  "address": "0.0.0.0:5050",
  // Required: HMAC auth secret — must match client
  "authSecret": "your-secret-here",
  // Optional: see "Log" section below (default: no logging)
  "log": "stdout",
  // Optional: log original and rewritten args for each command (default: false)
  "debug": true,
  // Optional: see "Rewrites" section below
  "rewrites": [
    ["h264_nvenc", "h264_qsv"],
  ],
}
```

The server looks for `ffmpeg` and `ffprobe` in the same directory as its own binary. Ship all three together.

## Client Config

```jsonc
{
  // Required: server address (host:port or unix:/path/to/socket)
  "address": "192.168.1.100:5050",
  // Required: HMAC auth secret — must match server
  "authSecret": "your-secret-here",
  // Optional: see "Log" section below
  "log": "/tmp/ffmpeg-over-ip.log",
  // Optional: see "Fallback to local ffmpeg" section below (default: false)
  "fallbackToLocal": true,
  // Optional: rewrites applied only when running the local fallback
  "fallbackRewrites": [
    ["h264_nvenc", "h264_qsv"],
  ],
  // Optional: log original and rewritten args when fallback runs (default: false)
  "debug": true,
}
```

## ffprobe

The client detects ffprobe mode from its binary name. Create a symlink (or copy) whose name contains "ffprobe":

```bash
# Linux / macOS
ln -s ffmpeg-over-ip-client ffprobe

# Windows
mklink ffprobe.exe ffmpeg-over-ip-client.exe
```

Any name containing "ffprobe" works — `ffprobe`, `my-ffprobe`, `ffprobe-remote`, etc.

## Fallback to local ffmpeg

When `fallbackToLocal` is enabled, the client runs the host's own `ffmpeg` (or `ffprobe`) directly if it can't connect to the server. This keeps transcoding working when the GPU machine is offline — useful for media servers like Jellyfin where any transcoder is better than none.

```jsonc
{
  "fallbackToLocal": true,
  // Optional: rewrites applied to argv before exec'ing the local ffmpeg.
  // Same format and semantics as server "rewrites" — useful for swapping
  // codecs that the local hardware can't handle (e.g., NVENC -> QSV).
  "fallbackRewrites": [
    ["h264_nvenc", "h264_qsv"],
  ],
}
```

Behavior:

- Only triggered when the initial TCP connection to the server fails. Once a session is established, mid-stream errors are still fatal — we don't restart a partial transcode locally.
- `ffmpeg` vs `ffprobe` is selected by the client's argv[0] basename, exactly like the non-fallback path. A binary named `ffprobe` (or any name containing `ffprobe`) looks for `ffprobe` on PATH; everything else looks for `ffmpeg`.
- The local binary is located by searching `$PATH`. On Windows the bare name is tried first (so MSYS / Git-Bash shims named simply `ffmpeg` resolve), then each `%PATHEXT%` extension. The client always skips its own executable, so it's safe to install the client as `ffmpeg` on `$PATH` without recursing.
- `FFMPEG_OVER_IP_*` env vars are stripped from the local ffmpeg's environment so the auth secret doesn't leak into `/proc/<pid>/environ`.
- Empty and `.` entries in `$PATH` are skipped (cwd-on-PATH is a known foot-gun).
- If no local binary is found, the client exits with code 1 and the diagnostic is written to the configured `log` sink. Logging is disabled by default — enable `"log": "stderr"` (or a file path) if you want to see fallback events.

Security notes:

- Enabling `fallbackToLocal` means anyone who can write a file named `ffmpeg` to a directory earlier in `$PATH` than the real binary can hijack transcodes. Only enable on hosts where you trust `$PATH`.
- On Windows, `%PATHEXT%` is searched in its declared order (default `.COM;.EXE;...`), so a malicious `ffmpeg.com` would be picked up before `ffmpeg.exe`. Same caveat: trust your `$PATH`.
- A non-root user with write access to any directory in root's `$PATH` can hijack transcodes that root invokes. Audit `$PATH` directory permissions when running the client as root.
- All fallback diagnostics go to the configured `log` sink, not to ffmpeg's stdout/stderr (the client is an invisible proxy). To debug fallback in production where stderr is consumed by Jellyfin or another media server, set `log` to a file path.

## Rewrites

Rewrites let the server substitute argv elements in ffmpeg arguments before running the command. This is useful when the client requests a codec the server doesn't have — for example, the client asks for `h264_nvenc` but the server has Intel QSV instead of NVIDIA.

```jsonc
{
  "rewrites": [
    ["h264_nvenc", "h264_qsv"],
    ["hevc_nvenc", "hevc_qsv"],
  ],
}
```

Each pair `["find", "replace"]` matches whole argv elements — not substrings within them. The example above rewrites any argv element exactly equal to `h264_nvenc` to `h264_qsv`. An element like `h264_nvenc_extra` would not match.

`find` and `replace` are each split on whitespace, so a single rewrite can match a consecutive run of argv elements and substitute a run of different length:

```jsonc
{
  "rewrites": [
    // Swap a two-element run (-hwaccel qsv) for a different two-element run.
    ["-hwaccel qsv", "-hwaccel cuda"],

    // Expand a two-element run into a four-element run.
    ["-hwaccel qsv", "-hwaccel cuda -hwaccel_output_format cuda"],

    // Remove a one-element argv entry entirely (empty replacement).
    ["-nostdin", ""],

    // Remove a two-element run.
    ["-preset veryfast", ""],
  ],
}
```

Rewrites are applied in declared order, so a later rewrite can match tokens produced by an earlier one. The same rewrite is applied to every matching run in argv, not just the first.

Enable `"debug": true` to log original and rewritten arguments for each command.

## Log

The `log` field controls where log output goes. Supported values:

| Value | Behavior |
|---|---|
| `"stdout"` | Log to standard output |
| `"stderr"` | Log to standard error |
| `false` or omitted | Disable logging (default) |
| `"/path/to/file.log"` | Log to a file (created if missing, appended if exists) |

Note: `false` must be the JSON boolean (no quotes). The string `"false"` would be treated as a file path.

File paths support `$TMPDIR`, `$HOME`, `$USER`, and `$PWD` interpolation (both `$VAR` and `${VAR}` syntax):

```jsonc
// expands to e.g. /tmp/ffmpeg-over-ip.log
"log": "$TMPDIR/ffmpeg-over-ip.log"

// use braces to disambiguate from e.g. $HOMEDIR
"log": "${HOME}/logs/ffmpeg-over-ip.log"
```

If the parent directory doesn't exist or the file can't be opened, a warning is printed to stderr and logging falls back to stderr.

## Address

The `address` field supports TCP and Unix domain sockets:

| Format | Example | Description |
|---|---|---|
| `host:port` | `"0.0.0.0:5050"` | TCP (default) |
| `hostname:port` | `"server.local:5050"` | TCP with hostname |
| `unix:/path` | `"unix:/tmp/ffmpeg.sock"` | Unix domain socket |

Unix domain sockets work on Linux, macOS, and Windows 10+. The server automatically cleans up the socket file on shutdown.

## Performance Tuning

The patched `ffmpeg` on the server tunnels its file reads back to the client. To cut round trips, the fio layer reads ahead, prefetches the next block while the current one is consumed, and caches recently-read ranges. Two optional environment variables tune this. They are read by `ffmpeg` at startup, so set them in the **server's** environment — the server passes its environment through to the `ffmpeg` it launches. The defaults suit most workloads; you rarely need to change them.

| Variable | Default | Description |
|---|---|---|
| `FFOIP_READAHEAD_BYTES` | `2097152` (2 MiB) | Maximum read-ahead window per open file. The window starts small and grows toward this value on sustained sequential reads. Values above 16 MiB are clamped. Set to `0` to disable read-ahead and prefetch entirely (one network read per ffmpeg read). |
| `FFOIP_RANGE_CACHE_BYTES` | `268435456` (256 MiB) | Maximum bytes of previously-read ranges cached per open file, so backward seeks and re-reads are served without a round trip. Only applies to read-only files of 256 MiB or smaller. Set to `0` to disable the range cache. |

Notes:

- Both values are byte counts. An unparseable value is ignored with a warning and the default is used.
- When `FFOIP_READAHEAD_BYTES` is left unset, a smaller automatic cap (1.25 MiB) is applied to files under 1 GiB; setting the variable explicitly overrides that heuristic for all files.
- These knobs trade a little server memory for fewer round trips. Larger values help high-latency links and sequential reads; they don't help seek-heavy access, where read-ahead resets on every seek.

```bash
# Set in the server's environment (shell, systemd unit, etc.)
FFOIP_READAHEAD_BYTES=4194304 FFOIP_RANGE_CACHE_BYTES=536870912 \
  ffmpeg-over-ip-server --config /etc/ffmpeg-over-ip.server.jsonc

# Docker: pass through with -e
docker run \
  -e FFOIP_READAHEAD_BYTES=4194304 \
  -e FFMPEG_OVER_IP_SERVER_ADDRESS=0.0.0.0:5050 \
  -e FFMPEG_OVER_IP_SERVER_AUTH_SECRET=my-secret \
  your-server-image
```
