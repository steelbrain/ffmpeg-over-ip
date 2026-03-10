"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { useState } from "react"
import type { DocPage } from "./content"

function useLinkClass() {
  const pathname = usePathname()
  return (slug: string) =>
    `block rounded-lg px-3 py-2 text-sm transition-colors ${
      pathname === `/docs/${slug}` ? "bg-white/10 text-white" : "text-gray-400 hover:bg-white/5 hover:text-white"
    }`
}

export function MobileNav({ docs }: { docs: DocPage[] }) {
  const [open, setOpen] = useState(false)
  const linkClass = useLinkClass()

  return (
    <div className="mb-6 md:hidden">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="flex items-center gap-2 rounded-lg border border-white/10 px-3 py-2 text-sm text-gray-300 hover:bg-white/5 transition-colors"
      >
        <svg
          className="h-4 w-4"
          fill="none"
          viewBox="0 0 24 24"
          strokeWidth={2}
          stroke="currentColor"
          aria-hidden="true"
        >
          {open ? (
            <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
          ) : (
            <path strokeLinecap="round" strokeLinejoin="round" d="M3.75 6.75h16.5M3.75 12h16.5m-16.5 5.25h16.5" />
          )}
        </svg>
        Documentation
      </button>
      {open && (
        <div className="mt-2 space-y-1 rounded-lg border border-white/10 bg-white/5 p-2">
          {docs.map((doc) => (
            <Link
              key={doc.slug}
              href={`/docs/${doc.slug}`}
              onClick={() => setOpen(false)}
              className={linkClass(doc.slug)}
            >
              {doc.title}
            </Link>
          ))}
        </div>
      )}
    </div>
  )
}

export function DesktopNav({ docs }: { docs: DocPage[] }) {
  const linkClass = useLinkClass()

  return (
    <nav className="hidden w-56 shrink-0 md:block">
      <div className="sticky top-24 space-y-1">
        <div className="mb-4 text-xs font-semibold uppercase tracking-wider text-gray-500">Documentation</div>
        {docs.map((doc) => (
          <Link key={doc.slug} href={`/docs/${doc.slug}`} className={linkClass(doc.slug)}>
            {doc.title}
          </Link>
        ))}
      </div>
    </nav>
  )
}
