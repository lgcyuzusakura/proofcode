import { useState } from "react";
import { Check, Copy, Pencil } from "lucide-react";

export function MessageActions({ content, reuse }: { content: string; reuse?: () => void }) {
  const [state, setState] = useState<"ready" | "copied" | "failed">("ready");
  return <div className="message-actions"><button type="button" aria-label="复制消息" onClick={() => void navigator.clipboard.writeText(content).then(() => setState("copied"), () => setState("failed"))}>{state === "copied" ? <Check size={12}/> : <Copy size={12}/>}<span>{state === "copied" ? "已复制" : state === "failed" ? "复制失败，请手动选择" : "复制"}</span></button>{reuse && <button type="button" onClick={reuse}><Pencil size={12}/>修改后重发</button>}</div>;
}
