# Quick Start

Download the latest release for your platform from the [releases page](https://github.com/steelbrain/ffmpeg-over-ip-v5/releases/latest).

## Server Setup (GPU machine)

1. Extract the release. Keep `ffmpeg-over-ip-server`, `ffmpeg`, and `ffprobe` in the same directory — the server finds them automatically.

2. Create a config file `ffmpeg-over-ip.server.jsonc` in the same directory:

```jsonc
{
  "address": "0.0.0.0:5050",
  "authSecret": "pick-a-strong-secret"
}
```

3. Make sure port 5050 is open (firewall, cloud security group, etc.), then start the server:

```bash
./ffmpeg-over-ip-server
```

## Client Setup (media server)

1. Extract `ffmpeg-over-ip-client` somewhere convenient.

2. Create a config file `ffmpeg-over-ip.client.jsonc` next to the binary or in your home directory:

```jsonc
{
  "address": "192.168.1.100:5050",  // your server's IP
  "authSecret": "pick-a-strong-secret"
}
```

3. Set up ffprobe so your media server can probe files too — symlink or copy the client binary:

```bash
ln -s ffmpeg-over-ip-client ffprobe
# or: cp ffmpeg-over-ip-client ffprobe
```

4. Point your media server at both binaries. For Jellyfin, set the FFmpeg path in Dashboard > Playback to `ffmpeg-over-ip-client`.

## Windows

On Windows, use the `.exe` binaries from the release. Config files work the same way — place `ffmpeg-over-ip.client.jsonc` next to the binary or in your user directory (`C:\Users\<you>\`). For ffprobe, copy and rename the client binary:

```cmd
copy ffmpeg-over-ip-client.exe ffprobe.exe
```

## Verify

Test the connection by running a command through the client:

```bash
./ffmpeg-over-ip-client -version
```

You should see the server's ffmpeg version and build info. If you get a connection error, double-check the address, port, and auth secret.
