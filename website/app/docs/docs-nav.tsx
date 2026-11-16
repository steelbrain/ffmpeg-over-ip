"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { useEffect, useRef } from "react"

export interface NavItem {
  slug: string
  title: string
}

function Links({ items, onNavigate }: { items: NavItem[]; onNavigate?: () => void }) {
  const pathname = usePathname()
  return (
    <ul className="space-y-0.5">
      {items.map((item) => {
        const active = pathname === `/docs/${item.slug}`
        return (
          <li key={item.slug}>
            <Link
              href={`/docs/${item.slug}`}
              onClick={onNavigate}
              aria-current={active ? "page" : undefined}
              className={`-ml-3 block border-l-2 py-1 pl-3 no-underline ${
                active ? "border-accent font-medium text-fg" : "border-transparent text-muted hover:text-fg"
              }`}
            >
              {item.title}
            </Link>
          </li>
        )
      })}
    </ul>
  )
}

export function DesktopNav({ items }: { items: NavItem[] }) {
  return (
    <nav aria-label="Documentation" className="hidden w-48 shrink-0 md:block">
      <div className="sticky top-6 pl-3">
        <Links items={items} />
      </div>
    </nav>
  )
}

export function MobileNav({ items }: { items: NavItem[] }) {
  const pathname = usePathname()
  const ref = useRef<HTMLDetailsElement>(null)
  const current = items.find((i) => pathname === `/docs/${i.slug}`)

  // Close the menu after navigating to another page.
  // biome-ignore lint/correctness/useExhaustiveDependencies: run on every path change
  useEffect(() => {
    if (ref.current) ref.current.open = false
  }, [pathname])

  return (
    <details ref={ref} className="mb-8 rounded-md border border-line md:hidden">
      <summary className="cursor-pointer px-3 py-2 text-sm">
        <span className="text-muted">Documentation:</span> {current?.title ?? "Pages"}
      </summary>
      <nav aria-label="Documentation" className="border-t border-line px-3 py-2 pl-6">
        <Links items={items} />
      </nav>
    </details>
  )
}
