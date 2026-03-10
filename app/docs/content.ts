export interface DocPage {
  slug: string
  title: string
  content: string
}

export const docs: DocPage[] = [
  {
    slug: "quick-start",
    title: "Quick Start",
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
`,
  },
  {
    slug: "configuration",
    title: "Configuration",
    content: `
Config files use [JSONC](https://code.visualstudio.com/docs/languages/json#_json-with-comments) format (JSON with \`//\` comments, \`/* */\` block comments, and trailing commas).

## Config File Search Paths

Config is loaded from the first file found (in order):

1. \`FFMPEG_OVER_IP_SERVER_CONFIG\` / \`FFMPEG_OVER_IP_CLIENT_CONFIG\` env var
2. Next to the binary: \`<exe-dir>/ffmpeg-over-ip.{server,client}.jsonc\`
3. \`./ffmpeg-over-ip.{server,client}.jsonc\`
4. \`./.ffmpeg-over-ip.{server,client}.jsonc\`
5. \`~/.ffmpeg-over-ip.{server,client}.jsonc\`
6. \`~/.config/ffmpeg-over-ip.{server,client}.jsonc\`
7. \`/etc/ffmpeg-over-ip.{server,client}.jsonc\`
8. \`/usr/local/etc/ffmpeg-over-ip.{server,client}.jsonc\`

The server also accepts \`--config <path>\`.

On Windows, \`~\` is your user directory (e.g., \`C:\\Users\\<you>\`). Paths 7 and 8 don't apply — use the binary directory, current directory, or an environment variable instead.

To see which paths are searched on your system, run:

\`\`\`bash
ffmpeg-over-ip-server --debug-print-search-paths
ffmpeg-over-ip-client --debug-print-search-paths
\`\`\`

## Server Config

\`\`\`jsonc
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
\`\`\`

The server looks for \`ffmpeg\` and \`ffprobe\` in the same directory as its own binary. Ship all three together.

## Client Config

\`\`\`jsonc
{
  "address": "192.168.1.100:5050",
  "authSecret": "your-secret-here",
  // Optional: see "Log" section below
  "log": "/tmp/ffmpeg-over-ip.log",
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

## Rewrites

Rewrites let the server substitute strings in ffmpeg arguments before running the command. This is useful when the client requests a codec the server doesn't have — for example, the client asks for \`h264_nvenc\` but the server has Intel QSV instead of NVIDIA.

\`\`\`jsonc
{
  "rewrites": [
    ["h264_nvenc", "h264_qsv"],
    ["hevc_nvenc", "hevc_qsv"],
  ],
}
\`\`\`

Each pair \`["from", "to"]\` does a plain string replacement across all ffmpeg arguments. In the example above, any argument containing \`h264_nvenc\` is rewritten to \`h264_qsv\`.

Enable \`"debug": true\` to log original and rewritten arguments for each command.

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
`,
  },
  {
    slug: "docker",
    title: "Docker Integration",
    content: `
You can use ffmpeg-over-ip in Docker environments by mounting the binary and configuration as volumes. This allows containers to use ffmpeg remotely without needing GPU passthrough or other special setup.

## Client in Docker

Mount the client binary as \`/usr/bin/ffmpeg\` so your app uses it transparently:

\`\`\`bash
docker run \\
  -v ./ffmpeg-over-ip-client:/usr/bin/ffmpeg \\
  -v ./ffmpeg-over-ip.client.jsonc:/etc/ffmpeg-over-ip.client.jsonc \\
  your-image
\`\`\`

For ffprobe support, add a symlink or a second mount:

\`\`\`bash
docker run \\
  -v ./ffmpeg-over-ip-client:/usr/bin/ffmpeg \\
  -v ./ffmpeg-over-ip-client:/usr/bin/ffprobe \\
  -v ./ffmpeg-over-ip.client.jsonc:/etc/ffmpeg-over-ip.client.jsonc \\
  your-image
\`\`\`

## Server in Docker

\`\`\`bash
docker run \\
  -v ./ffmpeg-over-ip-server:/usr/bin/ffmpeg-over-ip-server \\
  -v ./ffmpeg:/usr/bin/ffmpeg \\
  -v ./ffprobe:/usr/bin/ffprobe \\
  -v ./ffmpeg-over-ip.server.jsonc:/etc/ffmpeg-over-ip.server.jsonc \\
  --runtime=nvidia \\
  your-image
\`\`\`

All three binaries (\`ffmpeg-over-ip-server\`, \`ffmpeg\`, \`ffprobe\`) must be in the same directory inside the container.

## Unix Sockets (same-machine setups)

If the server and client run on the same machine (e.g., server on the host, client in a container), use a Unix socket instead of TCP to avoid network overhead and \`host.docker.internal\` configuration:

Server config:
\`\`\`jsonc
{
  "address": "unix:/tmp/ffmpeg-over-ip.sock",
  "authSecret": "your-secret",
}
\`\`\`

Client config:
\`\`\`jsonc
{
  "address": "unix:/tmp/ffmpeg-over-ip.sock",
  "authSecret": "your-secret",
}
\`\`\`

Then mount the socket into the container:

\`\`\`bash
docker run \\
  -v /tmp/ffmpeg-over-ip.sock:/tmp/ffmpeg-over-ip.sock \\
  -v ./ffmpeg-over-ip-client:/usr/bin/ffmpeg \\
  -v ./ffmpeg-over-ip.client.jsonc:/etc/ffmpeg-over-ip.client.jsonc \\
  your-image
\`\`\`

## Debugging

Enable \`debug\` and \`log\` in the server config to see what commands are being executed and how rewrites are applied:

\`\`\`jsonc
{
  "address": "0.0.0.0:5050",
  "authSecret": "your-secret",
  "log": "stdout",
  "debug": true,
  "rewrites": [
    ["h264_nvenc", "h264_qsv"],
  ],
}
\`\`\`

For the client, set the log to a file you can tail from inside the container:

\`\`\`jsonc
{
  "address": "192.168.1.100:5050",
  "authSecret": "your-secret",
  "log": "/tmp/ffmpeg-over-ip.client.log",
}
\`\`\`

Then tail it:

\`\`\`bash
docker exec -it <container> tail -f /tmp/ffmpeg-over-ip.client.log
\`\`\`
`,
  },
  {
    slug: "troubleshooting",
    title: "Troubleshooting",
    content: `
**Connection refused** — Check that the server is running, the address and port match your config, and the port is reachable (firewall, cloud security group, Docker network). Also verify the server is listening on the right host — \`127.0.0.1:5050\` only accepts local connections. Use \`0.0.0.0:5050\` to listen on all interfaces.

**Authentication failed** — The \`authSecret\` must match exactly between client and server configs.

**Codec not found / encoder not available** — The server's ffmpeg may not support the requested codec. Use \`rewrites\` in the server config to map unsupported codecs to available ones (e.g., \`["h264_nvenc", "h264_qsv"]\`). See [Configuration — Rewrites](/docs/configuration#rewrites).

**ffprobe not working** — The client detects ffprobe mode from its binary name. The binary or symlink must contain "ffprobe" in the name. See [Configuration — ffprobe](/docs/configuration#ffprobe).

**Server can't find ffmpeg** — \`ffmpeg\` and \`ffprobe\` must be in the same directory as \`ffmpeg-over-ip-server\`.

**Need more detail?** — Enable logging on both sides. Set \`"log": "stdout"\` and \`"debug": true\` on the server to see the commands being executed. Set \`"log": "/tmp/ffmpeg-over-ip.log"\` on the client to capture client-side activity.
`,
  },
  {
    slug: "upgrading",
    title: "Upgrading from v4 to v5",
    content: `
## Architecture Change

v4 required a **shared filesystem** (Docker mount, NFS, or SMB) between client and server to transfer video data. The client and server coordinated commands over a lightweight protocol, but actual file I/O happened through the shared filesystem.

v5 **eliminates the shared filesystem entirely**. The server runs a patched ffmpeg that tunnels all file I/O (reads, writes, seeks, stat, etc.) back to the client over the TCP connection. Files are never stored on the server, and no shared storage is needed.

This means:
- No more NFS/SMB mount setup or maintenance
- No more path mapping between client and server filesystems
- Works across networks where shared storage isn't practical

## Breaking Config Changes

### \`ffmpegPath\` removed

v4 required you to specify the path to ffmpeg on the server:

\`\`\`jsonc
// v4 (remove this)
"ffmpegPath": "/usr/bin/ffmpeg"
\`\`\`

v5 ships patched \`ffmpeg\` and \`ffprobe\` binaries in the release. The server finds them automatically in the same directory as its own binary. Place all three files together and remove \`ffmpegPath\` from your config.

### Unix socket address format changed

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

### \`--config\` removed from client

v4 supported \`--config\` on both client and server. v5 only supports it on the server, since the client forwards all arguments to ffmpeg. Use environment variables or config file search paths instead:

\`\`\`bash
FFMPEG_OVER_IP_CLIENT_CONFIG="/path/to/config.jsonc" ffmpeg-over-ip-client ...
\`\`\`

## New Features in v5

### No shared filesystem required

The biggest change. See the [home page](/) for the architecture overview.

### Pre-built ffmpeg binaries

Releases include patched \`ffmpeg\` and \`ffprobe\` with hardware acceleration support (NVENC, QSV, VAAPI, AMF, VideoToolbox, and more) built on [jellyfin-ffmpeg](https://github.com/jellyfin/jellyfin-ffmpeg). No need to install ffmpeg separately on the server.

### Block comments

Config files now support \`/* */\` block comments in addition to \`//\` line comments.

## Migration Checklist

1. Download the v5 release (includes \`ffmpeg-over-ip-server\`, \`ffmpeg\`, and \`ffprobe\`)
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
