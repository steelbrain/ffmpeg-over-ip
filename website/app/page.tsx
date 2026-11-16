import Link from "next/link"
import { Command } from "./components/command"
import { RELEASES_URL } from "./components/links"
import { SessionPlayer } from "./components/session/player"
import { bytes, eventTime, recording, totals } from "./components/session/recording"

const size = (n: number) =>
  n >= 1024 * 1024 ? `${(n / 1024 / 1024).toLocaleString("en-US")} MiB` : `${(n / 1024).toLocaleString("en-US")} KiB`

function facts() {
  const { events } = recording
  const all = totals(events)
  const reads = events.filter((e) => e.type === "READ").map((e) => e.n ?? 0)
  const input = events.find((e) => e.type === "OPEN")
  const segments = events.filter((e) => e.type === "OPEN" && e.path?.endsWith(".ts")).length
  return {
    ...all,
    accepted: eventTime("ACCEPTED"),
    firstOpen: eventTime("OPEN"),
    exitAt: eventTime("EXIT_CODE"),
    firstRead: reads[0] ?? 0,
    maxRead: Math.max(...reads),
    inputSize: input?.reply?.size ?? 0,
    segments,
  }
}

const h2 = "mt-20 text-2xl font-semibold tracking-tight"
const h3 = "text-lg font-semibold"
const text = "max-w-[42rem]"
const code = "prose-code"

