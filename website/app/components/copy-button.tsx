"use client"

import { useEffect, useState } from "react"

export function CopyButton({
  text,
  getText,
  label = "Copy",
}: {
  text?: string
  getText?: () => string
  label?: string
}) {
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle")

  useEffect(() => {
    if (state === "idle") return
    const id = setTimeout(() => setState("idle"), 2000)
    return () => clearTimeout(id)
  }, [state])

  async function copy() {
    try {
      await navigator.clipboard.writeText(text ?? getText?.() ?? "")
      setState("copied")
    } catch {
      setState("failed")
    }
  }

  return (
    <button
      type="button"
      onClick={copy}
      className="shrink-0 cursor-pointer rounded border border-line bg-bg px-2 py-0.5 font-sans text-xs text-muted hover:text-fg"
    >
      <span aria-live="polite">{state === "copied" ? "Copied" : state === "failed" ? "Copy failed" : label}</span>
    </button>
  )
}
