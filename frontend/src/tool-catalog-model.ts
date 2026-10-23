import type { TaskEvent } from "./types";

export type CatalogCall = { id: string; state: string; risk?: string; input?: unknown; output?: unknown; durationMs?: number };
export type CatalogTool = { name: string; description: string; parameters?: unknown; source: "builtin" | "mcp"; calls: CatalogCall[] };
export function buildToolCatalog(events: TaskEvent[], attempt?: number) {
  const ordered = [...new Map(events.map(event => [`${event.attempt}:${event.runId}:${event.sequence}`, event])).values()].sort((a, b) => a.sequence - b.sequence);
  const started = [...ordered].reverse().find(event => event.type === "task.started" && (attempt == null || event.attempt == null || event.attempt === attempt));
  const tools: CatalogTool[] = [];
  if (!started || !Array.isArray(started.payload.tools)) return { tools, readOnly: false, reported: false };
  const names = new Set<string>();
  for (const value of started.payload.tools) {
    if (!value || typeof value !== "object" || typeof value.name !== "string" || !value.name || names.has(value.name)) continue;
    names.add(value.name);
    tools.push({ name: value.name, description: typeof value.description === "string" ? value.description : "", parameters: value.parameters, source: value.name.startsWith("mcp__") ? "mcp" : "builtin", calls: [] });
  }
  const catalog = new Map(tools.map(tool => [tool.name, tool]));
  const calls = new Map<string, { call: CatalogCall; start?: number }>();
  for (const event of ordered) {
    if (event.sequence < started.sequence || (started.attempt != null && event.attempt != null && event.attempt !== started.attempt) || (started.runId != null && event.runId != null && event.runId !== started.runId)) continue;
    const definition = catalog.get(String(event.payload.tool));
    if (!definition || !["tool.requested", "tool.approval_required", "tool.started", "tool.completed", "tool.failed"].includes(event.type)) continue;
    const id = typeof event.payload.callId === "string" ? event.payload.callId : `event-${event.sequence}`;
    const key = `${definition.name}:${id}`;
    let entry = calls.get(key);
    if (!entry) { entry = { call: { id, state: "requested" } }; calls.set(key, entry); definition.calls.push(entry.call); }
    entry.call.state = event.type.slice(5);
    if (["read", "write", "exec"].includes(String(event.payload.risk))) entry.call.risk = String(event.payload.risk);
    if (event.payload.arguments != null) entry.call.input = event.payload.arguments;
    if (event.type === "tool.started") entry.start = Date.parse(event.timestamp);
    if (event.type === "tool.completed" || event.type === "tool.failed") {
      entry.call.output = event.payload.error ?? event.payload.result;
      const elapsed = Date.parse(event.timestamp) - (entry.start ?? NaN);
      if (Number.isFinite(elapsed) && elapsed >= 0) entry.call.durationMs = elapsed;
    }
  }
  return { tools, readOnly: started.payload.readOnly === true, reported: true };
}
