import type { Metadata } from "next"
import Link from "next/link"
import { docs } from "./content"
import { MobileNav } from "./mobile-nav"

export const metadata: Metadata = {
  title: "Documentation — ffmpeg-over-ip",
}

export default function DocsLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="mx-auto max-w-6xl px-6 py-10">
      <MobileNav docs={docs} />

      <div className="flex gap-10">
        <nav className="hidden w-56 shrink-0 md:block">
          <div className="sticky top-24 space-y-1">
            <div className="mb-4 text-xs font-semibold uppercase tracking-wider text-gray-500">Documentation</div>
            {docs.map((doc) => (
              <Link
                key={doc.slug}
                href={`/docs/${doc.slug}`}
                className="block rounded-lg px-3 py-2 text-sm text-gray-400 hover:bg-white/5 hover:text-white transition-colors"
              >
                {doc.title}
              </Link>
            ))}
          </div>
        </nav>
        <article className="min-w-0 flex-1">{children}</article>
      </div>
    </div>
  )
}
