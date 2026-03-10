# Contributing

## Building from Source

### Prerequisites

- Go 1.24+
- A C compiler (gcc or clang)
- Make

### Client

The client has no dependencies beyond the Go standard library.

```bash
go build -o ffmpeg-over-ip-client ./cmd/client
```

### Server (with patched ffmpeg)

```bash
# Build the patched ffmpeg (minimal, for testing)
bash scripts/build-ffmpeg.sh --minimal

# Build the server
go build -o build/ffmpeg-over-ip-server ./cmd/server

# Copy ffmpeg/ffprobe next to the server binary
cp build/ffmpeg/bin/ffmpeg build/
cp build/ffmpeg/bin/ffprobe build/
```


## Running Tests

```bash
# Go unit tests
go test ./internal/... -race

# C fio unit tests
cd fio && make test

# Integration tests (requires patched ffmpeg)
bash tests/integration/test-client-server.sh

# All integration tests
for t in tests/integration/test-*.sh; do bash "$t"; done

# Static analysis (scan for raw syscalls in patched ffmpeg)
python3 tests/static-analysis/scan-raw-syscalls.py
```

## How It Works (in detail)

The patched ffmpeg replaces 10 POSIX file operations (open, read, write, close, lseek, fstat, ftruncate, unlink, rename, mkdir) with `fio_*` equivalents. When ffmpeg runs on the server, every file operation is tunneled back to the client over the TCP connection. The client performs the actual I/O on its local filesystem and sends results back.


## Project Structure

- `cmd/client/` — client binary (drop-in ffmpeg replacement)
- `cmd/server/` — server binary (launches patched ffmpeg)
- `internal/` — shared Go packages (protocol, session, filehandler, config)
- `fio/` — C tunneling layer patched into ffmpeg (GPL v3)
- `patches/` — patches applied to jellyfin-ffmpeg source (GPL v3)
- `third_party/jellyfin-ffmpeg/` — jellyfin-ffmpeg submodule
- `scripts/` — build scripts
- `tests/` — integration tests and static analysis

## License

Split license — see [LICENSE.md](LICENSE.md). Code in `fio/` and `patches/` is GPL v3. Everything else is MIT.
