import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

/**
 * 记忆正文的 Markdown 渲染（标题 / 列表 / 代码块 / 表格 / 引用…）。
 * 安全约定：react-markdown 默认不渲染原始 HTML，记忆内容无法向控制台注入脚本；
 * 链接统一新窗口打开。样式见 styles/console.css 的 .md-body。
 */
export function MemoryMarkdown({ content }: { content: string }) {
  return (
    <div className="md-body">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          a: ({ href, children }) => (
            <a href={href} target="_blank" rel="noreferrer noopener">
              {children}
            </a>
          ),
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  )
}
