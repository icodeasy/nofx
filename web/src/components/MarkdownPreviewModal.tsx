import { useEffect } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import rehypeRaw from 'rehype-raw'
import { X } from 'lucide-react'

interface MarkdownPreviewModalProps {
  content: string
  title: string
  onClose: () => void
}

export function MarkdownPreviewModal({ content, title, onClose }: MarkdownPreviewModalProps) {
  // Handle escape key to close modal
  useEffect(() => {
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handleEscape)
    return () => window.removeEventListener('keydown', handleEscape)
  }, [onClose])

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="relative w-full max-w-5xl h-[90vh] bg-[#0B0E11] rounded-lg shadow-2xl flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-nofx-gold/20">
          <h2 className="text-lg font-semibold text-nofx-text">{title}</h2>
          <button
            onClick={onClose}
            className="p-2 rounded-lg hover:bg-white/10 transition-colors text-nofx-text-muted hover:text-white"
            title="Close (Esc)"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto p-6">
          <div className="markdown-content max-w-none">
            <ReactMarkdown
              remarkPlugins={[remarkGfm]}
              rehypePlugins={[rehypeHighlight, rehypeRaw]}
              components={{
                // Custom code block styling
                pre: ({ children, ...props }) => (
                  <pre
                    className="bg-[#1a1d21] p-4 rounded-lg overflow-x-auto border border-nofx-gold/20 my-4"
                    {...props}
                  >
                    {children}
                  </pre>
                ),
                // Inline code styling
                code: ({ className, children, ...props }) => {
                  const match = /language-(\w+)/.exec(className || '')
                  return match ? (
                    <code className={className} {...props}>
                      {children}
                    </code>
                  ) : (
                    <code
                      className="bg-[#1a1d21] px-2 py-0.5 rounded text-sm font-mono text-purple-400"
                      {...props}
                    >
                      {children}
                    </code>
                  )
                },
                // Heading styling
                h1: ({ children, ...props }) => (
                  <h1 className="text-3xl font-bold text-nofx-text mt-8 mb-4 pb-2 border-b border-nofx-gold/20" {...props}>
                    {children}
                  </h1>
                ),
                h2: ({ children, ...props }) => (
                  <h2 className="text-2xl font-semibold text-nofx-text mt-6 mb-3 pb-2 border-b border-nofx-gold/10" {...props}>
                    {children}
                  </h2>
                ),
                h3: ({ children, ...props }) => (
                  <h3 className="text-xl font-semibold text-nofx-text mt-5 mb-2" {...props}>
                    {children}
                  </h3>
                ),
                h4: ({ children, ...props }) => (
                  <h4 className="text-lg font-semibold text-nofx-text mt-4 mb-2" {...props}>
                    {children}
                  </h4>
                ),
                // Paragraph styling
                p: ({ children, ...props }) => (
                  <p className="text-nofx-text leading-relaxed my-3" {...props}>
                    {children}
                  </p>
                ),
                // List styling
                ul: ({ children, ...props }) => (
                  <ul className="list-disc list-inside text-nofx-text my-3 space-y-1" {...props}>
                    {children}
                  </ul>
                ),
                ol: ({ children, ...props }) => (
                  <ol className="list-decimal list-inside text-nofx-text my-3 space-y-1" {...props}>
                    {children}
                  </ol>
                ),
                li: ({ children, ...props }) => (
                  <li className="text-nofx-text ml-4" {...props}>
                    {children}
                  </li>
                ),
                // Blockquote styling
                blockquote: ({ children, ...props }) => (
                  <blockquote className="border-l-4 border-purple-500 pl-4 py-1 my-4 text-nofx-text-muted italic" {...props}>
                    {children}
                  </blockquote>
                ),
                // Link styling
                a: ({ href, children, ...props }) => (
                  <a
                    href={href}
                    className="text-purple-400 hover:text-purple-300 hover:underline"
                    target="_blank"
                    rel="noopener noreferrer"
                    {...props}
                  >
                    {children}
                  </a>
                ),
                // Table styling
                table: ({ children, ...props }) => (
                  <div className="overflow-x-auto my-4">
                    <table className="min-w-full border border-nofx-gold/20" {...props}>
                      {children}
                    </table>
                  </div>
                ),
                thead: ({ children, ...props }) => (
                  <thead className="bg-[#1a1d21]" {...props}>
                    {children}
                  </thead>
                ),
                th: ({ children, ...props }) => (
                  <th className="px-4 py-2 text-left text-nofx-text font-semibold border-b border-nofx-gold/20" {...props}>
                    {children}
                  </th>
                ),
                td: ({ children, ...props }) => (
                  <td className="px-4 py-2 text-nofx-text border-b border-nofx-gold/10" {...props}>
                    {children}
                  </td>
                ),
                // Horizontal rule styling
                hr: (props) => <hr className="border-t border-nofx-gold/20 my-6" {...props} />,
                // Image styling
                img: ({ src, alt, ...props }) => (
                  <img src={src} alt={alt} className="max-w-full h-auto rounded-lg my-4" {...props} />
                ),
                // Strong styling
                strong: ({ children, ...props }) => (
                  <strong className="font-bold text-nofx-text" {...props}>
                    {children}
                  </strong>
                ),
                // Em styling
                em: ({ children, ...props }) => (
                  <em className="italic text-nofx-text-muted" {...props}>
                    {children}
                  </em>
                ),
              }}
            >
              {content}
            </ReactMarkdown>
          </div>
        </div>

        {/* Footer */}
        <div className="px-6 py-3 border-t border-nofx-gold/20 bg-[#0B0E11]/50">
          <div className="flex items-center justify-between text-xs text-nofx-text-muted">
            <span>{content.length.toLocaleString()} characters</span>
            <span>Press Esc to close</span>
          </div>
        </div>
      </div>
    </div>
  )
}
