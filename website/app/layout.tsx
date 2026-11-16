import type { Metadata, Viewport } from "next"
import { Footer } from "./components/footer"
import { Header } from "./components/header"
import "./globals.css"

export const metadata: Metadata = {
  title: {
    default: "ffmpeg-over-ip",
    template: "%s · ffmpeg-over-ip",
  },
  description:
    "Use the GPU of another machine for ffmpeg. The client replaces the ffmpeg binary, and the server's ffmpeg reads and writes the client's files over one TCP connection.",
  icons: {
    icon: [{ url: "/icon.svg", type: "image/svg+xml" }],
  },
}

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#ffffff" },
    { media: "(prefers-color-scheme: dark)", color: "#111110" },
  ],
}

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode
}>) {
  return (
    <html lang="en">
      <body className="antialiased">
        <Header />
        <main>{children}</main>
        <Footer />
      </body>
    </html>
  )
}
