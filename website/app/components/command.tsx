import { CopyButton } from "./copy-button"

/** A shell command with a label and a copy button. */
export function Command({ label, command }: { label: string; command: string }) {
  return (
    <div className="mt-3">
      <div className="flex items-center justify-between gap-3 rounded-t-md border border-b-0 border-line bg-panel px-3 py-1.5 text-sm text-muted">
        <span>{label}</span>
        <CopyButton text={command} />
      </div>
      <pre className="whitespace-pre-wrap rounded-b-md border border-line px-3 py-2.5 text-[0.8125rem] leading-relaxed [overflow-wrap:anywhere]">
        <code>{command}</code>
      </pre>
    </div>
  )
}
