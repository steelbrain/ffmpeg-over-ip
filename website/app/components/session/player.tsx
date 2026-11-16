"use client"

import { useEffect, useRef, useState } from "react"
import styles from "./player.module.css"
import { bytes, recording, type SessionEvent, totals } from "./recording"

/** Playback runs at one tenth of the recorded speed. */
const SPEED = 0.1

const { command, duration, events, serverLog } = recording

/**
 * Render stderr the way a terminal does: "\r" moves the cursor back to the
 * start of the line and later characters overwrite it; "\n" starts a new line.
 * The terminal is treated as wide enough that no line wraps.
 */
function terminal(chunks: string[]): string {
  const lines: string[][] = [[]]
  let col = 0
  for (const chunk of chunks) {
    for (const ch of chunk) {
      if (ch === "\n") {
        lines.push([])
        col = 0
      } else if (ch === "\r") {
        col = 0
      } else {
        const line = lines[lines.length - 1] as string[]
        line[col] = ch
        col++
      }
    }
  }
  return lines.map((l) => l.join("")).join("\n")
}

function openMode(flags: number): string {
  const access = flags & 0x3
  const parts = [access === 0 ? "read" : access === 1 ? "write" : "read/write"]
  if (flags & 0x40) parts.push("create")
  if (flags & 0x200) parts.push("truncate")
  return parts.join(", ")
}

function detail(e: SessionEvent): string {
  switch (e.type) {
    case "HELLO":
      return `protocol 0x${(e.version ?? 0).toString(16).padStart(2, "0")}, client nonce`
    case "CHALLENGE":
      return "server nonce"
    case "COMMAND":
      return `ffmpeg with ${e.args?.length ?? 0} arguments, signed over both nonces`
    case "ACCEPTED":
      return "server's proof that it knows the secret"
    case "STDIN_CLOSE":
      return "stdin reached end of file"
    case "OPEN":
      return `${e.path} (${openMode(e.flags ?? 0)})`
    case "SEEK":
      return `file ${e.fid} to byte ${bytes.format(e.offset ?? 0)}`
    case "READ":
      return `file ${e.fid}, up to ${bytes.format(e.n ?? 0)} bytes`
    case "WRITE":
      return `file ${e.fid}, ${bytes.format(e.n ?? 0)} bytes`
    case "CLOSE":
      return `file ${e.fid}`
    case "STDERR":
      return (e.text ?? "").split(/[\r\n]/).find((l) => l.trim() !== "") ?? ""
    case "EXIT_CODE":
      return `ffmpeg exited with code ${e.code}`
    default:
      return ""
  }
}

function replyText(e: SessionEvent, time: number): string {
  const r = e.reply
  if (!r) return ""
  if (r.t > time) return "waiting for the client"
  const ms = `${(r.t - e.t).toFixed(1)} ms`
  switch (r.type) {
    case "OPEN_OK":
      return `opened, ${bytes.format(r.size ?? 0)} bytes, ${ms}`
    case "READ_OK":
      return `${bytes.format(r.n ?? 0)} bytes, ${ms}`
    case "WRITE_OK":
      return `${bytes.format(r.n ?? 0)} written, ${ms}`
    case "SEEK_OK":
      return `at byte ${bytes.format(r.offset ?? 0)}, ${ms}`
    case "CLOSE_OK":
      return `closed, ${ms}`
    case "IO_ERROR":
      return `error ${r.errno}, ${ms}`
    default:
      return `${r.type}, ${ms}`
  }
}

