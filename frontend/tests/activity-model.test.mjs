import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";

const compile = source => ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const moduleUrl = source => `data:text/javascript;base64,${Buffer.from(source).toString("base64")}`;
const presentation = moduleUrl(compile(await readFile(new URL("../src/presentation.ts", import.meta.url), "utf8")));
const source = compile(await readFile(new URL("../src/activityModel.ts", import.meta.url), "utf8")).replace('"./presentation"', JSON.stringify(presentation));
const { buildActivities } = await import(moduleUrl(source));
const event = (sequence, type, payload = {}, extras = {}) => ({ sequence, type, payload, timestamp: `2026-10-15T00:00:${String(sequence).padStart(2, "0")}Z`, attempt: 1, runId: "run-1", ...extras });

test("tool milestones merge their own input, report measured duration and ignore duplicate transport delivery", () => {
  const request = event(1, "tool.requested", { tool: "read_file", callId: "same", arguments: { path: "main.go" } });
  const events = [event(3, "tool.completed", { tool: "read_file", callId: "same", result: "file contents" }), request, event(2, "tool.started", { tool: "read_file", callId: "same" }), request];
  const rows = buildActivities(events);
  assert.equal(rows.length, 2);
  assert.equal(rows[1].durationMs, 1000);
  assert.deepEqual(rows[1].input, { path: "main.go" });
  assert.equal(rows[1].output, "file contents");
  assert.equal(rows[1].tone, "success");
});
test("reused call IDs across attempts and runs never borrow inputs or timing", () => {
  const rows = buildActivities([event(1, "tool.requested", { tool: "read_file", callId: "same", arguments: "old-secret-input" }), event(2, "tool.started", { tool: "read_file", callId: "same" }), event(3, "tool.completed", { tool: "read_file", callId: "same", result: "new" }, { attempt: 2, runId: "run-2" })]);
  assert.equal(rows[1].input, undefined);
  assert.equal(rows[1].durationMs, undefined);
});
test("approval, cancellation, failed validation and unstructured reports never claim success", () => {
  const rows = buildActivities([event(1, "tool.approval_required", { tool: "apply_patch", callId: "write" }), event(2, "verification.completed", { passed: false, output: "failed test" }), event(3, "verification.completed", { report: "Model review: inspect manually" }), event(4, "task.cancelled", { reason: "user cancelled" })]);
  assert.deepEqual(rows.map(row => row.tone), ["warning", "error", "observe", "muted"]);
  assert(rows[1].title.includes("未通过"));
  assert.equal(rows[2].title, "验证结果已返回");
  assert.equal(rows[3].title, "任务已停止");
});
test("usage and deltas stay out of the timeline; child tools and compression errors remain explicit", () => {
  const rows = buildActivities([event(1, "usage.updated"), event(2, "agent.message.delta", { delta: "hi" }), event(3, "agent.delegated", { agentId: "child", kind: "started", payload: { name: "调查登录模块" } }), event(4, "agent.delegated", { agentId: "child", kind: "tool.completed", payload: { tool: "search_code", result: "matched" } }), event(5, "context.selected", { kind: "compression", error: "compression unavailable" })]);
  assert.equal(rows.length, 3);
  assert.equal(rows[1].agent, "调查登录模块");
  assert.equal(rows[1].output, "matched");
  assert.equal(rows[2].tone, "warning");
});
