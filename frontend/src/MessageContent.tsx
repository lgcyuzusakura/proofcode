import { useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Check, Copy } from "lucide-react";

function CodeBlock({ children }: { children: React.ReactNode }) {
  const [copied, setCopied] = useState(false);
  return <div className="chat-code-block"><button type="button" className="text-button" aria-label="复制代码" onClick={async event => {
    const value = event.currentTarget.parentElement?.querySelector("pre")?.textContent || "";
    try { await navigator.clipboard.writeText(value); setCopied(true); setTimeout(() => setCopied(false), 1800); } catch { setCopied(false); }
  }}>{copied ? <Check size={12}/> : <Copy size={12}/>} {copied ? "已复制" : "复制"}</button><pre>{children}</pre></div>;
}

export function MessageContent({ content }: { content: string }) {
  return <div className="chat-markdown"><ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml components={{
    pre: ({ children }) => <CodeBlock>{children}</CodeBlock>,
    a: ({ href, children }) => <a href={href} target="_blank" rel="noopener noreferrer">{children}</a>,
    img: ({ alt }) => <span className="chat-image-description">[图片：{alt || "未命名"}]</span>
  }}>{content}</ReactMarkdown></div>;
}
