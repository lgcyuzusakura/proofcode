import type { TaskEvent } from "./types";

export type StreamingReview = { text: string; step: number; runId?: string; attempt?: number; sequence: number };

export function latestStreamingReview(events: TaskEvent[], attempt?: number): StreamingReview | null {
  // Events can arrive through both polling and the socket. A transport replay is
  // one persisted event, not another token. Sequence numbers are task-global.
  const ordered = [...new Map(events.map(event => [event.sequence, event])).values()]
    .filter(event => Number.isSafeInteger(event.sequence) && event.sequence > 0)
    .sort((a, b) => a.sequence - b.sequence);
  const currentAttempt = attempt ?? ordered.reduce((latest, event) => Math.max(latest, event.attempt ?? 1), 1);
  const scoped = ordered.filter(event => (event.attempt ?? 1) === currentAttempt);
  // A restarted runner may reuse model step numbers within the same attempt.
  // Prefer its explicit start over a late event from the superseded runner.
  const started = [...scoped].reverse().find(event => event.type === "task.started");
  const runId = started?.runId ?? [...scoped].reverse().find(event => event.runId != null)?.runId;
  const current = scoped.filter(event => event.runId === runId && (!started || event.sequence >= started.sequence));
  const chunks = new Map<string, StreamingReview>();
  const completedSteps = new Set<string>();
  for (const event of current) {
    if (event.type === "task.completed") { chunks.clear(); break; }
    if (["task.failed", "task.cancelled"].includes(event.type)) break;
    if (event.type === "agent.message.completed") {
      const step = event.payload?.step;
      // Older producers omit step on the final response.
      if (step == null) { chunks.clear(); break; }
      completedSteps.add(String(step));
      chunks.delete(String(step));
      continue;
    }
    if (event.type !== "agent.message.delta") continue;
    const delta = event.payload?.delta;
    if (typeof delta !== "string" || !delta) continue;
    const step = event.payload?.step;
    if (typeof step !== "number") continue;
    if (!Number.isSafeInteger(step) || step < 1) continue;
    const key = String(step);
    if (completedSteps.has(key)) continue;
    const current = chunks.get(key) ?? { text: "", step, runId: event.runId, attempt: event.attempt, sequence: event.sequence };
    current.text += delta;
    current.sequence = event.sequence;
    chunks.set(key, current);
  }
  return [...chunks.values()].sort((a, b) => b.sequence - a.sequence)[0] ?? null;
}
