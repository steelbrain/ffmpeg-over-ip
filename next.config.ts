import type { NextConfig } from "next"

const RAW_MAIN = "https://raw.githubusercontent.com/steelbrain/ffmpeg-over-ip/main/scripts"

const nextConfig: NextConfig = {
  async redirects() {
    return [
      {
        source: "/install-client.sh",
        destination: `${RAW_MAIN}/install-client.sh`,
        permanent: false,
      },
      {
        source: "/install-server.sh",
        destination: `${RAW_MAIN}/install-server.sh`,
        permanent: false,
      },
      {
        source: "/install-client.ps1",
        destination: `${RAW_MAIN}/install-client.ps1`,
        permanent: false,
      },
      {
        source: "/install-server.ps1",
        destination: `${RAW_MAIN}/install-server.ps1`,
        permanent: false,
      },
    ]
  },
}

export default nextConfig
