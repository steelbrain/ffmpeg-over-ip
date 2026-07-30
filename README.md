# [ffmpeg-over-ip](https://ffmpeg-over-ip.com)

Use GPU-accelerated ffmpeg from anywhere — a Docker container, a VM, or a remote machine — without GPU passthrough or shared filesystems.

## Key Features

- **Network-Transparent File I/O**: Access media files on remote clients via standard TCP loopback tunneling (`fio`).
- **Shared Storage Short-Circuiting**: Automatically bypasses network tunneling when both client and server share a storage mount (e.g. `/media`, NFS, Ceph, SeaweedFS), opening files directly off local disk.
- **Multi-Node Load Balancing & Fast Failover**: Define multiple transcode servers (`node1:5050, node2:5050`). The client automatically load-balances and fails over sequentially if a node goes down.
- **Docker & Kubernetes Ready**: Containerized server/client images, Docker Compose setups, and drop-in Kubernetes manifests for Jellyfin and transcode DaemonSets.

---

## Architecture

```
CLIENT (Jellyfin / App)                 SERVER (GPU Node)
=======================                 =================

Media server invokes "ffmpeg"         Daemon listening on :5050
        |                                      |
  ffmpeg-over-ip-client               ffmpeg-over-ip-server
        |                                      |
        +--------- TCP connection ------------>+
        |                                      |
  Local / Shared Storage                 patched ffmpeg
  (/media/movie.mkv)               (Short-circuits shared /media reads
                                    or tunnels network I/O)
```

---

## Quick Start

### 1. Docker Compose Integration
Test locally or in container environments using Docker Compose:

```bash
docker compose up
```

### 2. Systemd Installation (Linux Server Nodes)
Install `ffmpeg-over-ip-server` as a system service:

```bash
# Copy binary and systemd service file
sudo cp build/ffmpeg-over-ip-server /usr/local/bin/
sudo cp systemd/ffmpeg-over-ip-server.service /etc/systemd/system/

# Create configuration file
sudo mkdir -p /etc/ffmpeg-over-ip
sudo bash -c 'cat <<EOF > /etc/ffmpeg-over-ip/server.jsonc
{
  "address": "0.0.0.0:5050",
  "authSecret": "cluster_secret_9988",
  "shortCircuitRead": [
    "/media"
  ]
}
EOF'

# Enable & start service
sudo systemctl daemon-reload
sudo systemctl enable --now ffmpeg-over-ip-server
```

### 3. Kubernetes Deployment (Jellyfin & Server DaemonSet)
Deploy transcode daemons to GPU nodes and configure Jellyfin to transcode remotely:

```bash
kubectl apply -f k8s/jellyfin-ffmpeg-over-ip.yaml
```

---

## Configuration Reference

### Client Environment Variables
| Variable | Example | Description |
|---|---|---|
| `FFMPEG_OVER_IP_CLIENT_ADDRESS` | `mini:5050, pico:5050` | Comma-separated list of server addresses for randomized load balancing & failover |
| `FFMPEG_OVER_IP_CLIENT_AUTH_SECRET` | `cluster_secret` | Shared HMAC secret matching the server daemons |
| `FFMPEG_OVER_IP_CLIENT_DIAL_TIMEOUT` | `3s` | Timeout per dial attempt before failing over |
| `FFMPEG_OVER_IP_CLIENT_FALLBACK_TO_LOCAL` | `true` | Fallback to local `ffmpeg` binary if all remote servers are down |

### Server Environment Variables
| Variable | Example | Description |
|---|---|---|
| `FFMPEG_OVER_IP_SERVER_ADDRESS` | `0.0.0.0:5050` | Bind address and port for transcode daemon |
| `FFMPEG_OVER_IP_SERVER_AUTH_SECRET` | `cluster_secret` | Shared HMAC secret for client authentication |
| `FFMPEG_OVER_IP_SERVER_SHORT_CIRCUIT_READ` | `/media,/mnt/storage` | Directories to bypass network tunneling and open directly off shared storage |

---

## Building from Source

```bash
# Build server & client binaries for Linux AMD64
go build -o build/ffmpeg-over-ip-server ./cmd/server
go build -o build/ffmpeg-over-ip-client ./cmd/client

# Run C FIO unit test suite
make -C fio test
make -C fio wire-test

# Run Go unit test suite
go test ./...
```

---

## License

Split license — see [LICENSE.md](LICENSE.md). The fio layer and ffmpeg patches (`fio/`, `patches/`) are GPL v3 (derived from ffmpeg). Everything else is MIT.
