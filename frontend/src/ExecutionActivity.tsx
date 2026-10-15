import { useEffect, useMemo, useRef, useState } from "react";
import { AlertTriangle, Check, ChevronRight, Circle, Loader2, Pause, RefreshCw, Sparkles, Square } from "lucide-react";
import { api } from "./api";
import type { Task, TaskEvent } from "./types";
import { activityDuration, activityText, buildActivities, isTaskActive, type ActivityItem } from "./activityModel";

function ActivityIcon({ item, spinning = false }: { item: ActivityItem; spinning?: boolean }) {
  return <span className={`execution-icon tone-${item.tone}`}>{spinning ? <Loader2 className="activity-spin" size={12}/> : item.tone === "success" ? <Check size={12}/> : item.tone === "error" ? <AlertTriangle size={12}/> : item.tone === "warning" ? <Pause size={12}/> : item.tone === "decide" ? <Sparkles size={12}/> : item.tone === "muted" ? <Square size={10}/> : <Circle size={11}/>}</span>;
}

export function ExecutionTimeline({ events, task }: { events: TaskEvent[]; task: Task }) {
  const items = useMemo(() => buildActivities(events), [events]);
  const latest = items[items.length - 1];
  if (!items.length) return null;
  return <div className="execution-timeline" role="group" aria-label="任务执行过程">{items.map(item => <details className={`execution-entry tone-${item.tone}`} key={item.key}>
    <summary><ActivityIcon item={item} spinning={isTaskActive(task.status) && latest === item && !!item.pending}/><span className="execution-copy"><span className="execution-heading"><strong>{item.title}</strong><span className="execution-model">{item.agent || task.model}</span>{item.durationMs != null && <span className="execution-duration">{activityDuration(item.durationMs)}</span>}</span><span className="execution-preview">{item.summary}</span></span><ChevronRight className="execution-chevron" size={13}/></summary>
    <div className="execution-detail"><p>{item.summary}</p><div className="execution-detail-meta"><time dateTime={item.timestamp}>{new Date(item.timestamp).toLocaleString()}</time>{item.durationMs != null && <span>执行用时 {activityDuration(item.durationMs)}</span>}</div>{item.input != null && <section><strong>输入</strong><pre>{activityText(item.input)}</pre></section>}{item.output != null && <section><strong>输出</strong><pre>{activityText(item.output)}</pre></section>}<details className="execution-raw"><summary>原始事件</summary><pre>{JSON.stringify(item.event.payload, null, 2)}</pre></details></div>
  </details>)}</div>;
}

// Load older task evidence as it approaches the viewport, with pagination and
// cancellation. The selected task continues to use the shared live transport.
export function TaskActivity({ task, selected, events }: { task: Task; selected: boolean; events: TaskEvent[] }) {
  const host = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(false);
  const [history, setHistory] = useState<TaskEvent[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const element = host.current;
    if (!element || selected) return;
    const observer = new IntersectionObserver(entries => { if (entries.some(entry => entry.isIntersecting)) { setVisible(true); observer.disconnect(); } }, { root: element.closest(".conversation-stream"), rootMargin: "250px" });
    observer.observe(element); return () => observer.disconnect();
  }, [selected]);
  useEffect(() => {
    if (selected || !visible) return;
    const controller = new AbortController();
    setLoaded(false); setError("");
    void (async () => {
      const collected = new Map<number, TaskEvent>();
      let cursor = 0;
      try {
        while (!controller.signal.aborted) {
          const batch = await api<TaskEvent[]>(`/api/tasks/${task.id}/events?afterSequence=${cursor}`, { signal: controller.signal });
          if (controller.signal.aborted) return;
          for (const item of batch) collected.set(item.sequence, item);
          if (batch.length < 500) break;
          const next = Math.max(...batch.map(item => item.sequence));
          if (next <= cursor) throw new Error("执行记录分页未推进");
          cursor = next;
        }
        if (!controller.signal.aborted) { setHistory([...collected.values()]); setLoaded(true); }
      } catch (cause) { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : String(cause)); }
    })();
    return () => controller.abort();
  }, [task.id, task.status, task.attempt, selected, visible, retry]);
  const evidence = (selected ? events : history).filter(event => task.attempt == null || (event.attempt ?? 1) === task.attempt);
  return <div ref={host} className="task-activity" data-task-id={task.id}>{selected || loaded ? <ExecutionTimeline events={evidence} task={task}/> : error ? <div className="execution-load-state"><AlertTriangle size={13}/><span>执行记录暂未加载</span><button type="button" title={error} onClick={() => setRetry(value => value + 1)}><RefreshCw size={12}/>重试</button></div> : <div className="execution-load-state">{visible && <Loader2 size={13}/>}<span>{visible ? "正在读取执行记录…" : "执行记录"}</span></div>}</div>;
}

export function CurrentActivity({ task, events, onShow, onStop }: { task: Task; events: TaskEvent[]; onShow: () => void; onStop: () => void }) {
  const [now, setNow] = useState(Date.now());
  const active = isTaskActive(task.status);
  useEffect(() => { setNow(Date.now()); if (!active) return; const timer = setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(timer); }, [active, task.id]);
  const latest = useMemo(() => buildActivities(events).slice(-1)[0], [events]);
  const started = [...events].reverse().find(item => item.type === "task.started" && (task.attempt == null || item.attempt == null || item.attempt === task.attempt));
  const ended = [...events].reverse().find(item => ["task.completed", "task.failed", "task.cancelled"].includes(item.type));
  const elapsed = started ? (active ? now : ended ? Date.parse(ended.timestamp) : NaN) - Date.parse(started.timestamp) : NaN;
  const title = task.status === "WAITING_APPROVAL" ? "等待你的批准" : task.status === "QUEUED" ? "任务已排队" : task.status === "VERIFYING" ? "正在验证修改" : task.status === "FAILED" ? "任务未完成" : task.status === "CANCELLED" ? "任务已停止" : task.status === "SUCCEEDED" ? "任务已完成" : latest?.pending ? latest.title : "正在处理任务";
  return <div className={`current-activity ${active ? "is-running" : ""}`} aria-label="当前任务状态"><button className="current-activity-main" type="button" onClick={onShow}>{active && task.status !== "WAITING_APPROVAL" ? <Loader2 size={13} className="activity-spin"/> : task.status === "SUCCEEDED" ? <Check size={13}/> : task.status === "FAILED" ? <AlertTriangle size={13}/> : task.status === "WAITING_APPROVAL" ? <Pause size={13}/> : <Square size={11}/>}<span>{title}</span>{Number.isFinite(elapsed) && elapsed >= 0 && <time>{activityDuration(elapsed)}</time>}<ChevronRight size={12}/></button>{active && <button type="button" className="current-activity-stop" onClick={onStop}><Square size={10}/>停止</button>}</div>;
}
