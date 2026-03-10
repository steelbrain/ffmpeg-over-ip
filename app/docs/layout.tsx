import type { Metadata } from "next"
import { docs } from "./content"
import { DesktopNav, MobileNav } from "./docs-nav"

export const metadata: Metadata = {
  title: "Documentation — ffmpeg-over-ip",
}

export default function DocsLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="mx-auto max-w-6xl px-6 py-10">
      <MobileNav docs={docs} />
      <div className="flex gap-10">
        <DesktopNav docs={docs} />
        <article className="min-w-0 flex-1">{children}</article>
      </div>
    </div>
  )
}
