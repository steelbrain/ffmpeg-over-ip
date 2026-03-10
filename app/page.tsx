import Link from "next/link"

const GITHUB_URL = "https://github.com/steelbrain/ffmpeg-over-ip"
const RELEASE_URL = "https://github.com/steelbrain/ffmpeg-over-ip/releases/latest"

export default function Home() {
  return (
    <div>
      {/* Hero */}
      <section className="relative overflow-hidden px-6 py-16 sm:py-20">
        <div className="absolute inset-0 bg-gradient-to-b from-blue-950/20 to-transparent" />
        <div className="relative mx-auto max-w-4xl text-center">
          <h1 className="text-5xl font-bold tracking-tight sm:text-7xl">
            <span className="bg-gradient-to-r from-blue-400 to-emerald-400 bg-clip-text text-transparent">
              ffmpeg-over-ip
            </span>
          </h1>
          <p className="mt-6 text-lg leading-8 text-gray-300 sm:text-xl">
            Use GPU-accelerated ffmpeg from anywhere — a Docker container, a VM, or a remote machine — without GPU
            passthrough or shared filesystems.
          </p>
          <div className="mt-10 flex flex-wrap items-center justify-center gap-4">
            <a
              href={RELEASE_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="rounded-full bg-blue-600 px-6 py-3 text-sm font-semibold text-white shadow-lg hover:bg-blue-500 transition-colors"
            >
              Download Latest Release
            </a>
            <Link
              href="/docs/quick-start"
              className="rounded-full border border-white/20 px-6 py-3 text-sm font-semibold text-white hover:bg-white/10 transition-colors"
            >
              Documentation
            </Link>
            <a
              href={GITHUB_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="rounded-full border border-white/20 px-6 py-3 text-sm font-semibold text-white hover:bg-white/10 transition-colors"
            >
              GitHub
            </a>
          </div>
        </div>
      </section>

      {/* Problem */}
      <section className="px-6 py-20">
        <div className="mx-auto max-w-6xl">
          <h2 className="text-center text-3xl font-bold sm:text-4xl">The Problem</h2>
          <p className="mx-auto mt-4 max-w-2xl text-center text-gray-400">
            GPU transcoding is powerful, but getting access to the GPU is painful.
          </p>
          <div className="mt-12 grid gap-6 sm:grid-cols-3">
            <ProblemCard
              title="Docker Containers"
              description="Need --runtime=nvidia, device mounts, and driver version alignment between host and container."
            />
            <ProblemCard
              title="Virtual Machines"
              description="Need PCIe passthrough or SR-IOV — complex setup that locks the GPU to one VM."
            />
            <ProblemCard
              title="Remote Machines"
              description="Need shared filesystems (NFS/SMB) with all the path mapping, mount maintenance, and permission headaches."
            />
          </div>
        </div>
      </section>

      {/* Solution */}
      <section className="px-6 py-20">
        <div className="mx-auto max-w-6xl">
          <h2 className="text-center text-3xl font-bold sm:text-4xl">The Solution</h2>
          <p className="mx-auto mt-4 max-w-2xl text-center text-gray-400">
            Run the server on the GPU machine. Point your app at the client binary instead of ffmpeg. Done.
          </p>

          {/* Architecture diagram */}
          <div className="mt-12 flex flex-col items-center gap-8 sm:flex-row sm:justify-center sm:gap-4">
            <div className="w-full max-w-xs rounded-xl border border-white/10 bg-white/5 p-6">
              <div className="text-xs font-semibold uppercase tracking-wider text-blue-400">Client</div>
              <div className="mt-1 text-sm text-gray-400">has files, no GPU</div>
              <div className="mt-4 space-y-2 text-sm">
                <div className="rounded-lg bg-white/5 px-3 py-2 font-mono text-xs">ffmpeg-over-ip-client</div>
                <div className="rounded-lg bg-emerald-950/40 border border-emerald-500/20 px-3 py-2 text-gray-300">
                  Local filesystem (real files)
                </div>
              </div>
            </div>

            <div className="flex flex-col items-center gap-1 text-gray-500">
              <div className="hidden sm:block text-xs">TCP</div>
              <div className="text-2xl sm:rotate-0 rotate-90">&#8594;</div>
            </div>

            <div className="w-full max-w-xs rounded-xl border border-white/10 bg-white/5 p-6">
              <div className="text-xs font-semibold uppercase tracking-wider text-emerald-400">Server</div>
              <div className="mt-1 text-sm text-gray-400">has GPU</div>
              <div className="mt-4 space-y-2 text-sm">
                <div className="rounded-lg bg-white/5 px-3 py-2 font-mono text-xs">ffmpeg-over-ip-server</div>
                <div className="rounded-lg bg-blue-950/40 border border-blue-500/20 px-3 py-2 text-gray-300">
                  Patched ffmpeg (I/O tunneled back)
                </div>
              </div>
            </div>
          </div>

          <p className="mt-10 text-center text-lg font-medium text-gray-300">
            No GPU passthrough. No shared filesystem. No NFS. No SMB.{" "}
            <span className="text-white">Just one TCP port.</span>
          </p>
        </div>
      </section>

      {/* How it works */}
      <section className="px-6 py-20">
        <div className="mx-auto max-w-4xl">
          <h2 className="text-center text-3xl font-bold sm:text-4xl">How It Works</h2>
          <div className="mt-12 space-y-8">
            <Step
              number={1}
              title="Your app calls the client"
              description="Your media server invokes ffmpeg-over-ip-client with normal ffmpeg arguments."
            />
            <Step
              number={2}
              title="Client connects with authentication"
              description="The client connects to the server and sends the command, authenticated with HMAC-SHA256."
            />
            <Step
              number={3}
              title="Server runs patched ffmpeg"
              description="The server launches its patched ffmpeg, which tunnels all file reads and writes back to the client."
            />
            <Step
              number={4}
              title="Results stream back"
              description="stdout/stderr are forwarded in real-time. When ffmpeg exits, the client exits with the same code."
            />
          </div>
        </div>
      </section>

      {/* Features */}
      <section className="px-6 py-20">
        <div className="mx-auto max-w-6xl">
          <h2 className="text-center text-3xl font-bold sm:text-4xl">Features</h2>
          <div className="mt-12 grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
            <FeatureCard
              title="Pre-built Binaries"
              description="Releases include patched ffmpeg and ffprobe with broad hardware acceleration support. No need to install ffmpeg separately."
            />
            <FeatureCard
              title="Hardware Acceleration"
              description="Built on jellyfin-ffmpeg — supports NVENC, QSV, VAAPI, AMF, VideoToolbox, and more out of the box."
            />
            <FeatureCard
              title="HMAC Authentication"
              description="Every command is signed with HMAC-SHA256 using a shared secret. Only authorized clients can connect."
            />
            <FeatureCard
              title="Cross-Platform"
              description="Runs on Linux, macOS, and Windows. Both x86_64 and arm64 architectures supported."
            />
            <FeatureCard
              title="Multiple Clients"
              description="Multiple clients can connect simultaneously — each session gets its own ffmpeg process."
            />
            <FeatureCard
              title="Codec Rewrites"
              description="Server-side argument rewrites let you transparently map codecs (e.g., h264_nvenc to h264_qsv)."
            />
          </div>
        </div>
      </section>

      {/* Platform support */}
      <section className="px-6 py-20">
        <div className="mx-auto max-w-2xl">
          <h2 className="text-center text-3xl font-bold sm:text-4xl">Supported Platforms</h2>
          <div className="mt-12 overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-white/10 text-left">
                  <th className="pb-3 pr-8 font-semibold text-gray-300">Platform</th>
                  <th className="pb-3 pr-8 font-semibold text-gray-300">Client</th>
                  <th className="pb-3 font-semibold text-gray-300">Server + ffmpeg</th>
                </tr>
              </thead>
              <tbody className="text-gray-400">
                <PlatformRow platform="Linux x86_64" client server />
                <PlatformRow platform="Linux arm64" client server />
                <PlatformRow platform="macOS arm64" client server />
                <PlatformRow platform="macOS x86_64" client server />
                <PlatformRow platform="Windows x86_64" client server />
              </tbody>
            </table>
          </div>
        </div>
      </section>

      {/* CTA */}
      <section className="px-6 py-20">
        <div className="mx-auto max-w-2xl text-center">
          <h2 className="text-3xl font-bold sm:text-4xl">Get Started</h2>
          <p className="mt-4 text-gray-400">
            Up and running in a few minutes. Download the release, configure, and go.
          </p>
          <div className="mt-8 flex flex-wrap items-center justify-center gap-4">
            <a
              href={RELEASE_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="rounded-full bg-blue-600 px-6 py-3 text-sm font-semibold text-white shadow-lg hover:bg-blue-500 transition-colors"
            >
              Download Latest Release
            </a>
            <Link
              href="/docs/quick-start"
              className="rounded-full border border-white/20 px-6 py-3 text-sm font-semibold text-white hover:bg-white/10 transition-colors"
            >
              Read the Docs
            </Link>
          </div>
        </div>
      </section>
    </div>
  )
}

function ProblemCard({ title, description }: { title: string; description: string }) {
  return (
    <div className="rounded-xl border border-white/10 bg-white/5 p-6">
      <h3 className="font-semibold text-white">{title}</h3>
      <p className="mt-2 text-sm leading-6 text-gray-400">{description}</p>
    </div>
  )
}

function Step({ number, title, description }: { number: number; title: string; description: string }) {
  return (
    <div className="flex gap-4">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-blue-600 text-sm font-bold">
        {number}
      </div>
      <div>
        <h3 className="font-semibold text-white">{title}</h3>
        <p className="mt-1 text-sm text-gray-400">{description}</p>
      </div>
    </div>
  )
}

function FeatureCard({ title, description }: { title: string; description: string }) {
  return (
    <div className="rounded-xl border border-white/10 bg-white/5 p-6">
      <h3 className="font-semibold text-white">{title}</h3>
      <p className="mt-2 text-sm leading-6 text-gray-400">{description}</p>
    </div>
  )
}

function PlatformRow({ platform, client, server }: { platform: string; client: boolean; server: boolean }) {
  return (
    <tr className="border-b border-white/5">
      <td className="py-3 pr-8 font-medium text-white">{platform}</td>
      <td className="py-3 pr-8">{client ? <Check /> : <Dash />}</td>
      <td className="py-3">{server ? <Check /> : <Dash />}</td>
    </tr>
  )
}

function Check() {
  return <span className="text-emerald-400">&#10003;</span>
}

function Dash() {
  return <span className="text-gray-600">—</span>
}
