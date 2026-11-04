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

The patched ffmpeg replaces 10 POSIX file operations (open, read, write, close, lseek, fstat, ftruncate, unlink, rename, mkdir) with `fio_*` equivalents, plus three path queries: `fio_stat`, `fio_check` (in place of `access`) and `fio_opendir`/`fio_readdir`/`fio_closedir`. When ffmpeg runs on the server, every file operation is tunneled back to the client over the TCP connection. The client performs the actual I/O on its local filesystem and sends results back.

Code that would open files itself is patched to go through avio, and so through the tunnel: the `subtitles` and `ass` filters read the subtitle file and the fonts in `fontsdir` through avio instead of letting libass open them, and two-pass encoding writes its pass log through avio.

The server starts each ffmpeg with two environment variables: `FFOIP_PORT`, a loopback port the server listens on for that ffmpeg alone, and `FFOIP_TOKEN`, a random token. fio connects and sends `FIO1` followed by the token before anything else; the server accepts the first connection that sends the right token within 5 seconds and closes the others. If either variable is malformed or fio can't connect, every fio call on a tunneled path fails with EIO; fio never falls back to the server's own files. fio's virtual file descriptors start at `0x40000000`, and any other descriptor (`pipe:`, `fd:`) goes to the real syscall.

A client connection starts with a handshake (`internal/handshake`): the client sends `MsgHello` (protocol version and a nonce), the server answers `MsgChallenge` (its own nonce), the client sends `MsgCommand` signed over both nonces, and the server, once the signature checks out, answers `MsgAccepted` with an HMAC proof that it knows the secret. The client checks that proof before acting on any file request. The server sends nothing derived from the secret before the client has proved itself, so connecting to the server gives nobody anything to test guesses of the secret against. (Something posing as the server does receive the client's signature, which is why the secret should be long and random.) Before the handshake completes, messages are limited to 1 MiB and buffered only as they arrive, all unauthenticated connections together may buffer at most 64 MiB (16 MiB from one IP address or IPv6 /64), and each handshake must finish within 10 seconds. Stdin is flow-controlled: the client keeps at most `protocol.StdinWindow` bytes unacknowledged, and the server acknowledges with `MsgStdinAck` once it has written them to ffmpeg.

The client and server must run the same release. The server checks the protocol version in the hello and rejects a client with a different one; bump `protocol.CurrentVersion` whenever the wire protocol changes.


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
