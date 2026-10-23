import { useEffect, useRef, useState } from "react";
import { api, post } from "./api";
import type { Task, TaskEvent } from "./types";
import "./task-progress.css";

type Instruction = { id: string; sequence: number; content: string; deliveredAt?: string | null };
type PlanStep = { id: string; text: string; status: string };
const active = new Set(["QUEUED", "RUNNING", "VERIFYING", "WAITING_APPROVAL"]);
const labels: Record<string, string> = { pending: "待处理", in_progress: "进行中", completed: "已完成", blocked: "受阻" };

export function TaskProgress({ task, events, onNotice, onUsePlan }: { task: Task; events: TaskEvent[]; onNotice: (message: string) => void; onUsePlan?: (prompt: string) => void }) {
  const [instructions, setInstructions] = useState<Instruction[]>([]);
  const [content, setContent] = useState("");
  const [busy, setBusy] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [refresh, setRefresh] = useState(0);
  // Retain the key when a response is lost, so retrying cannot duplicate a message.
  const pending = useRef<{ content: string; key: string } | null>(null);
  const path = `/api/tasks/${task.id}/instructions`;
  const editable = task.executionMode !== "CHAT" && active.has(task.status);
  useEffect(() => {
    if (task.executionMode === "CHAT") return;
    let live = true;
    const load = async () => {
      try {
        const values = await api<Instruction[]>(path);
        if (!Array.isArray(values) || !values.every(item => item && typeof item.id === "string" && Number.isSafeInteger(item.sequence) && typeof item.content === "string" && (item.deliveredAt == null || typeof item.deliveredAt === "string"))) throw new Error("Invalid instruction response");
        if (live) { setInstructions(values); setLoadError(""); }
      }
      catch { if (live) setLoadError("未能读取补充指令，请重试。"); }
    };
    void load();
    const timer = editable ? window.setInterval(() => void load(), 3000) : undefined;
    return () => { live = false; window.clearInterval(timer); };
  }, [path, task.attempt, task.status, refresh]);
  if (task.executionMode === "CHAT") return null;
  const event = [...events].reverse().find(item => item.type === "plan.updated" && (item.attempt == null || item.attempt === task.attempt));
  const steps = (Array.isArray(event?.payload.steps) ? event.payload.steps : []) as PlanStep[];
  const send = async () => {
    const text = content.trim(); if (!text || busy || !editable) return;
    if (pending.current?.content !== text) pending.current = { content: text, key: crypto.randomUUID() };
    setBusy(true);
    try {
      const value = await post<Instruction>(path, { content: text, clientKey: pending.current.key, attempt: task.attempt || 1 });
      setInstructions(old => [...old.filter(item => item.id !== value.id), value].sort((a, b) => a.sequence - b.sequence));
      setContent(""); pending.current = null; setRefresh(value => value + 1);
    } catch { onNotice("补充指令未确认送达，内容已保留。可重试；任务结束后请新开一条消息。"); }
    finally { setBusy(false); }
  };
  return <div className="task-progress">
    {!!steps.length && <details className="task-plan" open><summary>{task.executionMode === "PLAN" ? "规划清单" : "执行计划"} <span>{steps.filter(item => item.status === "completed").length}/{steps.length}</span></summary>
      {!!event?.payload.summary && <p>{String(event.payload.summary)}</p>}
      <ol>{steps.map(step => <li key={step.id} data-status={step.status}><span>{labels[step.status] || step.status}</span><div>{step.text}</div></li>)}</ol>
    </details>}
    {task.executionMode === "PLAN" && task.status === "SUCCEEDED" && (!!steps.length || !!task.result) && onUsePlan && <div className="plan-execution"><button type="button" className="secondary-button" onClick={() => onUsePlan(`请根据以下方案执行代码任务。先核对当前项目状态，方案中的建议不代表修改或验证已经完成。\n\n原始目标：\n${task.prompt}\n\n方案来源：${task.id}，任务尝试 ${task.attempt || 1}\n${task.sourceSnapshotId ? `源码快照：${task.sourceSnapshotId}\n` : ""}\n${task.result || [String(event?.payload.summary || ""), ...steps.map(step => `- ${step.text}`)].join("\n")}`)}>用此方案创建代码任务</button><small>填入输入框，检查后发送。</small></div>}
    {(editable || !!instructions.length || !!loadError) && <details className="task-instructions"><summary>补充指令{instructions.length ? `（${instructions.length}）` : ""}</summary>
      {instructions.map(item => <article key={item.id}><small>{item.deliveredAt ? "已交付模型上下文" : editable ? "等待下一次模型调用" : "任务已结束，未交付"}</small><p>{item.content}</p></article>)}
      {loadError && <p role="status">{loadError}<button type="button" className="text-button" onClick={() => setRefresh(value => value + 1)}>重试</button></p>}
      {editable && <div className="instruction-editor"><label htmlFor={`instruction-${task.id}`}>调整当前任务</label><textarea id={`instruction-${task.id}`} value={content} disabled={busy} maxLength={16000} rows={2} onChange={e => setContent(e.target.value)} placeholder="补充要求或调整方向…"/>
        <div><small>下一次模型调用前生效；已执行动作不会撤销。</small><button type="button" className="secondary-button" disabled={busy || !content.trim()} onClick={() => void send()}>{busy ? "发送中…" : "发送补充"}</button></div>
      </div>}
    </details>}
  </div>;
}