export default function Home() {
  const f = facts()

  return (
    <div className="mx-auto max-w-6xl px-4 sm:px-6">
      <section className="pt-12 sm:pt-16">
        <h1 className="text-[2rem] font-semibold leading-tight tracking-tight sm:text-[2.5rem]">ffmpeg-over-ip</h1>
        <p className={`${text} mt-4 text-lg leading-relaxed`}>
          ffmpeg-over-ip lets a program that runs ffmpeg, such as Jellyfin, use a GPU on another machine. You put{" "}
          <code className={code}>ffmpeg-over-ip-client</code> where the program expects the ffmpeg binary. It sends the
          command line to <code className={code}>ffmpeg-over-ip-server</code>, which runs its own patched ffmpeg on the
          GPU machine. That ffmpeg reads and writes the client&apos;s files through the same TCP connection, so the
          client needs neither GPU passthrough nor a shared filesystem.
        </p>
      </section>

      <section aria-labelledby="install">
        <h2 id="install" className={h2}>
          Install
        </h2>
        <p className={`${text} mt-3`}>
          Each script downloads the latest release into the current directory, asks a few questions and writes a starter
          config file there. If the binary is already there, the download is skipped; set{" "}
          <code className={code}>FOIP_FORCE=1</code> to download again. An existing config file is left alone.
        </p>
        <div className="mt-8 grid gap-x-10 gap-y-12 lg:grid-cols-2">
          <div className="min-w-0">
            <h3 className={h3}>On the machine with the GPU</h3>
            <Command label="macOS or Linux" command="curl -fsSL https://ffmpeg-over-ip.com/install-server.sh | sh" />
            <Command label="Windows PowerShell" command="irm https://ffmpeg-over-ip.com/install-server.ps1 | iex" />
            <p className="mt-4">
              This installs <code className={code}>ffmpeg-over-ip-server</code> with its own{" "}
              <code className={code}>ffmpeg</code> and <code className={code}>ffprobe</code>, which must stay in the
              same directory. It asks for the listen IP (default 0.0.0.0), the port (default 5050) and an auth secret,
              then writes <code className={code}>ffmpeg-over-ip.server.jsonc</code>. Start the server with{" "}
              <code className={code}>./ffmpeg-over-ip-server</code> and make sure the client can reach the port.
            </p>
          </div>
          <div className="min-w-0">
            <h3 className={h3}>On the machine that runs your media server</h3>
            <Command label="macOS or Linux" command="curl -fsSL https://ffmpeg-over-ip.com/install-client.sh | sh" />
            <Command label="Windows PowerShell" command="irm https://ffmpeg-over-ip.com/install-client.ps1 | iex" />
            <p className="mt-4">
              This installs <code className={code}>ffmpeg-over-ip-client</code> and an{" "}
              <code className={code}>ffprobe</code> symlink to it (a copy on Windows). It asks for the server&apos;s
              host, port and auth secret, then writes <code className={code}>ffmpeg-over-ip.client.jsonc</code>.{" "}
              <code className={code}>./ffmpeg-over-ip-client -version</code> should print the server&apos;s ffmpeg
              version. Then set your media server&apos;s ffmpeg path to the client; in Jellyfin, that setting is under
              Dashboard &gt; Playback.
            </p>
          </div>
        </div>
        <p className={`${text} mt-8`}>
          Client and server must run the same release. To upgrade from an earlier version, see{" "}
          <Link href="/docs/upgrading">Upgrading</Link>. To install by hand or run either side in a container, see{" "}
          <Link href="/docs/quick-start">Quick start</Link> and <Link href="/docs/docker">Docker</Link>.
        </p>
      </section>

      <section aria-labelledby="how">
        <h2 id="how" className={h2}>
          How it works
        </h2>
        <p className={`${text} mt-3`}>
          The server runs on the machine with the GPU and listens on a TCP port. The client sits where your media server
          expects ffmpeg. Each time the media server starts ffmpeg, the client opens one connection to the server and
          this happens over it:
        </p>
        <ol className={`${text} mt-4 list-decimal space-y-3 pl-6`}>
          <li>
            Each side proves that it knows the shared secret, with an HMAC-SHA256 challenge-response over a fresh nonce
            from each side (the HELLO, CHALLENGE, COMMAND and ACCEPTED messages). The command line is signed as part of
            this, so a recorded command can&apos;t be replayed on a new connection.
          </li>
          <li>
            The server applies the rewrites from its config to the arguments and starts its bundled ffmpeg with them.
            Each connection gets its own ffmpeg process, so several clients can use one server at the same time.
          </li>
          <li>
            Each file operation of that ffmpeg (OPEN, READ, WRITE, SEEK, CLOSE and a few more) is sent to the client,
            which carries it out on its own disk. The patched ffmpeg reads ahead and caches what it has read, so it
            needs far fewer round trips than ffmpeg&apos;s own small reads would.
          </li>
          <li>
            ffmpeg&apos;s stdout and stderr are forwarded to the client while it runs. When ffmpeg exits, the server
            sends its exit code and the client exits with the same code.
          </li>
        </ol>
      </section>

      <section aria-labelledby="session">
        <h2 id="session" className={h2}>
          A recorded session
        </h2>
        <p className={`${text} mt-3`}>
          This is a real transcode, recorded on {recording.recordedOn} with version {recording.version}. The client ran
          in a Linux container that could see the input file but not a GPU. The server ran on the macOS host and encoded
          with <code className={code}>h264_videotoolbox</code>. The left pane shows what the client printed, the right
          pane shows the server&apos;s log, and the list below them shows every message between the two. Replay it at
          one tenth of the recorded speed, or drag the slider.
        </p>
        <div className="mt-6">
          <SessionPlayer />
        </div>
        <p className={`${text} mt-6`}>
          In this recording, the handshake was done {f.accepted.toFixed(1)} ms after the client connected. ffmpeg opened
          the input {(f.firstOpen - f.accepted).toFixed(1)} ms after the command was accepted, read all{" "}
          {bytes.format(f.inputSize)} bytes of it in {f.reads} READ requests, and wrote {f.segments} HLS segments and a
          playlist in {f.writes} WRITE requests. The READ sizes grow from {size(f.firstRead)} to {size(f.maxRead)}, the
          default read-ahead limit for files under 1 GiB. The client exited with ffmpeg&apos;s exit code, 0,{" "}
          {f.exitAt.toLocaleString("en-US", { maximumFractionDigits: 1 })} ms after connecting.
        </p>
        <p className={`${text} mt-4 text-sm text-muted`}>
          How this was recorded: the client was built for linux/arm64 from the v{recording.version} tag and mounted as{" "}
          <code className={code}>/usr/bin/ffmpeg</code> in an Alpine container on OrbStack. The input and output
          directories were on the host and mounted into the container, so the server&apos;s ffmpeg could reach them only
          through the client. The server and its ffmpeg came from the macos-arm64 release package. The input was a
          30-second 1080p test pattern, H.264 and AAC in Matroska. A logging proxy inside the container, between client
          and server, recorded the messages. OrbStack forwards the container&apos;s connections to the host&apos;s
          loopback address, which is why the server logged 127.0.0.1. Each server log line is shown at the message that
          led to it. The client pane applies carriage returns the way a terminal too wide to wrap lines does, which is
          why the ends of overwritten progress lines stay visible.
        </p>
      </section>

      <section aria-labelledby="configuration">
        <h2 id="configuration" className={h2}>
          Configuration
        </h2>
        <p className={`${text} mt-3`}>
          Each side reads a JSONC file (JSON that allows comments and trailing commas) or environment variables. The
          smallest working setup is an address and a secret that matches on both sides.
        </p>
        <div className="mt-6 grid gap-6 lg:grid-cols-2">
          <ConfigFile
            name="ffmpeg-over-ip.server.jsonc"
            body={`{\n  "address": "0.0.0.0:5050",\n  "authSecret": "pick-a-strong-secret"\n}`}
          />
          <ConfigFile
            name="ffmpeg-over-ip.client.jsonc"
            body={`{\n  "address": "192.168.1.100:5050",  // your server's IP\n  "authSecret": "pick-a-strong-secret"\n}`}
          />
        </div>
        <dl className={`${text} mt-8 space-y-4`}>
          <Option name="rewrites" side="server">
            Replaces whole arguments before ffmpeg starts, for example <code className={code}>h264_nvenc</code> with{" "}
            <code className={code}>h264_qsv</code> when the server has an Intel GPU instead of an NVIDIA one.
          </Option>
          <Option name="fallbackToLocal" side="client">
            Runs the client machine&apos;s own ffmpeg when the server can&apos;t be reached. Only a failed connection
            triggers it; a transcode that has started is never restarted locally.
          </Option>
          <Option name="log, debug" side="both">
            Send logs to stdout, stderr or a file. With <code className={code}>debug</code>, the server logs each
            command&apos;s original and rewritten arguments.
          </Option>
          <Option name="unix:/path" side="address">
            Connects over a Unix socket instead of TCP, for example when the server runs on a host and the client in one
            of its containers.
          </Option>
          <Option name="FFMPEG_OVER_IP_CLIENT_ADDRESS" side="environment">
            With <code className={code}>FFMPEG_OVER_IP_CLIENT_AUTH_SECRET</code> (or the{" "}
            <code className={code}>SERVER</code> pair), replaces the config file, which suits Docker. Rewrites still
            need a file.
          </Option>
          <Option name="ffprobe" side="file name">
            A symlink or copy of the client whose name contains &quot;ffprobe&quot; runs ffprobe on the server instead
            of ffmpeg.
          </Option>
        </dl>
        <p className={`${text} mt-6`}>
          The <Link href="/docs/configuration">configuration reference</Link> covers every option, the search paths for
          config files and the read-ahead settings.
        </p>
      </section>

      <section aria-labelledby="security">
        <h2 id="security" className={h2}>
          Security
        </h2>
        <ul className={`${text} mt-4 list-disc space-y-3 pl-6`}>
          <li>
            Client and server each prove that they know the shared secret before a command runs, and the client acts on
            file requests only after the server&apos;s proof. The secret itself is never sent.
          </li>
          <li>
            Nothing is encrypted. Someone who can intercept the connection can read and change command lines and file
            contents. Across a network you don&apos;t trust, run it through a VPN or an SSH tunnel.
          </li>
          <li>
            Use a long random secret, such as the output of <code className={code}>openssl rand -hex 32</code>. Someone
            who records a session can test guesses of the secret against it offline.
          </li>
          <li>
            Anyone with the secret can run ffmpeg on the server with any arguments, and can pose as the server to read,
            write or delete any file the client&apos;s user can reach.
          </li>
          <li>Only the server listens on a port. The client only makes outbound connections.</li>
          <li>
            A few ffmpeg options open files on the server rather than the client, such as{" "}
            <code className={code}>-vstats_file</code> and the two-pass stats files of libx264.{" "}
            <Link href="/docs/troubleshooting#files-opened-on-the-server">Troubleshooting</Link> lists them.
          </li>
        </ul>
      </section>

      <section aria-labelledby="platforms">
        <h2 id="platforms" className={h2}>
          Platforms
        </h2>
        <p className={`${text} mt-3`}>
          Each <a href={RELEASES_URL}>release</a> has a client and a server package for Linux (x86_64 and arm64), macOS
          (arm64 and x86_64) and Windows (x86_64). The server package includes ffmpeg and ffprobe built with
          jellyfin-ffmpeg&apos;s build scripts, with hardware acceleration such as NVENC, QSV, VAAPI, AMF and
          VideoToolbox, depending on the platform. The bundled ffmpeg needs glibc 2.28 or newer on Linux and macOS 12.0
          or newer.
        </p>
      </section>
    </div>
  )
}

function ConfigFile({ name, body }: { name: string; body: string }) {
  return (
    <div className="min-w-0">
      <div className="rounded-t-md border border-b-0 border-line bg-panel px-3 py-1.5 font-mono text-[0.8125rem] text-muted">
        {name}
      </div>
      <pre className="overflow-x-auto rounded-b-md border border-line px-3 py-2.5 text-[0.8125rem] leading-relaxed">
        <code>{body}</code>
      </pre>
    </div>
  )
}

function Option({ name, side, children }: { name: string; side: string; children: React.ReactNode }) {
  return (
    <div>
      <dt>
        <code className="font-mono text-[0.9375rem] font-semibold">{name}</code>{" "}
        <span className="text-sm text-muted">({side})</span>
      </dt>
      <dd className="mt-0.5">{children}</dd>
    </div>
  )
}
