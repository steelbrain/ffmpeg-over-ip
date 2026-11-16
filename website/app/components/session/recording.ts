import data from "./recording.json"

// A real session, recorded with a logging proxy between ffmpeg-over-ip-client
// and ffmpeg-over-ip-server (v7.0.0). Times are milliseconds since the client
// connected. File requests (sent by the server on behalf of its ffmpeg) are
// paired with the client's reply.

export interface Reply {
  t: number
  type: string
  n?: number
  size?: number
  offset?: number
  mode?: number
  errno?: number
}

export interface SessionEvent {
  t: number
  dir: "c2s" | "s2c"
  type: string
  path?: string
  n?: number
  offset?: number
  whence?: number
  fid?: number
  flags?: number
  code?: number
  text?: string
  version?: number
  args?: string[]
  program?: number
  reply?: Reply
}

export interface ServerLogLine {
  /** Message time the line belongs to; null for lines logged before the session. */
  t: number | null
  text: string
}

export interface Recording {
  recordedOn: string
  version: string
  command: string
  duration: number
  serverLog: ServerLogLine[]
  events: SessionEvent[]
}

export const recording = data as unknown as Recording

/** Totals over the whole session, used in the page copy. */
export function totals(events: SessionEvent[], until = Number.POSITIVE_INFINITY) {
  let reads = 0
  let readBytes = 0
  let writes = 0
  let writeBytes = 0
  let opens = 0
  let stderr = 0
  for (const e of events) {
    if (e.t > until) break
    if (e.type === "OPEN") opens++
    if (e.type === "STDERR") stderr++
    if (e.type === "READ") {
      reads++
      if (e.reply && e.reply.t <= until) readBytes += e.reply.n ?? 0
    }
    if (e.type === "WRITE") {
      writes++
      if (e.reply && e.reply.t <= until) writeBytes += e.reply.n ?? 0
    }
  }
  return { reads, readBytes, writes, writeBytes, opens, stderr }
}

export function eventTime(type: string): number {
  const e = recording.events.find((x) => x.type === type)
  if (!e) throw new Error(`no ${type} in recording`)
  return e.t
}

export const bytes = new Intl.NumberFormat("en-US")
