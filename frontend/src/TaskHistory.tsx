import { useEffect, useRef, useState } from "react";
import { FileCode2, RefreshCw } from "lucide-react";
import { api } from "./api";
import type { Task, TaskArtifact } from "./types";
import "./history.css";

type HistoryEntry = { sequence: number; attempt: number; type: string; title: string; timestamp: string };
type HistoryPage = { taskId: string; currentAttempt: number; status: string; entries: HistoryEntry[]; artifacts: TaskArtifact[]; nextBeforeSequence: number | null };

export function TaskHistory({ task, onReview }: { task: Task | null; onReview: (artifact: TaskArtifact) => void }) {
 const [entries, setEntries] = useState<HistoryEntry[]>([]), [artifacts, setArtifacts] = useState<TaskArtifact[]>([]), [before, setBefore] = useState<number | null>(null), [busy, setBusy] = useState(false), [error, setError] = useState("");
 const generation = useRef(0), request = useRef<AbortController | null>(null);
 const load = async (next: number | null = null) => {
  if (!task) return;request.current?.abort();const controller = new AbortController();request.current = controller;const id = ++generation.current;
  setBusy(true);setError("");if (!next) { setEntries([]);setArtifacts([]);setBefore(null); }
  try {
   const value = await api<HistoryPage>(`/api/tasks/${task.id}/history${next ? `?beforeSequence=${next}` : ""}`, { signal: controller.signal });
   if (value.taskId !== task.id || !Array.isArray(value.entries) || !Array.isArray(value.artifacts) || (next && value.nextBeforeSequence != null && value.nextBeforeSequence >= next)) throw new Error("恢复记录分页无效，请刷新重试。");
   if (id !== generation.current || controller.signal.aborted) return;
   setEntries(old => next ? [...old, ...value.entries.filter(item => !old.some(existing => existing.sequence === item.sequence))] : value.entries);
   setArtifacts(old => next ? [...old, ...value.artifacts.filter(item => !old.some(existing => existing.id === item.id))] : value.artifacts);setBefore(value.nextBeforeSequence);
  } catch (cause) { if (!controller.signal.aborted && id === generation.current) setError(String(cause)); }
  finally { if (id === generation.current) setBusy(false); }
 };
 useEffect(() => { void load();return () => { request.current?.abort();generation.current++; }; }, [task?.id, task?.attempt]);
 if (!task) return <p className="history-empty">选择任务后查看恢复记录。</p>;
 const attempts = [...new Set([task.attempt || 1, ...entries.map(item => item.attempt), ...artifacts.map(item => item.attempt || 1)])].sort((a, b) => b - a);
 return <section className="task-history" aria-label="任务恢复记录" aria-busy={busy}>
  <div className="task-history-heading"><p>查看各次执行的状态与已保存产物。打开记录不会重试任务或应用补丁。</p><button type="button" className="secondary-button" disabled={busy} onClick={() => void load()}><RefreshCw size={14}/>刷新记录</button></div>
  {error && <p className="history-error" role="alert">{error}</p>}
  {attempts.map(attempt => <section className="task-history-attempt" key={attempt}><h3>{attempt === (task.attempt || 1) ? "当前执行" : "历史执行"}<small>尝试 {attempt}</small></h3><div className="task-history-artifacts">{artifacts.filter(item => (item.attempt || 1) === attempt).map(item => <button type="button" key={item.id} onClick={() => onReview(item)}><FileCode2 size={16}/><span><strong>{item.kind === "recovery" ? "查看恢复补丁" : "查看检查点修改"}</strong><small>{new Date(item.createdAt).toLocaleString()}{item.commitHash ? ` · ${item.commitHash.slice(0, 12)}` : ""}</small></span></button>)}</div><ol>{entries.filter(item => item.attempt === attempt).map(item => <li key={item.sequence}><span>{item.title}</span><time>{new Date(item.timestamp).toLocaleString()}</time></li>)}</ol>{!entries.some(item => item.attempt === attempt) && <p>{busy ? "正在读取执行记录…" : "该次执行尚无已保存的状态记录。"}</p>}</section>)}
  {before && <button type="button" className="secondary-button" disabled={busy} onClick={() => void load(before)}>{busy ? "加载中…" : "加载更早记录"}</button>}
 </section>;
}
