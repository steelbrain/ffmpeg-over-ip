import Image from "next/image";

export default function Home() {
  return (
    <div className="flex flex-col items-center justify-center min-h-screen bg-gradient-to-b from-gray-900 to-black text-white p-6">
      <div className="max-w-3xl mx-auto text-center">
        <div className="mb-8">
          <h1 className="text-5xl font-bold mb-4 bg-clip-text text-transparent bg-gradient-to-r from-blue-400 to-emerald-400">
            ffmpeg-over-ip
          </h1>
          <div className="text-xl font-semibold mb-2 text-blue-300">
            Coming Soon
          </div>
        </div>

        <div className="mb-10 space-y-6">
          <p className="text-xl">
            GPU accelerated ffmpeg from containers and VMs without GPU passthrough
          </p>

          <div className="bg-gray-800 p-6 rounded-lg shadow-xl text-left">
            <h2 className="text-xl font-semibold mb-4 text-emerald-300">How it works</h2>
            <p className="mb-4">
              ffmpeg-over-ip is a client-server combo that allows you to transcode videos on a machine with access to a GPU from a container or a VM without having to passthrough a GPU. This means you can run GPU accelerated ffmpeg from a docker container and use the GPU from the hypervisor or your Gaming PC running Windows.
            </p>
            <p>
              ffmpeg-over-ip uses a shared filesystem (this could be a Docker mount, NFS or SMB) to transfer video data and a lightweight communication protocol to coordinate the commands. This means that you do not need an ssh server running on the GPU-host (making Windows support easier/simpler), and that the client can stream the output(s) as they are processed. This allows ffmpeg-over-ip to be used in Plex or Emby media servers for just-in-time transcoding.
            </p>
          </div>
        </div>

        <div className="mt-10">
          <a
            href="https://github.com/steelbrain/ffmpeg-over-ip"
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-2 bg-blue-600 hover:bg-blue-700 text-white font-medium py-3 px-6 rounded-full transition-colors duration-200"
          >
            <svg className="h-5 w-5" fill="currentColor" viewBox="0 0 24 24" aria-hidden="true">
              <path fillRule="evenodd" d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z" clipRule="evenodd" />
            </svg>
            View on GitHub
          </a>
        </div>
      </div>

      <footer className="mt-20 text-gray-400 text-sm space-y-2">
        <p>Made with ❤️ by <a href="https://aneesiqbal.ai/" className="text-blue-400 hover:text-blue-300 underline">Anees Iqbal</a> (@steelbrain)</p>
      </footer>
    </div>
  );
}
