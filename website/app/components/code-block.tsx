"use client"

import { useRef } from "react"
import { CopyButton } from "./copy-button"

export function CodeBlock({ lang, children }: { lang?: string; children: React.ReactNode }) {
  const ref = useRef<HTMLPreElement>(null)
  return (
    <div className="code-block">
      <div className="flex items-center justify-between gap-3 rounded-t-md border border-b-0 border-line bg-panel px-3 py-1 text-xs text-muted">
        <span className="font-mono">{lang ?? ""}</span>
        <CopyButton getText={() => (ref.current?.textContent ?? "").replace(/\n$/, "")} />
      </div>
      {/* Plain-text blocks (messages, output) wrap; code keeps its lines and scrolls. */}
      <pre
        ref={ref}
        className={lang ? "rounded-t-none" : "rounded-t-none whitespace-pre-wrap [overflow-wrap:anywhere]"}
      >
        {children}
      </pre>
    </div>
  )
}
