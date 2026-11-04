# Troubleshooting

**Connection refused** — Check that the server is running, the address and port match your config, and the port is reachable (firewall, cloud security group, Docker network). Also verify the server is listening on the right host — `127.0.0.1:5050` only accepts local connections. Use `0.0.0.0:5050` to listen on all interfaces.

> **Tip:** To test whether the port is reachable, run `telnet <server-ip> <port>` from the client machine. If telnet can't connect, a firewall or security group is blocking the port — fix that before troubleshooting ffmpeg-over-ip itself.

**Authentication failed** — The `authSecret` must match exactly between client and server configs. The server rejects a client with a different secret: the client prints `server error: authentication failed`, and the server logs `auth failed from <address>`. If the client instead prints `the server accepted the command but did not prove it knows the auth secret`, something other than your ffmpeg-over-ip server answered at the configured address.

**Protocol version mismatch** — The server refuses a client from a different ffmpeg-over-ip release with `protocol version mismatch: client sent 0x.., server expects 0x..`. A client talking to a server from a release before 0x07 gets `server error: expected command message (0x01), got 0x07`. Upgrade the client and server together so both run the same release.

**Server busy** — `server error: server busy: too much data from connections that haven't authenticated yet` means connections that hadn't finished the handshake were holding as much unverified data as the server buffers: 64 MiB in all, or 16 MiB from your client's IP address (its /64 for IPv6). The server logs `handshake with <address> failed: ... too much data buffered for unauthenticated connections`. Each such connection is dropped after at most 10 seconds, so retry; if it keeps happening, something on the network is flooding the server's port.

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

## Files opened on the server

ffmpeg-over-ip tunnels ffmpeg's file access to the client, including inputs and outputs, HLS/DASH segments and playlists, concat lists, subtitle files and `fontsdir` fonts, and two-pass logs. A few ffmpeg features open files themselves, outside that layer, so they read or write the server's disk instead:

- the two-pass stats files of libx264, libx265 and libvvenc (their `stats` option, which `-passlogfile` sets for them)
- filter option files: `stats_file` of `psnr`, `ssim`, `xpsnr`, `vmafmotion` and `loudnorm`; `filename` of `sendcmd`/`asendcmd` and `deshake`; `psfile` and `plot` of `curves`; `psfile` of `selectivecolor`; `file` of `lut3d` and `lut1d`; `fontfile` and `textfile` of `drawtext`; `textfile` of `flite` and `qrencode`; `lut` and `custom_shader_path` of `libplacebo`; `model` of `arnndn`; `dumpfile` of `firequalizer`; `hint` of `fieldhint`; `weights` of `nnedi`; `labels` of `dnn_detect` and `dnn_classify`; `debug_kdtree` of `paletteuse`
- `-vstats_file`, `-fpre`, `-report` and `FFREPORT`, the `bluray:` protocol, image2's `-pattern_type glob`, and the DVD subtitle decoder's `ifo_palette`
- the HDS and Smooth Streaming muxers create their output directories and delete old fragments on the server (the fragments and manifests themselves go through the tunnel)
- system fonts libass finds through fontconfig

Use paths that exist on the server for these, or keep them off the command line.

