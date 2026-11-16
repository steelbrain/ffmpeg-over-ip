import type { Metadata } from "next"
import { notFound } from "next/navigation"
import { Markdown } from "@/app/components/markdown"
import { docs, getDoc } from "../content"

export function generateStaticParams() {
  return docs.map((doc) => ({ slug: doc.slug }))
}

export const dynamicParams = false

export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }): Promise<Metadata> {
  const { slug } = await params
  const doc = getDoc(slug)
  if (!doc) return {}
  return {
    title: doc.title,
    description: doc.description,
  }
}

export default async function DocPage({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params
  const doc = getDoc(slug)
  if (!doc) notFound()

  return (
    <>
      <h1 className="text-[2rem] font-semibold leading-tight tracking-tight">{doc.title}</h1>
      <div className="mt-6">
        <Markdown content={doc.content} />
      </div>
    </>
  )
}
