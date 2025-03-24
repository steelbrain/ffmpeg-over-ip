# ffmpeg-over-ip

[ffmpeg-over-ip][] is a client-server combo that allows you to transcode videos on a machine with access to a GPU from a container or a VM without having to passthrough a GPU. This means you can run GPU accelerated ffmpeg from a docker container and use the GPU from the hypervisor **or your Gaming PC running Windows**.

If you're looking for **v3** (pre Golang rewrite), please check out [v3 branch](https://github.com/steelbrain/ffmpeg-over-ip/tree/v3)

## How it works

ffmpeg-over-ip uses a shared filesystem (this could be a Docker mount, NFS or SMB) to transfer video data and a lightweight communication protocol to coordinate the commands. This means that you do not need an ssh server running on the GPU-host (making Windows support easier/simpler), and that the client can stream the output(s) as they are processed. This allows ffmpeg-over-ip to be used in [Plex][] or [Emby][] media servers for just-in-time transcoding.

## Key Features

- **Authentication**: Client & Server communicate through signed messages
- **Flexible Connectivity**: Supports both TCP and Unix socket connections
- **Real-time Streaming**: Output(s) are streamed in real-time to the client through filesystem
- **Robust Cancellation**: Client can cancel running processes, and server properly cleans up resources
- **Path and Command Rewrites**: Rewrite paths/codecs to handle differences between client and server

## Installation

### Option 1: Download Pre-built Binaries (Recommended)

