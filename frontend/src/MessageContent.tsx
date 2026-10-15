import { isValidElement, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Check, Copy } from "lucide-react";

function CodeBlock({ children }: { children: React.ReactNode }) {
  const [copied, setCopied] = useState(false);
  const language = isValidElement<{ className?: string }>(children) ? children.props.className?.replace(/^language-/, "") : "";
  return <div className="chat-code-block"><div className="chat-code-header"><span>{language || "代码"}</span><button type="button" className="text-button" aria-label="复制代码" onClick={async event => {
    const value = event.currentTarget.closest(".chat-code-block")?.querySelector("pre")?.textContent || "";
    try { await navigator.clipboard.writeText(value); setCopied(true); setTimeout(() => setCopied(false), 1800); } catch { setCopied(false); }
  }}>{copied ? <Check size={12}/> : <Copy size={12}/>} {copied ? "已复制" : "复制"}</button></div><pre>{children}</pre></div>;
}

export function MessageContent({ content }: { content: string }) {
  return <div className="chat-markdown"><ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml components={{
    pre: ({ children }) => <CodeBlock>{children}</CodeBlock>,
    a: ({ href, children }) => <a href={href} target="_blank" rel="noopener noreferrer">{children}</a>,
    img: ({ alt }) => <span className="chat-image-description">[图片：{alt || "未命名"}]</span>
  }}>{content}</ReactMarkdown></div>;
}
