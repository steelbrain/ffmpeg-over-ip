# Upgrading from v4 to v5

## Architecture Change

v4 required a **shared filesystem** (Docker mount, NFS, or SMB) between client and server to transfer video data. The client and server coordinated commands over a lightweight protocol, but actual file I/O happened through the shared filesystem.

v5 **eliminates the shared filesystem entirely**. The server runs a patched ffmpeg that tunnels all file I/O (reads, writes, seeks, stat, etc.) back to the client over the TCP connection. Files are never stored on the server, and no shared storage is needed.

This means:
- No more NFS/SMB mount setup or maintenance
- No more path mapping between client and server filesystems
- Works across networks where shared storage isn't practical

## Breaking Config Changes

### `ffmpegPath` removed

v4 required you to specify the path to ffmpeg on the server:

```jsonc
// v4 (remove this)
"ffmpegPath": "/usr/bin/ffmpeg"
```

v5 ships patched `ffmpeg` and `ffprobe` binaries in the release. The server finds them automatically in the same directory as its own binary. Place all three files together and remove `ffmpegPath` from your config.

### Unix socket address format changed

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

### `--config` removed from client

v4 supported `--config` on both client and server. v5 only supports it on the server, since the client forwards all arguments to ffmpeg. Use environment variables or config file search paths instead:

```bash
FFMPEG_OVER_IP_CLIENT_CONFIG="/path/to/config.jsonc" ffmpeg-over-ip-client ...
```

## New Features in v5

### No shared filesystem required

The biggest change. See the "How It Works" section in the [README](../README.md#how-it-works).

### Pre-built ffmpeg binaries

Releases include patched `ffmpeg` and `ffprobe` with hardware acceleration support (NVENC, QSV, VAAPI, AMF, VideoToolbox, and more) built on [jellyfin-ffmpeg](https://github.com/jellyfin/jellyfin-ffmpeg). No need to install ffmpeg separately on the server.

### Block comments

Config files now support `/* */` block comments in addition to `//` line comments.

## Migration Checklist

1. Download the v5 release (includes `ffmpeg-over-ip-server`, `ffmpeg`, and `ffprobe`)
2. Place all three server binaries in the same directory
3. Remove `ffmpegPath` from your server config
4. If using Unix sockets, add the `unix:` prefix to your address
5. Remove path rewrites from your server config (no longer needed without shared filesystems)
6. Remove any shared filesystem mounts that were only needed for ffmpeg-over-ip
7. If using `--config` on the client, switch to the `FFMPEG_OVER_IP_CLIENT_CONFIG` env var
