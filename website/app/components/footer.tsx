import { CHANGELOG_URL, GITHUB_URL, LICENSE_URL, RELEASES_URL } from "./links"

export function Footer() {
  return (
    <footer className="mt-24 border-t border-line">
      <div className="mx-auto max-w-6xl px-4 py-8 text-sm text-muted sm:px-6">
        <p className="max-w-2xl">
          ffmpeg-over-ip is MIT licensed, except the fio layer and the ffmpeg patches, which are GPL v3 because they are
          derived from ffmpeg. See <a href={LICENSE_URL}>LICENSE.md</a>. Made by{" "}
          <a href="https://aneesiqbal.ai/">Anees Iqbal</a>.
        </p>
        <p className="mt-3 flex flex-wrap gap-x-5 gap-y-1">
          <a href={GITHUB_URL}>Source on GitHub</a>
          <a href={RELEASES_URL}>Releases</a>
          <a href={CHANGELOG_URL}>Changelog</a>
        </p>
      </div>
    </footer>
  )
}
