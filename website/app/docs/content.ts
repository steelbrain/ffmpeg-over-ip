export interface DocPage {
  slug: string
  title: string
  description: string
  content: string
}

export const docs: DocPage[] = [
  {
    slug: "quick-start",
    title: "Quick Start",
    description:
      "Get ffmpeg-over-ip running in minutes — install the server on your GPU machine, configure the client on your media server, and verify the connection.",
    content: `
Download the latest release for your platform from the [releases page](https://github.com/steelbrain/ffmpeg-over-ip/releases/latest).

## Server Setup (GPU machine)

1. Extract the release. Keep \`ffmpeg-over-ip-server\`, \`ffmpeg\`, and \`ffprobe\` in the same directory — the server finds them automatically.

2. Create a config file \`ffmpeg-over-ip.server.jsonc\` in the same directory:

\`\`\`jsonc
{
  "address": "0.0.0.0:5050",
  "authSecret": "pick-a-strong-secret"
}
\`\`\`

3. Make sure port 5050 is open (firewall, cloud security group, etc.), then start the server:

\`\`\`bash
./ffmpeg-over-ip-server
\`\`\`

## Client Setup (media server)

1. Extract \`ffmpeg-over-ip-client\` somewhere convenient.

2. Create a config file \`ffmpeg-over-ip.client.jsonc\` next to the binary or in your home directory:

\`\`\`jsonc
{
  "address": "192.168.1.100:5050",  // your server's IP
  "authSecret": "pick-a-strong-secret"
}
\`\`\`

3. Set up ffprobe so your media server can probe files too — symlink or copy the client binary:

\`\`\`bash
ln -s ffmpeg-over-ip-client ffprobe
# or: cp ffmpeg-over-ip-client ffprobe
\`\`\`

4. Point your media server at both binaries. For Jellyfin, set the FFmpeg path in Dashboard > Playback to \`ffmpeg-over-ip-client\`.

## Windows

On Windows, use the \`.exe\` binaries from the release. Config files work the same way — place \`ffmpeg-over-ip.client.jsonc\` next to the binary or in your user directory (\`C:\\Users\\<you>\\\`). For ffprobe, copy and rename the client binary:

\`\`\`cmd
copy ffmpeg-over-ip-client.exe ffprobe.exe
\`\`\`

## Verify

Test the connection by running a command through the client:

\`\`\`bash
./ffmpeg-over-ip-client -version
\`\`\`

You should see the server's ffmpeg version and build info. If you get a connection error, double-check the address, port, and auth secret.

## Alternative: Environment Variables

Instead of config files, you can pass configuration via environment variables. This is useful for Docker and scripted deployments:

\`\`\`bash
export FFMPEG_OVER_IP_SERVER_ADDRESS=0.0.0.0:5050
export FFMPEG_OVER_IP_SERVER_AUTH_SECRET=pick-a-strong-secret
./ffmpeg-over-ip-server
\`\`\`

See [Configuration](/docs/configuration#environment-variables) for all supported variables.
`,
  },
  {
    slug: "configuration",
    title: "Configuration",
    description:
      "Reference for ffmpeg-over-ip config — addresses, auth, environment variables, search paths, codec rewrites, local fallback, logging, and Unix sockets.",
    content: `
Config files use [JSONC](https://code.visualstudio.com/docs/languages/json#_json-with-comments) format (JSON with \`//\` comments, \`/* */\` block comments, and trailing commas).

## Config Resolution Order

Configuration is resolved in this order (first match wins):

1. **Explicit path** — \`--config <path>\` (server only)
2. **Config file env var** — \`FFMPEG_OVER_IP_SERVER_CONFIG\` / \`FFMPEG_OVER_IP_CLIENT_CONFIG\` pointing to a file
3. **Individual env vars** — if both \`_ADDRESS\` and \`_AUTH_SECRET\` are set (see [Environment Variables](#environment-variables) below)
4. **File search** — standard paths (see below)

## Environment Variables

If both \`ADDRESS\` and \`AUTH_SECRET\` env vars are set (and no \`_CONFIG\` env var is set), configuration is read entirely from environment variables — no config file is needed.

### Client

| Variable | Required | Description |
|---|---|---|
| \`FFMPEG_OVER_IP_CLIENT_ADDRESS\` | Yes | Server address (\`host:port\` or \`unix:/path\`) |
| \`FFMPEG_OVER_IP_CLIENT_AUTH_SECRET\` | Yes | HMAC auth secret (must match server) |
| \`FFMPEG_OVER_IP_CLIENT_LOG\` | No | Log destination: \`stdout\`, \`stderr\`, or file path |
| \`FFMPEG_OVER_IP_CLIENT_FALLBACK_TO_LOCAL\` | No | Run local ffmpeg if the server is unreachable (\`true\`, \`1\`, \`yes\`, \`y\` — case-insensitive) |
| \`FFMPEG_OVER_IP_CLIENT_DEBUG\` | No | Log original/rewritten args when fallback runs (\`true\`, \`1\`, \`yes\`, \`y\` — case-insensitive) |

### Server

| Variable | Required | Description |
|---|---|---|
| \`FFMPEG_OVER_IP_SERVER_ADDRESS\` | Yes | Listen address (\`host:port\` or \`unix:/path\`) |
| \`FFMPEG_OVER_IP_SERVER_AUTH_SECRET\` | Yes | HMAC auth secret (must match client) |
| \`FFMPEG_OVER_IP_SERVER_LOG\` | No | Log destination: \`stdout\`, \`stderr\`, or file path |
| \`FFMPEG_OVER_IP_SERVER_DEBUG\` | No | Log original/rewritten args (\`true\`, \`1\`, \`yes\`, \`y\` — case-insensitive) |

Rewrites (server \`rewrites\` and client \`fallbackRewrites\`) are not supported via environment variables — use a config file if you need them.

### Example (Docker / scripted deployment)

\`\`\`bash
docker run \\
  -e FFMPEG_OVER_IP_CLIENT_ADDRESS=192.168.1.100:5050 \\
  -e FFMPEG_OVER_IP_CLIENT_AUTH_SECRET=my-secret \\
  -v ./ffmpeg-over-ip-client:/usr/bin/ffmpeg \\
  your-image
\`\`\`

## Config File Search Paths

If no explicit path or env var config is used, the first file found wins:

1. Next to the binary: \`<exe-dir>/ffmpeg-over-ip.{server,client}.jsonc\`
2. Next to the binary (hidden): \`<exe-dir>/.ffmpeg-over-ip.{server,client}.jsonc\`
3. \`./ffmpeg-over-ip.{server,client}.jsonc\`
4. \`./.ffmpeg-over-ip.{server,client}.jsonc\`
5. \`~/.ffmpeg-over-ip.{server,client}.jsonc\`
6. \`~/.config/ffmpeg-over-ip.{server,client}.jsonc\`
7. \`/etc/ffmpeg-over-ip.{server,client}.jsonc\`
8. \`/usr/local/etc/ffmpeg-over-ip.{server,client}.jsonc\`

The server also accepts \`--config <path>\`.

On Windows, \`~\` is your user directory (e.g., \`C:\\Users\\<you>\`). The \`/etc/...\` and \`/usr/local/etc/...\` paths are still searched but won't exist on a stock install — use the binary directory, current directory, or an environment variable instead.

To see which paths are searched on your system, run:

\`\`\`bash
ffmpeg-over-ip-server --debug-print-search-paths
ffmpeg-over-ip-client --debug-print-search-paths
\`\`\`

## Server Config

\`\`\`jsonc
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
\`\`\`

The server looks for \`ffmpeg\` and \`ffprobe\` in the same directory as its own binary. Ship all three together.

## Client Config

\`\`\`jsonc
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
\`\`\`

## ffprobe

The client detects ffprobe mode from its binary name. Create a symlink (or copy) whose name contains "ffprobe":

\`\`\`bash
# Linux / macOS
ln -s ffmpeg-over-ip-client ffprobe

# Windows
mklink ffprobe.exe ffmpeg-over-ip-client.exe
\`\`\`

Any name containing "ffprobe" works — \`ffprobe\`, \`my-ffprobe\`, \`ffprobe-remote\`, etc.

## Fallback to local ffmpeg

When \`fallbackToLocal\` is enabled, the client runs the host's own \`ffmpeg\` (or \`ffprobe\`) directly if it can't connect to the server. This keeps transcoding working when the GPU machine is offline — useful for media servers like Jellyfin where any transcoder is better than none.

\`\`\`jsonc
{
  "fallbackToLocal": true,
  // Optional: rewrites applied to argv before exec'ing the local ffmpeg.
  // Same format and semantics as server "rewrites" — useful for swapping
  // codecs that the local hardware can't handle (e.g., NVENC -> QSV).
  "fallbackRewrites": [
    ["h264_nvenc", "h264_qsv"],
  ],
}
\`\`\`

Behavior:

- Only triggered when the initial TCP connection to the server fails. Once a session is established, mid-stream errors are still fatal — we don't restart a partial transcode locally.
- \`ffmpeg\` vs \`ffprobe\` is selected by the client's argv[0] basename, exactly like the non-fallback path. A binary named \`ffprobe\` (or any name containing \`ffprobe\`) looks for \`ffprobe\` on PATH; everything else looks for \`ffmpeg\`.
- The local binary is located by searching \`$PATH\`. On Windows the bare name is tried first (so MSYS / Git-Bash shims named simply \`ffmpeg\` resolve), then each \`%PATHEXT%\` extension. The client always skips its own executable, so it's safe to install the client as \`ffmpeg\` on \`$PATH\` without recursing.
- \`FFMPEG_OVER_IP_*\` env vars are stripped from the local ffmpeg's environment so the auth secret doesn't leak into \`/proc/<pid>/environ\`.
- Empty and \`.\` entries in \`$PATH\` are skipped (cwd-on-PATH is a known foot-gun).
- If no local binary is found, the client exits with code 1 and the diagnostic is written to the configured \`log\` sink. Logging is disabled by default — enable \`"log": "stderr"\` (or a file path) if you want to see fallback events.

Security notes:

- Enabling \`fallbackToLocal\` means anyone who can write a file named \`ffmpeg\` to a directory earlier in \`$PATH\` than the real binary can hijack transcodes. Only enable on hosts where you trust \`$PATH\`.
- On Windows, \`%PATHEXT%\` is searched in its declared order (default \`.COM;.EXE;...\`), so a malicious \`ffmpeg.com\` would be picked up before \`ffmpeg.exe\`. Same caveat: trust your \`$PATH\`.
- A non-root user with write access to any directory in root's \`$PATH\` can hijack transcodes that root invokes. Audit \`$PATH\` directory permissions when running the client as root.
- All fallback diagnostics go to the configured \`log\` sink, not to ffmpeg's stdout/stderr (the client is an invisible proxy). To debug fallback in production where stderr is consumed by Jellyfin or another media server, set \`log\` to a file path.

## Rewrites

Rewrites let the server substitute argv elements in ffmpeg arguments before running the command. This is useful when the client requests a codec the server doesn't have — for example, the client asks for \`h264_nvenc\` but the server has Intel QSV instead of NVIDIA.

\`\`\`jsonc
{
  "rewrites": [
    ["h264_nvenc", "h264_qsv"],
    ["hevc_nvenc", "hevc_qsv"],
  ],
}
\`\`\`

Each pair \`["find", "replace"]\` matches whole argv elements — not substrings within them. The example above rewrites any argv element exactly equal to \`h264_nvenc\` to \`h264_qsv\`. An element like \`h264_nvenc_extra\` would not match.

\`find\` and \`replace\` are each split on whitespace, so a single rewrite can match a consecutive run of argv elements and substitute a run of different length:

\`\`\`jsonc
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
\`\`\`

Rewrites are applied in declared order, so a later rewrite can match tokens produced by an earlier one. The same rewrite is applied to every matching run in argv, not just the first.

Enable \`"debug": true\` on the server to log original and rewritten arguments for each command.

## Log

The \`log\` field controls where log output goes. Supported values:

| Value | Behavior |
|---|---|
| \`"stdout"\` | Log to standard output |
| \`"stderr"\` | Log to standard error |
| \`false\` or omitted | Disable logging (default) |
| \`"/path/to/file.log"\` | Log to a file (created if missing, appended if exists) |

Note: \`false\` must be the JSON boolean (no quotes). The string \`"false"\` would be treated as a file path.

File paths support \`$TMPDIR\`, \`$HOME\`, \`$USER\`, and \`$PWD\` interpolation (both \`$VAR\` and \`\${VAR}\` syntax):

\`\`\`jsonc
// expands to e.g. /tmp/ffmpeg-over-ip.log
"log": "$TMPDIR/ffmpeg-over-ip.log"

// use braces to disambiguate from e.g. $HOMEDIR
"log": "\${HOME}/logs/ffmpeg-over-ip.log"
\`\`\`

If the parent directory doesn't exist or the file can't be opened, a warning is printed to stderr and logging falls back to stderr.

## Address

The \`address\` field supports TCP and Unix domain sockets:

| Format | Example | Description |
|---|---|---|
| \`host:port\` | \`"0.0.0.0:5050"\` | TCP (default) |
| \`hostname:port\` | \`"server.local:5050"\` | TCP with hostname |
| \`unix:/path\` | \`"unix:/tmp/ffmpeg.sock"\` | Unix domain socket |

Unix domain sockets work on Linux, macOS, and Windows 10+. The server automatically cleans up the socket file on shutdown.

## Performance Tuning

The patched \`ffmpeg\` on the server tunnels its file reads back to the client. To cut round trips, the fio layer reads ahead, prefetches the next block while the current one is consumed, and caches recently-read ranges. Two optional environment variables tune this. They are read by \`ffmpeg\` at startup, so set them in the **server's** environment — the server passes its environment through to the \`ffmpeg\` it launches. The defaults suit most workloads; you rarely need to change them.

| Variable | Default | Description |
|---|---|---|
| \`FFOIP_READAHEAD_BYTES\` | \`2097152\` (2 MiB) | Maximum read-ahead window per open file. The window starts small and grows toward this value on sustained sequential reads. Values above 16 MiB are clamped. Set to \`0\` to disable read-ahead and prefetch entirely (one network read per ffmpeg read). |
| \`FFOIP_RANGE_CACHE_BYTES\` | \`268435456\` (256 MiB) | Maximum bytes of previously-read ranges cached, across all files the \`ffmpeg\` process has open, so backward seeks and re-reads are served without a round trip. Only applies to read-only files of 256 MiB or smaller. Set to \`0\` to disable the range cache. |

Notes:

- Both values are byte counts. An unparseable value is ignored with a warning and the default is used.
- When \`FFOIP_READAHEAD_BYTES\` is left unset, a smaller automatic cap (1.25 MiB) is applied to files under 1 GiB; setting the variable explicitly overrides that heuristic for all files.
- These knobs trade a little server memory for fewer round trips. Larger values help high-latency links and sequential reads; they don't help seek-heavy access, where read-ahead resets on every seek.

\`\`\`bash
# Set in the server's environment (shell, systemd unit, etc.)
FFOIP_READAHEAD_BYTES=4194304 FFOIP_RANGE_CACHE_BYTES=536870912 \\
  ffmpeg-over-ip-server --config /etc/ffmpeg-over-ip.server.jsonc

# Docker: pass through with -e
docker run \\
  -e FFOIP_READAHEAD_BYTES=4194304 \\
  -e FFMPEG_OVER_IP_SERVER_ADDRESS=0.0.0.0:5050 \\
  -e FFMPEG_OVER_IP_SERVER_AUTH_SECRET=my-secret \\
  your-server-image
\`\`\`
`,
  },
  {
    slug: "docker",
    title: "Docker Integration",
    description:
      "Run ffmpeg-over-ip in Docker — mount the client as /usr/bin/ffmpeg, configure via environment variables, and use Unix sockets for same-machine setups.",
    content: `
You can use ffmpeg-over-ip in Docker environments by mounting the binary and passing configuration via environment variables. This allows containers to use ffmpeg remotely without needing GPU passthrough or other special setup.

## Client in Docker

Mount the client binary as \`/usr/bin/ffmpeg\` and pass config via environment variables:

\`\`\`bash
docker run \\
  -e FFMPEG_OVER_IP_CLIENT_ADDRESS=192.168.1.100:5050 \\
  -e FFMPEG_OVER_IP_CLIENT_AUTH_SECRET=your-secret \\
  -v ./ffmpeg-over-ip-client:/usr/bin/ffmpeg \\
  your-image
\`\`\`

For ffprobe support, add a second mount:

\`\`\`bash
docker run \\
  -e FFMPEG_OVER_IP_CLIENT_ADDRESS=192.168.1.100:5050 \\
  -e FFMPEG_OVER_IP_CLIENT_AUTH_SECRET=your-secret \\
  -v ./ffmpeg-over-ip-client:/usr/bin/ffmpeg \\
  -v ./ffmpeg-over-ip-client:/usr/bin/ffprobe \\
  your-image
\`\`\`

You can also use a config file volume instead of env vars — see [Configuration](/docs/configuration) for details.

## Server in Docker

\`\`\`bash
docker run \\
  -e FFMPEG_OVER_IP_SERVER_ADDRESS=0.0.0.0:5050 \\
  -e FFMPEG_OVER_IP_SERVER_AUTH_SECRET=your-secret \\
  -v ./ffmpeg-over-ip-server:/usr/bin/ffmpeg-over-ip-server \\
  -v ./ffmpeg:/usr/bin/ffmpeg \\
  -v ./ffprobe:/usr/bin/ffprobe \\
  --runtime=nvidia \\
  your-image
\`\`\`

All three binaries (\`ffmpeg-over-ip-server\`, \`ffmpeg\`, \`ffprobe\`) must be in the same directory inside the container.

## Unix Sockets (same-machine setups)

If the server and client run on the same machine (e.g., server on the host, client in a container), use a Unix socket instead of TCP to avoid network overhead and \`host.docker.internal\` configuration:

Start the server with a socket address (config file or env var):

\`\`\`bash
export FFMPEG_OVER_IP_SERVER_ADDRESS=unix:/tmp/ffmpeg-over-ip.sock
export FFMPEG_OVER_IP_SERVER_AUTH_SECRET=your-secret
./ffmpeg-over-ip-server
\`\`\`

Then mount the socket into the container:

\`\`\`bash
docker run \\
  -e FFMPEG_OVER_IP_CLIENT_ADDRESS=unix:/tmp/ffmpeg-over-ip.sock \\
  -e FFMPEG_OVER_IP_CLIENT_AUTH_SECRET=your-secret \\
  -v /tmp/ffmpeg-over-ip.sock:/tmp/ffmpeg-over-ip.sock \\
  -v ./ffmpeg-over-ip-client:/usr/bin/ffmpeg \\
  your-image
\`\`\`

## Debugging

Enable debug logging on the server via env vars:

\`\`\`bash
docker run \\
  -e FFMPEG_OVER_IP_SERVER_ADDRESS=0.0.0.0:5050 \\
  -e FFMPEG_OVER_IP_SERVER_AUTH_SECRET=your-secret \\
  -e FFMPEG_OVER_IP_SERVER_LOG=stdout \\
  -e FFMPEG_OVER_IP_SERVER_DEBUG=true \\
  -v ./ffmpeg-over-ip-server:/usr/bin/ffmpeg-over-ip-server \\
  -v ./ffmpeg:/usr/bin/ffmpeg \\
  -v ./ffprobe:/usr/bin/ffprobe \\
  --runtime=nvidia \\
  your-image
\`\`\`

For the client, log to a file you can tail from inside the container:

\`\`\`bash
docker run \\
  -e FFMPEG_OVER_IP_CLIENT_ADDRESS=192.168.1.100:5050 \\
  -e FFMPEG_OVER_IP_CLIENT_AUTH_SECRET=your-secret \\
  -e FFMPEG_OVER_IP_CLIENT_LOG=/tmp/ffmpeg-over-ip.client.log \\
  -v ./ffmpeg-over-ip-client:/usr/bin/ffmpeg \\
  your-image
\`\`\`

Then tail it:

\`\`\`bash
docker exec -it <container> tail -f /tmp/ffmpeg-over-ip.client.log
\`\`\`

Note: rewrites require a config file — they can't be set via env vars. If you need rewrites alongside env var config, use \`FFMPEG_OVER_IP_SERVER_CONFIG\` to point to a config file instead.
`,
  },
  {
    slug: "troubleshooting",
    title: "Troubleshooting",
    description:
      "Fixes for common ffmpeg-over-ip errors, including connection, authentication and version mismatch failures, and the ffmpeg options that open files on the server.",
    content: `
**Connection refused** — Check that the server is running, the address and port match your config, and the port is reachable (firewall, cloud security group, Docker network). Also verify the server is listening on the right host — \`127.0.0.1:5050\` only accepts local connections. Use \`0.0.0.0:5050\` to listen on all interfaces.

> **Tip:** To test whether the port is reachable, run \`telnet <server-ip> <port>\` from the client machine. If telnet can't connect, a firewall or security group is blocking the port — fix that before troubleshooting ffmpeg-over-ip itself.

**Authentication failed** — The \`authSecret\` must match exactly between client and server configs. The server rejects a client with a different secret: the client prints \`server error: authentication failed\`, and the server logs \`auth failed from <address>\`. If the client instead prints \`the server accepted the command but did not prove it knows the auth secret\`, something other than your ffmpeg-over-ip server answered at the configured address.

**Protocol version mismatch** — The server refuses a client from a different ffmpeg-over-ip release with \`protocol version mismatch: client sent 0x.., server expects 0x..\`. A client talking to a server from a release before 0x07 gets \`server error: expected command message (0x01), got 0x07\`. Upgrade the client and server together so both run the same release; see [Upgrading](/docs/upgrading).

**Server busy** — \`server error: server busy: too much data from connections that haven't authenticated yet\` means connections that hadn't finished the handshake were holding as much unverified data as the server buffers: 64 MiB in all, or 16 MiB from your client's IP address (its /64 for IPv6). The server logs \`handshake with <address> failed: ... too much data buffered for unauthenticated connections\`. Each such connection is dropped after at most 10 seconds, so retry; if it keeps happening, something on the network is flooding the server's port.

**Codec not found / encoder not available** — The server's ffmpeg may not support the requested codec. Use \`rewrites\` in the server config to map unsupported codecs to available ones (e.g., \`["h264_nvenc", "h264_qsv"]\`). See [Rewrites](/docs/configuration#rewrites).

**ffprobe not working** — The client detects ffprobe mode from its binary name. The binary or symlink must contain "ffprobe" in the name. See [ffprobe](/docs/configuration#ffprobe).

**Server can't find ffmpeg** — \`ffmpeg\` and \`ffprobe\` must be in the same directory as \`ffmpeg-over-ip-server\`.

**macOS Gatekeeper error** — Downloaded binaries may be quarantined by macOS. You may see \`"ffmpeg-over-ip" Not Opened — Apple could not verify "ffmpeg-over-ip" is free of malware that may harm your Mac or compromise your privacy\`, or \`Killed: 9\` when running from the terminal. Remove the quarantine attribute:

\`\`\`bash
# Server package
xattr -dr com.apple.quarantine ffmpeg ffprobe ffmpeg-over-ip-server

# Client package
xattr -dr com.apple.quarantine ffmpeg-over-ip-client
\`\`\`

**Need more detail?** — Enable logging on both sides. Set \`"log": "stdout"\` and \`"debug": true\` on the server to see the commands being executed. Set \`"log": "/tmp/ffmpeg-over-ip.log"\` on the client to capture client-side activity. If you use \`fallbackToLocal\`, also set \`"debug": true\` on the client to log the original and rewritten arguments when the local fallback runs.

## Files opened on the server

ffmpeg-over-ip tunnels ffmpeg's file access to the client, including inputs and outputs, HLS/DASH segments and playlists, concat lists, subtitle files and \`fontsdir\` fonts, and two-pass logs. A few ffmpeg features open files themselves, outside that layer, so they read or write the server's disk instead:

- the two-pass stats files of libx264, libx265 and libvvenc (their \`stats\` option, which \`-passlogfile\` sets for them)
- filter option files: \`stats_file\` of \`psnr\`, \`ssim\`, \`xpsnr\`, \`vmafmotion\` and \`loudnorm\`; \`filename\` of \`sendcmd\`/\`asendcmd\` and \`deshake\`; \`psfile\` and \`plot\` of \`curves\`; \`psfile\` of \`selectivecolor\`; \`file\` of \`lut3d\` and \`lut1d\`; \`fontfile\` and \`textfile\` of \`drawtext\`; \`textfile\` of \`flite\` and \`qrencode\`; \`lut\` and \`custom_shader_path\` of \`libplacebo\`; \`model\` of \`arnndn\`; \`dumpfile\` of \`firequalizer\`; \`hint\` of \`fieldhint\`; \`weights\` of \`nnedi\`; \`labels\` of \`dnn_detect\` and \`dnn_classify\`; \`debug_kdtree\` of \`paletteuse\`
- \`-vstats_file\`, \`-fpre\`, \`-report\` and \`FFREPORT\`, the \`bluray:\` protocol, image2's \`-pattern_type glob\`, and the DVD subtitle decoder's \`ifo_palette\`
- the HDS and Smooth Streaming muxers create their output directories and delete old fragments on the server (the fragments and manifests themselves go through the tunnel)
- system fonts libass finds through fontconfig

Use paths that exist on the server for these, or keep them off the command line.

`,
  },
  {
    slug: "upgrading",
    title: "Upgrading",
    description:
      "How to upgrade ffmpeg-over-ip with the install scripts or by hand, and what changed for users in each release from v4 to v7.",
    content: `
This guide covers what changed for users in each release since v4, newest first. To skip releases, read every section between your version and the newest. You don't need to install the releases in between: install the newest one and follow the checklist in each of those sections.

| You run | Read |
|---|---|
| 6.0.0 | [v6 to v7](#v6-to-v7) |
| 5.2.0 or 5.2.1 | [v5 to v6](#v5-to-v6), then [v6 to v7](#v6-to-v7) |
| 5.0.x or 5.1.0 | [Within v5](#within-v5), [v5 to v6](#v5-to-v6), then [v6 to v7](#v6-to-v7) |
| v4 | [v4 to v5](#v4-to-v5), then every section above it |

The [CHANGELOG](https://github.com/steelbrain/ffmpeg-over-ip/blob/main/CHANGELOG.md) lists every change in each release.

## Installing a new release

Upgrading replaces binaries. Config files and environment variables keep working: no release since 5.0.0 has removed or renamed a setting, though 5.2.0 changed how \`rewrites\` match (see [Within v5](#within-v5)).

Upgrade the server while it has no transcodes running. Stop it before you replace its files and start it again afterwards; until it restarts, it keeps running the old version.

### With the install scripts

The install scripts always install the latest release, into the current directory. If the binary is already there, they skip the download unless \`FOIP_FORCE\` is set. Run them in the directory of the existing install.

Server, on Linux or macOS:

\`\`\`sh
curl -fsSL https://ffmpeg-over-ip.com/install-server.sh | FOIP_FORCE=1 sh
\`\`\`

Client, on Linux or macOS:

\`\`\`sh
curl -fsSL https://ffmpeg-over-ip.com/install-client.sh | FOIP_FORCE=1 sh
\`\`\`

Server, on Windows (PowerShell):

\`\`\`powershell
$env:FOIP_FORCE = '1'
irm https://ffmpeg-over-ip.com/install-server.ps1 | iex
\`\`\`

Client, on Windows (PowerShell):

\`\`\`powershell
$env:FOIP_FORCE = '1'
irm https://ffmpeg-over-ip.com/install-client.ps1 | iex
\`\`\`

With \`FOIP_FORCE\` set, the script overwrites the files in the release archive and leaves everything else in the directory alone:

| Archive | Files |
|---|---|
| Server | \`ffmpeg-over-ip-server\`, \`ffmpeg\`, \`ffprobe\`, \`LICENSE\`, \`LICENSE.ffmpeg\` |
| Client | \`ffmpeg-over-ip-client\`, \`LICENSE\`, \`LICENSE.ffmpeg\` |

On Windows the binaries end in \`.exe\`. After extracting, the script handles the config file and \`ffprobe\` like this:

- An existing \`ffmpeg-over-ip.server.jsonc\` or \`ffmpeg-over-ip.client.jsonc\` in the directory is kept. If there is none, because your config is elsewhere or in environment variables, the script asks for an address and secret and writes one. Upgrade by hand if you don't want that file.
- An existing \`ffprobe\` is kept. On Linux and macOS the client script created it as a symlink to \`ffmpeg-over-ip-client\`, so it runs the new client. On Windows it created \`ffprobe.exe\` as a copy of the client, and that copy stays at the old version: delete \`ffprobe.exe\` before you run the script, and the script makes a new copy.

### By hand

Download the archives for your platform from the [releases page](https://github.com/steelbrain/ffmpeg-over-ip/releases): \`<platform>-ffmpeg-over-ip-server.zip\` for the server and \`<platform>-ffmpeg-over-ip-client.zip\` for the client, where \`<platform>\` is \`linux-amd64\`, \`linux-arm64\`, \`macos-arm64\`, \`macos-amd64\` or \`windows-amd64\`. The release's \`SHA256SUMS\` file has their checksums.

- **Server**: replace \`ffmpeg-over-ip-server\`, \`ffmpeg\` and \`ffprobe\` with the three files from the new archive. All three must come from the same release and stay in the same directory.
- **Client**: replace \`ffmpeg-over-ip-client\` and every copy of it, such as an \`ffprobe\` or \`ffprobe.exe\` made with \`cp\` or \`copy\`, or a copy installed under the name \`ffmpeg\`. Symlinks to the client need no change.
- On macOS, downloaded binaries may be quarantined; remove the attribute as shown in [Troubleshooting](/docs/troubleshooting).
- If containers mount the binaries, as in [Docker Integration](/docs/docker), replace the files on the host and then restart the containers.

### Check the result

On each client machine, run:

\`\`\`bash
./ffmpeg-over-ip-client -version
\`\`\`

When the client and server work together, this prints the server's ffmpeg version and build info. Otherwise it prints an error, such as the version mismatch errors in [v6 to v7](#v6-to-v7).

## v6 to v7

7.0.0 changes the wire protocol from version 0x06 to 0x07. Config files, environment variables and the install layout are the same as in v6. The bundled ffmpeg is still jellyfin-ffmpeg 8.1.3-2, rebuilt with v7's changes to the fio layer.

### What breaks

A client and a server from different releases no longer work together, so upgrade the server and every client at the same time. Transcodes fail from the moment you upgrade the first side until you have upgraded the other.

A v5 or v6 client connecting to a v7 server prints:

\`\`\`
server error: invalid command: protocol version mismatch: client sent 0x06, server expects 0x07; client and server must run the same ffmpeg-over-ip release
\`\`\`

A v7 client connecting to a v5 or v6 server prints:

\`\`\`
server error: expected command message (0x01), got 0x07 (the server runs a different ffmpeg-over-ip release; client and server must run the same release)
\`\`\`

The client exits with code 1 in both cases. \`fallbackToLocal\` doesn't cover this: the client runs the local ffmpeg only when it can't connect to the server at all.

The server's \`ffmpeg\` and \`ffprobe\` must be the v7 builds too. The v7 server gives each ffmpeg it starts a random token, and only accepts ffmpeg's file tunnel after ffmpeg sends the token back. The v6 builds don't send it.

### What else changes

- Each ffmpeg or ffprobe run starts one network round trip later. The client and server now each prove they know the secret before the command runs.
- \`FFOIP_RANGE_CACHE_BYTES\` now limits the range cache of the whole ffmpeg process. In v6 the limit applied to each open file separately. If you set it, check the value against the new meaning. See [Performance Tuning](/docs/configuration#performance-tuning).
- Two-pass encoding works. ffmpeg's pass log is written to the client through the tunnel; in v6, \`-pass 1\` failed with \`Cannot write log file\`. The stats files that libx264, libx265 and libvvenc write themselves still go to the server's disk, as listed in [Files opened on the server](/docs/troubleshooting#files-opened-on-the-server).
- The \`subtitles\` and \`ass\` filters read the subtitle file and the fonts in \`fontsdir\` from the client. In v6 they were opened on the server's disk, so if you copied these files to the server, they now have to exist on the client at the path the command uses. System fonts that libass finds through fontconfig still come from the server.
- Path checks and directory listings run on the client. \`-n\` now refuses to overwrite an output file that exists on the client; in v6 it checked the server's disk and overwrote the client's file. Image-sequence inputs and image2's \`-ts_from_file\` now work.
- Killing a client, or closing its connection, stops its ffmpeg on the server at once. Stopping the server with SIGINT or SIGTERM stops every running ffmpeg and waits for them to exit.
- The server limits how much data connections that haven't finished the handshake can make it buffer. A client turned away by that limit gets \`server busy\`; see [Troubleshooting](/docs/troubleshooting).

### Use a long random secret

Since you are changing both sides anyway, this is a good time to replace a short or guessable secret. Someone who records a session can test guesses of the secret against it offline, so the [Security](/#security) section recommends a long random secret, such as the output of:

\`\`\`bash
openssl rand -hex 32
\`\`\`

Put the same value in \`authSecret\`, or in \`FFMPEG_OVER_IP_SERVER_AUTH_SECRET\` and \`FFMPEG_OVER_IP_CLIENT_AUTH_SECRET\`, on the server and on every client. The install scripts don't change an existing config file, so edit it yourself.

### Checklist

1. Stop the server, replace \`ffmpeg-over-ip-server\`, \`ffmpeg\` and \`ffprobe\` with the v7 files, set a new secret if you are changing it, and start the server.
2. On every client machine, replace \`ffmpeg-over-ip-client\` and every copy of it (on Windows, including \`ffprobe.exe\`), and set the new secret if you changed it.
3. Run \`ffmpeg-over-ip-client -version\` on every client machine. It should print \`ffmpeg version 8.1.3-Jellyfin\` and the build info, not one of the errors above.
4. If you set \`FFOIP_RANGE_CACHE_BYTES\`, check that the value suits a limit for the whole ffmpeg process.

## v5 to v6

6.0.0 moves the bundled \`ffmpeg\` and \`ffprobe\` from jellyfin-ffmpeg 7.1.3-3 (FFmpeg 7.1.3) to 8.1.3-2 (FFmpeg 8.1.3) on every platform. Nothing else changed for users: the client and server code is the same as in 5.2.1, and v5 config files work unchanged. Minimum platforms are also unchanged: glibc 2.28 on Linux and macOS 12.0.

Every 5.x release and 6.0.0 use wire protocol version 0x06 with the same messages, so their clients and servers work with each other, and you can upgrade one machine at a time.

### What breaks

- Commands that depend on options, codecs or defaults that FFmpeg 8 changed or removed. This covers the commands your media server sends and the server's \`rewrites\`, which produce part of the final command line.
- The experimental \`sonic\` and \`sonicls\` encoders are no longer included, because jellyfin-ffmpeg now builds with \`--disable-unstable\`.

v6 also adds hardware acceleration to the bundled ffmpeg; the [CHANGELOG](https://github.com/steelbrain/ffmpeg-over-ip/blob/main/CHANGELOG.md) lists it.

### What to do

v6 only changes the server's ffmpeg, so test what your media server runs against it. Set \`"log": "stdout"\` and \`"debug": true\` in the server config to log each command's arguments before and after rewrites. ffmpeg's errors reach the client's stderr as usual, because the server forwards ffmpeg's output to the client.

### Checklist

1. Replace the server's \`ffmpeg-over-ip-server\`, \`ffmpeg\` and \`ffprobe\` with the v6 files, or with the v7 files if you are continuing to v7.
2. Remove \`sonic\` and \`sonicls\` from command lines and rewrites.
3. Run your usual transcodes with server debug logging on, and fix any command FFmpeg 8 rejects. For commands that come from your media server, a server \`rewrites\` rule can change the arguments.
4. Replace the client. A 5.x client works with a v6 server, so this can wait, but v7 needs both sides upgraded.

## Within v5

Of the 5.x releases, only 5.2.0 needs a config change: it changed how \`rewrites\` match. Every 5.x release uses wire protocol version 0x06, so the client and server can be upgraded separately.

### 5.2.0: rewrites match whole arguments

Up to 5.1.0, each \`["find", "replace"]\` pair replaced \`find\` wherever it appeared inside an argument, as a plain substring. From 5.2.0 on, \`find\` has to equal a whole argument. A \`find\` that contains spaces is split into words that must match consecutive arguments, and \`replace\` is split into separate arguments the same way.

A substring rule like \`["nvenc", "qsv"]\` no longer matches \`h264_nvenc\` or \`hevc_nvenc\`. Write one rule per full argument instead:

\`\`\`jsonc
// Up to 5.1.0
"rewrites": [
  ["nvenc", "qsv"],
],

// 5.2.0 and later
"rewrites": [
  ["h264_nvenc", "h264_qsv"],
  ["hevc_nvenc", "hevc_qsv"],
],
\`\`\`

Also check these cases:

- A rule whose \`find\` is already a whole argument, such as \`["h264_nvenc", "h264_qsv"]\`, still matches that argument, but it no longer changes longer arguments that contain it, such as a filter graph passed to \`-vf\` or \`-filter_complex\`. A rewrite can't change part of an argument.
- A \`replace\` that contains spaces now becomes several arguments. Up to 5.1.0 it was inserted as one argument with the spaces inside it.

Set \`"debug": true\` in the server config to log each command's arguments before and after rewrites. The client's \`fallbackRewrites\`, added in 5.2.0, follow the same rules. See [Rewrites](/docs/configuration#rewrites).

### Other 5.x changes

None of these need a config change. The last column says which side has to be upgraded to get the change.

| Release | Change | Side |
|---|---|---|
| 5.0.1 | ffmpeg's \`pipe:\` and \`fd:\` protocols work | server |
| 5.1.0 | One-line install scripts, and configuration through \`FFMPEG_OVER_IP_CLIENT_*\` and \`FFMPEG_OVER_IP_SERVER_*\` environment variables | both |
| 5.2.0 | Optional fallback to the client machine's own ffmpeg when the server is unreachable (\`fallbackToLocal\`) | client |
| 5.2.1 | Read-ahead, prefetch and a range cache for tunneled reads, on by default and tuned with \`FFOIP_READAHEAD_BYTES\` and \`FFOIP_RANGE_CACHE_BYTES\` (see [Performance Tuning](/docs/configuration#performance-tuning)) | both |

### Checklist

1. If the server config has \`rewrites\`, rewrite each rule in whole-argument form and check the debug log.
2. Replace the server's \`ffmpeg-over-ip-server\`, \`ffmpeg\` and \`ffprobe\`, and the client binary.

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

#### \`ffmpegPath\` removed

v4 required you to specify the path to ffmpeg on the server:

\`\`\`jsonc
// v4 (remove this)
"ffmpegPath": "/usr/bin/ffmpeg"
\`\`\`

v5 ships patched \`ffmpeg\` and \`ffprobe\` binaries in the release. The server finds them automatically in the same directory as its own binary. Place all three files together and remove \`ffmpegPath\` from your config.

#### Unix socket address format changed

v4 used bare paths for Unix sockets:

\`\`\`jsonc
// v4
"address": "/tmp/ffmpeg-over-ip.sock"
\`\`\`

v5 uses the \`unix:\` prefix (following the gRPC/Docker convention):

\`\`\`jsonc
// v5
"address": "unix:/tmp/ffmpeg-over-ip.sock"
\`\`\`

TCP addresses (\`host:port\`) are unchanged.

#### \`--config\` removed from client

v4 supported \`--config\` on both client and server. v5 only supports it on the server, since the client forwards all arguments to ffmpeg. Use environment variables or config file search paths instead:

\`\`\`bash
FFMPEG_OVER_IP_CLIENT_CONFIG="/path/to/config.jsonc" ffmpeg-over-ip-client ...
\`\`\`

Since 5.1.0 you can also set the address and secret in environment variables, with no config file:

\`\`\`bash
FFMPEG_OVER_IP_CLIENT_ADDRESS=192.168.1.100:5050 \\
FFMPEG_OVER_IP_CLIENT_AUTH_SECRET=your-secret \\
ffmpeg-over-ip-client ...
\`\`\`

See [Environment Variables](/docs/configuration#environment-variables) for the full list.

### New Features in v5

#### No shared filesystem required

The biggest change. See [How It Works](/#how).

#### Pre-built ffmpeg binaries

Releases include patched \`ffmpeg\` and \`ffprobe\` with hardware acceleration support (NVENC, QSV, VAAPI, AMF, VideoToolbox, and more) built on [jellyfin-ffmpeg](https://github.com/jellyfin/jellyfin-ffmpeg). No need to install ffmpeg separately on the server.

#### Block comments

Config files now support \`/* */\` block comments in addition to \`//\` line comments.

### Checklist

1. Download the latest release (the server archive includes \`ffmpeg-over-ip-server\`, \`ffmpeg\`, and \`ffprobe\`)
2. Place all three server binaries in the same directory
3. Remove \`ffmpegPath\` from your server config
4. If using Unix sockets, add the \`unix:\` prefix to your address
5. Remove path rewrites from your server config (no longer needed without shared filesystems)
6. Remove any shared filesystem mounts that were only needed for ffmpeg-over-ip
7. If using \`--config\` on the client, switch to the \`FFMPEG_OVER_IP_CLIENT_CONFIG\` env var
`,
  },
]

export function getDoc(slug: string): DocPage | undefined {
  return docs.find((d) => d.slug === slug)
}
