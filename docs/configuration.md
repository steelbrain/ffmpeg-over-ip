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
| `FFMPEG_OVER_IP_CLIENT_ADDRESS` | Yes | Server address (`host:port` or `unix:/path`) |
| `FFMPEG_OVER_IP_CLIENT_AUTH_SECRET` | Yes | HMAC auth secret (must match server) |
| `FFMPEG_OVER_IP_CLIENT_LOG` | No | Log destination: `stdout`, `stderr`, or file path |

### Server

| Variable | Required | Description |
|---|---|---|
| `FFMPEG_OVER_IP_SERVER_ADDRESS` | Yes | Listen address (`host:port` or `unix:/path`) |
| `FFMPEG_OVER_IP_SERVER_AUTH_SECRET` | Yes | HMAC auth secret (must match client) |
| `FFMPEG_OVER_IP_SERVER_LOG` | No | Log destination: `stdout`, `stderr`, or file path |
| `FFMPEG_OVER_IP_SERVER_DEBUG` | No | Log original/rewritten args (`true`, `1`, `yes`, `y`) |

Rewrites are not supported via environment variables — use a config file if you need them.

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

On Windows, `~` is your user directory (e.g., `C:\Users\<you>`). Paths 7 and 8 don't apply — use the binary directory, current directory, or an environment variable instead.

To see which paths are searched on your system, run:

```bash
ffmpeg-over-ip-server --debug-print-search-paths
ffmpeg-over-ip-client --debug-print-search-paths
```

## Server Config

```jsonc
{
  "address": "0.0.0.0:5050",
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
  "address": "192.168.1.100:5050",
  "authSecret": "your-secret-here",
  // Optional: see "Log" section below
  "log": "/tmp/ffmpeg-over-ip.log",
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

## Rewrites

Rewrites let the server substitute strings in ffmpeg arguments before running the command. This is useful when the client requests a codec the server doesn't have — for example, the client asks for `h264_nvenc` but the server has Intel QSV instead of NVIDIA.

```jsonc
{
  "rewrites": [
    ["h264_nvenc", "h264_qsv"],
    ["hevc_nvenc", "hevc_qsv"],
  ],
}
```

Each pair `["from", "to"]` does a plain string replacement across all ffmpeg arguments. In the example above, any argument containing `h264_nvenc` is rewritten to `h264_qsv`.

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