export function SessionPlayer() {
  const [time, setTime] = useState(duration)
  const [playing, setPlaying] = useState(false)
  const [follow, setFollow] = useState(false)
  const clientRef = useRef<HTMLPreElement>(null)
  const serverRef = useRef<HTMLPreElement>(null)
  const listRef = useRef<HTMLOListElement>(null)

  useEffect(() => {
    if (!playing) return
    let frame = 0
    let last = performance.now()
    const tick = (now: number) => {
      const dt = (now - last) * SPEED
      last = now
      setTime((t) => {
        const next = Math.min(duration, t + dt)
        if (next >= duration) setPlaying(false)
        return next
      })
      frame = requestAnimationFrame(tick)
    }
    frame = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(frame)
  }, [playing])

  const visible = events.filter((e) => e.t <= time)
  const current = time < duration ? visible.length - 1 : -1
  const stderr = visible.filter((e) => e.type === "STDERR").map((e) => e.text ?? "")
  const exit = visible.find((e) => e.type === "EXIT_CODE")
  const log = serverLog.filter((l) => l.t === null || l.t <= time)
  const sums = totals(events, time)

  // While playing or scrubbing, keep the newest output in view.
  // biome-ignore lint/correctness/useExhaustiveDependencies: scroll whenever the visible content changes
  useEffect(() => {
    if (!follow) return
    for (const el of [clientRef.current, serverRef.current, listRef.current]) {
      if (el) el.scrollTop = el.scrollHeight
    }
  }, [follow, visible.length, log.length])

  function toggle() {
    setFollow(true)
    if (playing) {
      setPlaying(false)
      return
    }
    if (time >= duration) setTime(0)
    setPlaying(true)
  }

  const label = playing ? "Pause" : time >= duration ? "Replay at 1/10 speed" : "Resume at 1/10 speed"

  return (
    <div className={styles.player}>
      <div className={styles.controls}>
        <button type="button" className={styles.button} onClick={toggle}>
          {label}
        </button>
        <input
          className={styles.scrubber}
          type="range"
          min={0}
          max={Math.ceil(duration)}
          step={1}
          value={time}
          aria-label="Time in the recorded session"
          aria-valuetext={`${time.toFixed(0)} milliseconds`}
          onChange={(ev) => {
            setFollow(true)
            setPlaying(false)
            setTime(Math.min(duration, Number(ev.target.value)))
          }}
        />
        <span className={styles.clock}>
          {time.toFixed(1).padStart(6, " ")} / {duration.toFixed(1)} ms
        </span>
      </div>

      <div className={styles.panes}>
        <section className={styles.pane} aria-label="Client terminal">
          <div className={styles.paneHead}>
            <strong>Client</strong>Linux container without GPU access; /usr/bin/ffmpeg is ffmpeg-over-ip-client
          </div>
          {/* biome-ignore lint/a11y/noNoninteractiveTabindex: scrollable region must be reachable by keyboard */}
          <pre ref={clientRef} className={styles.screen} tabIndex={0}>
            <span className={styles.prompt}>$ </span>
            {command}
            {"\n"}
            {terminal(stderr)}
          </pre>
          <div className={styles.status}>
            {exit ? (
              <>
                Client exited with code <span className={styles.ok}>{exit.code}</span>, {exit.t.toFixed(1)} ms after
                connecting.
              </>
            ) : (
              "Running."
            )}
          </div>
        </section>

        <section className={styles.pane} aria-label="Server log">
          <div className={styles.paneHead}>
            <strong>Server</strong>macOS host; ffmpeg-over-ip-server with &quot;log&quot;: &quot;stdout&quot;
          </div>
          {/* biome-ignore lint/a11y/noNoninteractiveTabindex: scrollable region must be reachable by keyboard */}
          <pre ref={serverRef} className={styles.screen} tabIndex={0}>
            <span className={styles.prompt}>$ </span>./ffmpeg-over-ip-server{"\n"}
            {log.map((l) => l.text).join("\n")}
          </pre>
          <div className={styles.status}>{exit ? "Waiting for the next connection." : "Session running."}</div>
        </section>
      </div>

      <section aria-label="Messages on the connection">
        <div className={styles.messagesHead}>
          <span>
            <strong>Messages on the connection</strong>
            {visible.length} of {events.length} rows
          </span>
          <span>Each reply from the client is shown on the row of the request it answers.</span>
        </div>
        <div className={styles.colnames} aria-hidden="true">
          <span className={styles.ms}>ms</span>
          <span className={styles.dir}>sent to</span>
          <span className={styles.type}>message</span>
          <span className={styles.detail}>contents</span>
          <span className={styles.reply}>client&apos;s reply</span>
        </div>
        {/* biome-ignore lint/a11y/noNoninteractiveTabindex: scrollable region must be reachable by keyboard */}
        <ol ref={listRef} className={styles.list} tabIndex={0} aria-label="Messages">
          {visible.map((e, i) => (
            <li key={`${e.t}-${e.type}`} className={`${styles.row} ${i === current ? styles.current : ""}`}>
              <span className={styles.ms}>{e.t.toFixed(1)}</span>
              <span className={styles.dir}>{e.dir === "c2s" ? "→ server" : "→ client"}</span>
              <span className={styles.type}>{e.type}</span>
              <span className={styles.detail}>{detail(e)}</span>
              {e.reply ? <span className={styles.reply}>{replyText(e, time)}</span> : null}
            </li>
          ))}
        </ol>
        <dl className={styles.counters}>
          <div>
            <dt>Read from the client&apos;s disk</dt>
            <dd>
              {bytes.format(sums.readBytes)} bytes in {sums.reads} READs
            </dd>
          </div>
          <div>
            <dt>Written to the client&apos;s disk</dt>
            <dd>
              {bytes.format(sums.writeBytes)} bytes in {sums.writes} WRITEs
            </dd>
          </div>
          <div>
            <dt>Files opened on the client</dt>
            <dd>{sums.opens}</dd>
          </div>
          <div>
            <dt>stderr chunks forwarded</dt>
            <dd>{sums.stderr}</dd>
          </div>
        </dl>
      </section>
    </div>
  )
}
