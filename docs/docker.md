# Docker Integration

You can use ffmpeg-over-ip in Docker environments by mounting the binary and configuration as volumes. This allows containers to use ffmpeg remotely without needing GPU passthrough or other special setup.

## Client in Docker

Mount the client binary as `/usr/bin/ffmpeg` so your app uses it transparently:

```bash
docker run \
  -v ./ffmpeg-over-ip-client:/usr/bin/ffmpeg \
  -v ./ffmpeg-over-ip.client.jsonc:/etc/ffmpeg-over-ip.client.jsonc \
  your-image
```

For ffprobe support, add a symlink or a second mount:

```bash
docker run \
  -v ./ffmpeg-over-ip-client:/usr/bin/ffmpeg \
  -v ./ffmpeg-over-ip-client:/usr/bin/ffprobe \
  -v ./ffmpeg-over-ip.client.jsonc:/etc/ffmpeg-over-ip.client.jsonc \
  your-image
```

## Server in Docker

```bash
docker run \
  -v ./ffmpeg-over-ip-server:/usr/bin/ffmpeg-over-ip-server \
  -v ./ffmpeg:/usr/bin/ffmpeg \
  -v ./ffprobe:/usr/bin/ffprobe \
  -v ./ffmpeg-over-ip.server.jsonc:/etc/ffmpeg-over-ip.server.jsonc \
  --runtime=nvidia \
  your-image
```

All three binaries (`ffmpeg-over-ip-server`, `ffmpeg`, `ffprobe`) must be in the same directory inside the container.

## Unix Sockets (same-machine setups)

If the server and client run on the same machine (e.g., server on the host, client in a container), use a Unix socket instead of TCP to avoid network overhead and `host.docker.internal` configuration:

Server config:
```jsonc
{
  "address": "unix:/tmp/ffmpeg-over-ip.sock",
  "authSecret": "your-secret",
}
```

Client config:
```jsonc
{
  "address": "unix:/tmp/ffmpeg-over-ip.sock",
  "authSecret": "your-secret",
}
```

Then mount the socket into the container:

```bash
docker run \
  -v /tmp/ffmpeg-over-ip.sock:/tmp/ffmpeg-over-ip.sock \
  -v ./ffmpeg-over-ip-client:/usr/bin/ffmpeg \
  -v ./ffmpeg-over-ip.client.jsonc:/etc/ffmpeg-over-ip.client.jsonc \
  your-image
```

## Debugging

Enable `debug` and `log` in the server config to see what commands are being executed and how rewrites are applied:

```jsonc
{
  "address": "0.0.0.0:5050",
  "authSecret": "your-secret",
  "log": "stdout",
  "debug": true,
  "rewrites": [
    ["h264_nvenc", "h264_qsv"],
  ],
}
```

For the client, set the log to a file you can tail from inside the container:

```jsonc
{
  "address": "192.168.1.100:5050",
  "authSecret": "your-secret",
  "log": "/tmp/ffmpeg-over-ip.client.log",
}
```

Then tail it:

```bash
docker exec -it <container> tail -f /tmp/ffmpeg-over-ip.client.log
```
