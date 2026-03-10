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

See [docs/quick-start.md](docs/quick-start.md) to get up and running in a few minutes.

## Upgrading from v4

See [docs/upgrading.md](docs/upgrading.md) for migration guide and breaking changes.

## Configuration

See [docs/configuration.md](docs/configuration.md) for full configuration reference (config file search paths, server/client options, rewrites, logging, address formats).

## Docker

See [docs/docker.md](docs/docker.md) for Docker integration, Unix socket setup, and debugging tips.

## How It Works

1. Your media server calls `ffmpeg-over-ip-client` with normal ffmpeg arguments
2. The client connects to the server and sends the command with HMAC authentication
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
| Windows arm64 | ✓ | — |


## Troubleshooting

See [docs/troubleshooting.md](docs/troubleshooting.md) for common issues and debugging tips.

## Building from Source

See [CONTRIBUTING.md](CONTRIBUTING.md) for build instructions, running tests, and project structure.

## Security

- **Authentication**: HMAC-SHA256 with a shared secret. Every command is signed.
- **Single port**: Only the server listens on a port. The client makes outbound connections only.

## License

Split license — see [LICENSE.md](LICENSE.md). The fio layer and ffmpeg patches (`fio/`, `patches/`) are GPL v3 (derived from ffmpeg). Everything else is MIT.