Download the latest binaries for your platform from the [GitHub Releases](https://github.com/steelbrain/ffmpeg-over-ip/releases) page.

1. Navigate to the releases page and download the appropriate binaries for your platform
2. Extract the archive to get the `ffmpeg-over-ip-client` and `ffmpeg-over-ip-server` executables
3. Make the binaries executable if needed: `chmod +x ffmpeg-over-ip-*`

### Option 2: Build from Source

If you prefer to build from source:

1. Make sure you have Go 1.18+ installed
2. Clone the repository: `git clone https://github.com/steelbrain/ffmpeg-over-ip.git`
3. Use the included Makefile:

```bash
# Build both client and server binaries
make build

# Install to your Go path (optional)
make install
```

The built binaries will be placed in the `bin` directory as `ffmpeg-over-ip-client` and `ffmpeg-over-ip-server`.

## Configuration

Both the client and server are configured using JSONC (JSON with comments) configuration files. Example configuration files for both the server and client are included in the repository as `template.ffmpeg-over-ip-client.jsonc` and `template.ffmpeg-over-ip-server.jsonc`.

### Client Configuration

Create a client configuration file in one of the default search paths, or specify it with `--config`:

```jsonc
{
  // Where logs should go. Can be "stdout", "stderr", a file path, or false/null to disable logging
  "log": "/tmp/ffmpeg-over-ip-client.log",

  // Server address: "hostname:port" for TCP or "/path/to/socket" for Unix socket
  "address": "localhost:5050",

  // Authentication secret (must match server config)
  "authSecret": "your-secret-here"
}
```

### Server Configuration

Create a server configuration file similarly:

```jsonc
{
  // Where logs should go. Can be "stdout", "stderr", a file path, or false/null to disable logging
  "log": "stdout",

  // Listen address: "hostname:port" for TCP or "/path/to/socket" for Unix socket
  "address": "0.0.0.0:5050",

  // Authentication secret (must match client config)
  "authSecret": "your-secret-here",

  // Path to ffmpeg binary on the server
  "ffmpegPath": "/usr/bin/ffmpeg",

  // Path rewrites to map client paths to server paths
  "rewrites": [
    // File path rewrites - maps client paths to server paths
    ["/client/path", "/server/path"],
    ["/another/client/path", "/corresponding/server/path"],

    // Codec rewrites - allow transparent use of hardware-accelerated codecs
    ["h264_nvenc", "h264_qsv"],      // Use Intel QuickSync instead of NVIDIA for h264
    ["hevc_nvenc", "hevc_vaapi"],    // Use VAAPI instead of NVIDIA for HEVC
    ["cuda", "qsv"]                  // Use QuickSync acceleration instead of CUDA
  ]
}
```

### Configuration lookup

For Docker use cases, **please skip this part** and go to Docker Integration below, otherwise keep reading.

To see where the application looks for configuration files:

```bash
# For the client
./bin/ffmpeg-over-ip-client --debug-print-search-paths

# For the server
./bin/ffmpeg-over-ip-server --debug-print-search-paths
```

In addition to configuration files, you can also specify the configuration file path via environment variables.
This is useful if you'd like to be able to switch between multiple configurations within the same install.
Here's the recognized environment variables:

```bash
# For the client
export FFMPEG_OVER_IP_CLIENT_CONFIG="/path/to/custom-client-config.jsonc"

# For the server
export FFMPEG_OVER_IP_SERVER_CONFIG="/path/to/custom-server-config.jsonc"
```

## Usage

### Starting the Server

```bash
# Uses default configuration paths
./bin/ffmpeg-over-ip-server

# Uses the specified configuration path
./bin/ffmpeg-over-ip-server --config /path/to/your/server-config.jsonc

# Uses configuration path from environment variable
FFMPEG_OVER_IP_SERVER_CONFIG="/path/to/your/server-config.jsonc" ./bin/ffmpeg-over-ip-server
```

### Using the Client

Use the client exactly as you would use ffmpeg, with the same arguments:

```bash
# Uses default configuration paths
./bin/ffmpeg-over-ip-client -i input.mp4 -c:v libx264 -preset medium output.mp4

# Uses the specified configuration path
./bin/ffmpeg-over-ip-client --config /path/to/your/client-config.jsonc -i input.mp4 output.mp4

# Uses configuration path from environment variable
FFMPEG_OVER_IP_CLIENT_CONFIG="/path/to/your/client-config.jsonc" ./bin/ffmpeg-over-ip-client -i input.mp4 output.mp4
```

## Docker Integration

You can use ffmpeg-over-ip in Docker environments by mounting the binary and configuration as volumes.
Using ffmpeg-over-ip with containers allows containers to use ffmpeg remotely without needing GPU
passthrough or other special setup.

For client, you can do:

```bash
docker run -v ./path/to/ffmpeg-over-ip-client:/usr/bin/ffmpeg \
           -v ./path/to/config:/etc/ffmpeg-over-ip.client.jsonc \
           your-image
```

For server, you can do:

```bash
docker run -v ./path/to/ffmpeg-over-ip-server:/usr/bin/ffmpeg \
           -v ./path/to/config:/etc/ffmpeg-over-ip.server.jsonc \
           your-image
```

### Recommendations for Docker

- If you are running the server on the same machine as the client, it may be a good idea to use a unix socket for the `address`
  and then forward that over a volume, otherwise you'll have to add an extra host like `host.docker.internal:host-gateway` in your
  Docker config.
- If you want to run the server in a Docker container as well, it may be a good idea to use a GPU accelerated docker runtime
  like `nvidia-container-runtime`, which can be used by adding `--runtime=nvidia` to the docker run command.

### Debugging with Docker

For a working docker setup, you may need to configure rewrites for the paths between the client and server, as well
as rewrites for the codecs. To see what ffmpeg commands are being executed and what they are being rewritten to, please
enable the `log` option in both client and server, you can set the server log to `stdout` for easier debugging, and for the client
you may set it to `/tmp/ffmpeg-over-ip.client.log` and then tail it from the docker container.

Additionally, you can turn on the `debug` flag in the server configuration to see the output of the commands being executed, to catch
any issues/bugs as they occur.

## Development

### Project Structure

- `cmd/client`: Client implementation
- `cmd/server`: Server implementation
- `pkg/common`: Shared utilities
- `pkg/config`: Configuration handling
- `pkg/protocol`: Network protocol implementation

### Environment Variables

For security reasons, only a limited set of environment variables are allowed for expansion in log paths:
- HOME, TMPDIR, TMP, TEMP, USER, LOGDIR, PWD, XDG_*

## License

The contents of this project are licensed under the terms of the MIT License.

[ffmpeg-over-ip]: https://ffmpeg-over-ip.com
[Plex]: https://www.plex.tv/
[Emby]: https://www.emby.media/
