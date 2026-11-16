import type { Metadata } from "next"
import { docs } from "./content"
import { DesktopNav, MobileNav } from "./docs-nav"

export const metadata: Metadata = {
  title: {
    default: "Documentation",
    template: "%s · ffmpeg-over-ip",
  },
}

export default function DocsLayout({ children }: { children: React.ReactNode }) {
  const items = docs.map(({ slug, title }) => ({ slug, title }))
  return (
    <div className="mx-auto max-w-6xl px-4 pt-10 sm:px-6">
      <MobileNav items={items} />
      <div className="flex gap-12">
        <DesktopNav items={items} />
        <article className="min-w-0 flex-1">{children}</article>
      </div>
    </div>
  )
}
