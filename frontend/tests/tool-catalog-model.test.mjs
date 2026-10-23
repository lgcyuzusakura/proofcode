import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";
const source = await readFile(new URL("../src/tool-catalog-model.ts", import.meta.url), "utf8");
const javascript = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText;
const { buildToolCatalog } = await import(`data:text/javascript;base64,${Buffer.from(javascript).toString("base64")}`);
const event = (sequence, type, payload, extra = {}) => ({ sequence, type, payload, attempt: 2, runId: "current", timestamp: new Date(1000 + sequence * 100).toISOString(), ...extra });
const definitions = [{ name: "read_file", description: "Read source", parameters: { type: "object" } }, { name: "mcp__docs__find", description: "Search docs" }];
test("catalog uses the current run and never merges stale attempt capabilities", () => {
  const values = [event(1, "task.started", { tools: [{ name: "old-secret-tool" }] }, { attempt: 1, runId: "old" }), event(2, "tool.requested", { tool: "read_file", callId: "old" }), event(3, "task.started", { tools: definitions, readOnly: true }), event(4, "tool.requested", { tool: "read_file", callId: "other-run" }, { runId: "other" }), event(5, "tool.requested", { tool: "not-registered", callId: "phantom" })];
  const result = buildToolCatalog(values.reverse(), 2);
  assert.deepEqual(result.tools.map(tool => tool.name), ["read_file", "mcp__docs__find"]);
  assert(result.readOnly && result.reported);
  assert(result.tools.every(tool => !tool.calls.length));
  assert.equal(buildToolCatalog(values, 3).reported, false);
});
test("catalog deduplicates events and resumed requests, retains actual risk, failure and elapsed time", () => {
  const values = [event(1, "task.started", { tools: definitions }), event(2, "tool.requested", { tool: "mcp__docs__find", callId: "one", risk: "exec", arguments: { query: "API" } }), event(3, "tool.approval_required", { tool: "mcp__docs__find", callId: "one" }), event(4, "tool.requested", { tool: "mcp__docs__find", callId: "one", resumed: true }), event(5, "tool.started", { tool: "mcp__docs__find", callId: "one" }), event(6, "tool.failed", { tool: "mcp__docs__find", callId: "one", result: "connection timed out" })];
  const result = buildToolCatalog([...values, values[2]]);
  const external = result.tools[1];
  assert.equal(external.source, "mcp");
  assert.equal(external.calls.length, 1);
  assert.deepEqual(external.calls[0], { id: "one", state: "failed", risk: "exec", input: { query: "API" }, output: "connection timed out", durationMs: 100 });
  assert.equal(result.tools[0].source, "builtin");
});
test("unknown catalog and unreported risk remain unknown while empty catalogs are reported", () => {
  assert.equal(buildToolCatalog([event(1, "task.started", {})]).reported, false);
  assert.equal(buildToolCatalog([event(1, "task.started", { tools: [] })]).reported, true);
  const result = buildToolCatalog([event(1, "task.started", { tools: [...definitions, null, { name: "read_file" }] }), event(2, "tool.completed", { tool: "read_file", callId: "missing-request", result: "read" })]);
  assert.equal(result.tools.length, 2);
  assert.equal(result.tools[0].calls[0].risk, undefined);
  assert.equal(result.tools[0].calls[0].durationMs, undefined);
});
