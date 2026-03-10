import type { Metadata } from "next"
import { Geist, Geist_Mono } from "next/font/google"
import { Footer } from "./components/footer"
import { Header } from "./components/header"
import "./globals.css"

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
})

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
})

export const metadata: Metadata = {
  title: "ffmpeg-over-ip — GPU-accelerated ffmpeg without GPU passthrough",
  description:
    "Use GPU-accelerated ffmpeg from anywhere — a Docker container, a VM, or a remote machine — without GPU passthrough or shared filesystems.",
  icons: {
    icon: [{ url: "/icon.svg", type: "image/svg+xml" }],
  },
}

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode
}>) {
  return (
    <html lang="en">
      <body className={`${geistSans.variable} ${geistMono.variable} antialiased`}>
        <Header />
        <main>{children}</main>
        <Footer />
      </body>
    </html>
  )
}
