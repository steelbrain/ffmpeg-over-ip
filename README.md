# [ffmpeg-over-ip](https://ffmpeg-over-ip.com)

Use GPU-accelerated ffmpeg from anywhere — a Docker container, a VM, or a remote machine — without GPU passthrough or shared filesystems.

## The Problem

GPU transcoding is powerful, but getting access to the GPU is painful:

- **Docker containers** need `--runtime=nvidia`, device mounts, and driver version alignment between host and container
- **Virtual machines** need PCIe passthrough or SR-IOV — complex setup that locks the GPU to one VM
- **Remote machines** need shared filesystems (NFS/SMB) with all the path mapping, mount maintenance, and permission headaches that come with them

You just want your media server to use the GPU for transcoding. You shouldn't need to restructure your infrastructure to make that happen.

## The Solution

Run the ffmpeg-over-ip server on the host (or any machine with a GPU). Point your app at the client binary instead of ffmpeg. Done — your app gets GPU-accelerated transcoding without needing direct GPU access.

The client pretends to be ffmpeg. It forwards arguments to the server, which runs a patched ffmpeg that tunnels all file I/O back through the connection. Files are never stored on the server.

```
CLIENT (has files, no GPU)              SERVER (has GPU)
========================              ===========================

Media server invokes "ffmpeg"         Daemon listening on :5050
        |                                      |
  ffmpeg-over-ip-client               ffmpeg-over-ip-server
        |                                      |
        +--------- TCP connection ------------>+
        |                                      |
  Local filesystem                      patched ffmpeg
  (real files)                    (file I/O tunneled back to client)
```

No GPU passthrough. No shared filesystem. No NFS. No SMB. Just one TCP port.

Releases include pre-built ffmpeg and ffprobe binaries with broad hardware acceleration support (NVENC, QSV, VAAPI, AMF, VideoToolbox, and more) — built on the [jellyfin-ffmpeg](https://github.com/jellyfin/jellyfin-ffmpeg) pipeline. No need to install ffmpeg separately on either side.

## Quick Start

One-line install. Each script downloads the latest release, prompts for host/port/auth secret, and writes a starter config file in the current directory.

**On the GPU machine** (server):

```sh
curl -fsSL https://ffmpeg-over-ip.com/install-server.sh | sh
```

```powershell
irm https://ffmpeg-over-ip.com/install-server.ps1 | iex
```

**On the media-server machine** (client):

```sh
curl -fsSL https://ffmpeg-over-ip.com/install-client.sh | sh
```

```powershell
irm https://ffmpeg-over-ip.com/install-client.ps1 | iex
```

Re-runs are idempotent — already-installed binaries and existing configs are left alone. For manual install or detailed setup, see [docs/quick-start.md](docs/quick-start.md).

## Upgrading from v4

See [docs/upgrading.md](docs/upgrading.md) for migration guide and breaking changes.

## Configuration

See [docs/configuration.md](docs/configuration.md) for full configuration reference (config file search paths, server/client options, rewrites, logging, address formats).

## Docker

See [docs/docker.md](docs/docker.md) for Docker integration, Unix socket setup, and debugging tips.

## How It Works

1. Your media server calls `ffmpeg-over-ip-client` with normal ffmpeg arguments
2. The client connects to the server; each proves to the other that it knows the shared secret, and the client sends the signed command
3. The server launches its patched ffmpeg, which tunnels all file reads and writes back to the client
4. stdout/stderr are forwarded in real-time; when ffmpeg exits, the client exits with the same code

Multiple clients can connect to the same server simultaneously — each session gets its own ffmpeg process.

## Supported Platforms

| | Client | Server + ffmpeg |
|---|---|---|
| Linux x86_64 | ✓ | ✓ |
| Linux arm64 | ✓ | ✓ |
| macOS arm64 | ✓ | ✓ |
| macOS x86_64 | ✓ | ✓ |
| Windows x86_64 | ✓ | ✓ |


## Troubleshooting

See [docs/troubleshooting.md](docs/troubleshooting.md) for common issues and debugging tips.

## Building from Source

See [CONTRIBUTING.md](CONTRIBUTING.md) for build instructions, running tests, and project structure.

## Security

- **Authentication**: Client and server each prove they know the shared secret (an HMAC-SHA256 challenge-response with a fresh nonce from each side). The client proves it first; the server answers with its own proof only after checking the client's, and the client acts on file requests only after checking the server's. A recorded command can't be replayed on a new connection.
- **Use a long random secret**, for example the output of `openssl rand -hex 32`. Someone who records a session can test guesses of the secret against it offline, so a short or common secret can be found.
- **No encryption**: Traffic, including command lines and file contents, is neither encrypted nor integrity-protected. Someone who can intercept and modify the connection can read and alter it, including by relaying the handshake to the real server and then changing what follows. Across a network you don't trust, run it over a VPN or SSH tunnel.
- **What the secret allows**: Anyone with the secret can run ffmpeg on the server with any arguments, and can pose as the server to clients that use it, reading, writing or deleting any file the client's user can reach. File access goes to the client, except for the ffmpeg options listed in [troubleshooting](docs/troubleshooting.md#files-opened-on-the-server), which open files on the server itself.
- **Single port**: Only the server listens on a port. The client makes outbound connections only.

## License

Split license — see [LICENSE.md](LICENSE.md). The fio layer and ffmpeg patches (`fio/`, `patches/`) are GPL v3 (derived from ffmpeg). Everything else is MIT.
