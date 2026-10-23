import type { TaskEvent } from "./types";

export type WorkspacePreview = { state: "ready" | "oversized" | "unavailable"; patch?: string; timestamp: string; sequence: number };
const limit = 128 * 1024;

export function latestWorkspacePreview(events: TaskEvent[], attempt?: number): WorkspacePreview | null {
  const ordered = [...new Map(events.map(event => [event.sequence, event])).values()]
    .filter(event => Number.isSafeInteger(event.sequence) && event.sequence > 0)
    .sort((a, b) => a.sequence - b.sequence);
  const selectedAttempt = attempt ?? ordered.reduce((latest, event) => Math.max(latest, event.attempt ?? 1), 1);
  const scoped = ordered.filter(event => (event.attempt ?? 1) === selectedAttempt);
  const start = [...scoped].reverse().find(event => event.type === "task.started");
  const runId = start?.runId ?? [...scoped].reverse().find(event => event.runId != null)?.runId;
  let preview: WorkspacePreview | null = null;
  for (const event of scoped) {
    if (event.runId !== runId || (start && event.sequence < start.sequence)) continue;
    if (["task.completed", "task.failed", "task.cancelled"].includes(event.type)) break;
    if (event.type !== "workspace.updated") continue;
    const payload = event.payload || {};
    const result = { timestamp: event.timestamp, sequence: event.sequence };
    if (payload.state === "ready" && typeof payload.patch === "string" &&
        typeof payload.sha256 === "string" && /^[a-f0-9]{64}$/.test(payload.sha256) &&
        new TextEncoder().encode(payload.patch).byteLength <= limit) {
      preview = { ...result, state: "ready", patch: payload.patch };
    } else {
      preview = { ...result, state: payload.state === "oversized" ? "oversized" : "unavailable" };
    }
  }
  return preview;
}
