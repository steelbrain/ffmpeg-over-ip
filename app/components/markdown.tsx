import Link from "next/link"
import type { Components } from "react-markdown"
import ReactMarkdown from "react-markdown"
import rehypeSlug from "rehype-slug"
import remarkGfm from "remark-gfm"

const components: Components = {
  a: ({ href, children, ...props }) => {
    if (href?.startsWith("/")) {
      return (
        <Link href={href} {...props}>
          {children}
        </Link>
      )
    }
    return (
      <a href={href} target="_blank" rel="noopener noreferrer" {...props}>
        {children}
      </a>
    )
  },
}

export function Markdown({ content }: { content: string }) {
  return (
    <div className="prose prose-invert max-w-none prose-headings:font-semibold prose-a:text-blue-400 prose-a:no-underline hover:prose-a:underline prose-code:font-normal prose-code:rounded prose-code:bg-white/10 prose-code:px-1.5 prose-code:py-0.5 prose-code:before:content-none prose-code:after:content-none prose-pre:bg-white/5 [&_pre_code]:bg-transparent [&_pre_code]:p-0 [&_pre_code]:rounded-none prose-pre:border prose-pre:border-white/10 prose-th:text-left prose-table:text-sm">
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeSlug]} components={components}>
        {content}
      </ReactMarkdown>
    </div>
  )
}
