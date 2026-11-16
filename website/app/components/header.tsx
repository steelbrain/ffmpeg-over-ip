import Link from "next/link"
import { CHANGELOG_URL, GITHUB_URL, RELEASES_URL } from "./links"

const navLink = "text-muted no-underline hover:text-fg"

export function Header() {
  return (
    <header className="border-b border-line">
      <div className="mx-auto flex max-w-6xl flex-wrap items-baseline justify-between gap-x-6 gap-y-2 px-4 py-4 sm:px-6">
        <Link href="/" className="font-mono text-[0.9375rem] font-semibold text-fg no-underline">
          ffmpeg-over-ip
        </Link>
        <nav aria-label="Site" className="flex flex-wrap gap-x-5 gap-y-1 text-[0.9375rem]">
          <Link href="/docs/quick-start" className={navLink}>
            Docs
          </Link>
          <a href={CHANGELOG_URL} className={navLink}>
            Changelog
          </a>
          <a href={RELEASES_URL} className={navLink}>
            Releases
          </a>
          <a href={GITHUB_URL} className={navLink}>
            GitHub
          </a>
        </nav>
      </div>
    </header>
  )
}
