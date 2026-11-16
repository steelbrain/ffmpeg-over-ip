import Link from "next/link"
import type { Components } from "react-markdown"
import ReactMarkdown from "react-markdown"
import rehypeSlug from "rehype-slug"
import remarkGfm from "remark-gfm"
import { CodeBlock } from "./code-block"

const components: Components = {
  a: ({ href, children, node: _node, ...props }) => {
    if (href?.startsWith("/")) {
      return (
        <Link href={href} {...props}>
          {children}
        </Link>
      )
    }
    return (
      <a href={href} {...props}>
        {children}
      </a>
    )
  },
  pre: ({ children, node }) => {
    const first = node?.children[0]
    const classes = first?.type === "element" ? first.properties.className : undefined
    const lang = Array.isArray(classes)
      ? (classes.map(String).find((c) => c.startsWith("language-")) ?? undefined)
      : undefined
    return <CodeBlock lang={lang?.slice("language-".length)}>{children}</CodeBlock>
  },
  table: ({ children, node: _node, ...props }) => (
    <div className="table-scroll">
      <table {...props}>{children}</table>
    </div>
  ),
}

export function Markdown({ content }: { content: string }) {
  return (
    <div className="doc">
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeSlug]} components={components}>
        {content}
      </ReactMarkdown>
    </div>
  )
}
