# Troubleshooting

**Connection refused** — Check that the server is running, the address and port match your config, and the port is reachable (firewall, cloud security group, Docker network). Also verify the server is listening on the right host — `127.0.0.1:5050` only accepts local connections. Use `0.0.0.0:5050` to listen on all interfaces.

**Authentication failed** — The `authSecret` must match exactly between client and server configs.

**Codec not found / encoder not available** — The server's ffmpeg may not support the requested codec. Use `rewrites` in the server config to map unsupported codecs to available ones (e.g., `["h264_nvenc", "h264_qsv"]`). See [configuration.md](configuration.md#rewrites).

**ffprobe not working** — The client detects ffprobe mode from its binary name. The binary or symlink must contain "ffprobe" in the name. See [configuration.md](configuration.md#ffprobe).

**Server can't find ffmpeg** — `ffmpeg` and `ffprobe` must be in the same directory as `ffmpeg-over-ip-server`.

**macOS Gatekeeper error** — Downloaded binaries may be quarantined by macOS. You may see `"ffmpeg-over-ip" Not Opened — Apple could not verify "ffmpeg-over-ip" is free of malware that may harm your Mac or compromise your privacy`, or `Killed: 9` when running from the terminal. Remove the quarantine attribute:

```bash
# Server package
xattr -dr com.apple.quarantine ffmpeg ffprobe ffmpeg-over-ip-server

# Client package
xattr -dr com.apple.quarantine ffmpeg-over-ip-client
```

**Need more detail?** — Enable logging on both sides. Set `"log": "stdout"` and `"debug": true` on the server to see the commands being executed. Set `"log": "/tmp/ffmpeg-over-ip.log"` on the client to capture client-side activity.
