const GITHUB_URL = "https://github.com/steelbrain/ffmpeg-over-ip"
const RELEASE_URL = "https://github.com/steelbrain/ffmpeg-over-ip/releases/latest"

export function Footer() {
  return (
    <footer className="border-t border-white/10 bg-[#0a0a0a]">
      <div className="mx-auto max-w-6xl px-6 py-12">
        <div className="flex flex-col items-center gap-6 sm:flex-row sm:justify-between">
          <div className="text-sm text-gray-400">
            <p>
              Made by{" "}
              <a href="https://aneesiqbal.ai/" className="text-blue-400 hover:text-blue-300 underline">
                Anees Iqbal
              </a>
            </p>
          </div>
          <div className="flex gap-6 text-sm text-gray-400">
            <a
              href={GITHUB_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-white transition-colors"
            >
              GitHub
            </a>
            <a
              href={RELEASE_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-white transition-colors"
            >
              Releases
            </a>
            <a
              href={`${GITHUB_URL}/blob/main/LICENSE.md`}
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-white transition-colors"
            >
              License
            </a>
          </div>
        </div>
      </div>
    </footer>
  )
}
