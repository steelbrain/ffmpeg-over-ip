# Docker Integration

You can use ffmpeg-over-ip in Docker environments by mounting the binary and passing configuration via environment variables. This allows containers to use ffmpeg remotely without needing GPU passthrough or other special setup.

## Client in Docker

Mount the client binary as `/usr/bin/ffmpeg` and pass config via environment variables:

```bash
docker run \
  -e FFMPEG_OVER_IP_CLIENT_ADDRESS=192.168.1.100:5050 \
  -e FFMPEG_OVER_IP_CLIENT_AUTH_SECRET=your-secret \
  -v ./ffmpeg-over-ip-client:/usr/bin/ffmpeg \
  your-image
```

For ffprobe support, add a second mount:

```bash
docker run \
  -e FFMPEG_OVER_IP_CLIENT_ADDRESS=192.168.1.100:5050 \
  -e FFMPEG_OVER_IP_CLIENT_AUTH_SECRET=your-secret \
  -v ./ffmpeg-over-ip-client:/usr/bin/ffmpeg \
  -v ./ffmpeg-over-ip-client:/usr/bin/ffprobe \
  your-image
```

You can also use a config file volume instead of env vars — see [Configuration](configuration.md) for details.

## Server in Docker

```bash
docker run \
  -e FFMPEG_OVER_IP_SERVER_ADDRESS=0.0.0.0:5050 \
  -e FFMPEG_OVER_IP_SERVER_AUTH_SECRET=your-secret \
  -v ./ffmpeg-over-ip-server:/usr/bin/ffmpeg-over-ip-server \
  -v ./ffmpeg:/usr/bin/ffmpeg \
  -v ./ffprobe:/usr/bin/ffprobe \
  --runtime=nvidia \
  your-image
```

All three binaries (`ffmpeg-over-ip-server`, `ffmpeg`, `ffprobe`) must be in the same directory inside the container.

## Unix Sockets (same-machine setups)

If the server and client run on the same machine (e.g., server on the host, client in a container), use a Unix socket instead of TCP to avoid network overhead and `host.docker.internal` configuration:

Start the server with a socket address (config file or env var):

```bash
export FFMPEG_OVER_IP_SERVER_ADDRESS=unix:/tmp/ffmpeg-over-ip.sock
export FFMPEG_OVER_IP_SERVER_AUTH_SECRET=your-secret
./ffmpeg-over-ip-server
```

Then mount the socket into the container:

```bash
docker run \
  -e FFMPEG_OVER_IP_CLIENT_ADDRESS=unix:/tmp/ffmpeg-over-ip.sock \
  -e FFMPEG_OVER_IP_CLIENT_AUTH_SECRET=your-secret \
  -v /tmp/ffmpeg-over-ip.sock:/tmp/ffmpeg-over-ip.sock \
  -v ./ffmpeg-over-ip-client:/usr/bin/ffmpeg \
  your-image
```

## Debugging

Enable debug logging on the server via env vars:

```bash
docker run \
  -e FFMPEG_OVER_IP_SERVER_ADDRESS=0.0.0.0:5050 \
  -e FFMPEG_OVER_IP_SERVER_AUTH_SECRET=your-secret \
  -e FFMPEG_OVER_IP_SERVER_LOG=stdout \
  -e FFMPEG_OVER_IP_SERVER_DEBUG=true \
  -v ./ffmpeg-over-ip-server:/usr/bin/ffmpeg-over-ip-server \
  -v ./ffmpeg:/usr/bin/ffmpeg \
  -v ./ffprobe:/usr/bin/ffprobe \
  --runtime=nvidia \
  your-image
```

For the client, log to a file you can tail from inside the container:

```bash
docker run \
  -e FFMPEG_OVER_IP_CLIENT_ADDRESS=192.168.1.100:5050 \
  -e FFMPEG_OVER_IP_CLIENT_AUTH_SECRET=your-secret \
  -e FFMPEG_OVER_IP_CLIENT_LOG=/tmp/ffmpeg-over-ip.client.log \
  -v ./ffmpeg-over-ip-client:/usr/bin/ffmpeg \
  your-image
```

Then tail it:

```bash
docker exec -it <container> tail -f /tmp/ffmpeg-over-ip.client.log
```

Note: rewrites require a config file — they can't be set via env vars. If you need rewrites alongside env var config, use `FFMPEG_OVER_IP_SERVER_CONFIG` to point to a config file instead.
