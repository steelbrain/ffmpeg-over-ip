# Recording the session on the home page

`app/components/session/recording.json` is a real ffmpeg-over-ip session, captured
with the tools in this directory. Re-record it when the protocol or the
output changes, so the home page keeps showing what the current release does.

- `tracer/` is a TCP proxy that sits between client and server, forwards
  every byte unchanged, and writes one JSON line per protocol message (time,
  direction, type, decoded fields). It is its own Go module, so the repo's
  `go test ./...` does not build it.
- `process.py` pairs each file request with the client's reply, adds the
  server log, and writes `recording.json`.

The 7.0.0 recording was made on an Apple Silicon Mac with OrbStack: the server
ran on the host (VideoToolbox), the client in an Alpine container with no GPU
access. Steps, from the repo root:

```bash
REC=/tmp/foip-rec
mkdir -p $REC/gpu $REC/media/transcodes/test-pattern $REC/trace "$REC/media/movies/Test Pattern (2026)"

# Server: the release package, so the bundled ffmpeg is the shipped one.
# Download macos-arm64-ffmpeg-over-ip-server.zip for the release and check it
# against the release's SHA256SUMS, then:
unzip macos-arm64-ffmpeg-over-ip-server.zip -d $REC/gpu

# Client and tracer, built for the container.
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o $REC/media/ffmpeg-over-ip-client ./cmd/client
(cd website/scripts/record-session/tracer && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o $REC/tracer .)

# Input: a 30 s 1080p test pattern, H.264 + AAC in Matroska.
$REC/gpu/ffmpeg -hide_banner -loglevel error \
  -f lavfi -i testsrc2=size=1920x1080:rate=24 -f lavfi -i sine=frequency=440:sample_rate=48000 \
  -t 30 -c:v libx264 -preset veryfast -b:v 8M -maxrate 8M -bufsize 16M -pix_fmt yuv420p \
  -c:a aac -b:a 192k -ac 2 -metadata title="Test Pattern" \
  "$REC/media/movies/Test Pattern (2026)/Test Pattern (2026).mkv"

# Start the server.
SECRET=$(openssl rand -hex 32)
printf '{\n  "address": "127.0.0.1:5050",\n  "authSecret": "%s",\n  "log": "stdout"\n}\n' "$SECRET" \
  > $REC/gpu/ffmpeg-over-ip.server.jsonc
(cd $REC/gpu && ./ffmpeg-over-ip-server > $REC/server.log 2>&1 &)

# Run the client in a container, through the tracer.
docker run --rm \
  -v $REC/media/movies:/media/movies \
  -v $REC/media/transcodes:/transcodes \
  -v $REC/media/ffmpeg-over-ip-client:/usr/bin/ffmpeg:ro \
  -v $REC/tracer:/usr/local/bin/tracer:ro \
  -v $REC/trace:/trace \
  -e FFMPEG_OVER_IP_CLIENT_ADDRESS=127.0.0.1:5050 \
  -e FFMPEG_OVER_IP_CLIENT_AUTH_SECRET="$SECRET" \
  alpine:3.20 sh -c '
    tracer -listen 127.0.0.1:5050 -upstream host.docker.internal:5050 -out /trace/transcode.jsonl &
    sleep 0.5
    ffmpeg -hide_banner -i "/media/movies/Test Pattern (2026)/Test Pattern (2026).mkv" -map 0:v:0 -map 0:a:0 -c:v h264_videotoolbox -b:v 6M -c:a aac -b:a 192k -f hls -hls_time 4 -hls_playlist_type vod -hls_segment_filename "/transcodes/test-pattern/seg%d.ts" /transcodes/test-pattern/main.m3u8
    sleep 0.3'

# Stop the server, then convert. --command must match what ran in the container.
python3 website/scripts/record-session/process.py $REC/trace/transcode.jsonl $REC/server.log \
  website/app/components/session/recording.json --version 7.0.0 --date 2026-11-15 \
  --command 'ffmpeg -hide_banner -i "/media/movies/Test Pattern (2026)/Test Pattern (2026).mkv" -map 0:v:0 -map 0:a:0 -c:v h264_videotoolbox -b:v 6M -c:a aac -b:a 192k -f hls -hls_time 4 -hls_playlist_type vod -hls_segment_filename "/transcodes/test-pattern/seg%d.ts" /transcodes/test-pattern/main.m3u8'
(cd website && yarn format)
```

The home page computes its numbers (handshake time, READ and WRITE counts,
bytes) from `recording.json`, but the prose around the player names the setup
(macOS host, VideoToolbox, OrbStack, the input file). Update `app/page.tsx` if
you record on different hardware.
