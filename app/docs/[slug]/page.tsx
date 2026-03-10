import type { Metadata } from "next"
import { notFound } from "next/navigation"
import { Markdown } from "@/app/components/markdown"
import { docs, getDoc } from "../content"

export function generateStaticParams() {
  return docs.map((doc) => ({ slug: doc.slug }))
}

export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }): Promise<Metadata> {
  const { slug } = await params
  const doc = getDoc(slug)
  if (!doc) return {}
  return {
    title: `${doc.title} — ffmpeg-over-ip`,
  }
}

export default async function DocPage({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params
  const doc = getDoc(slug)
  if (!doc) notFound()

  return (
    <div>
      <h1 className="text-3xl font-bold">{doc.title}</h1>
      <div className="mt-8">
        <Markdown content={doc.content} />
      </div>
    </div>
  )
}
