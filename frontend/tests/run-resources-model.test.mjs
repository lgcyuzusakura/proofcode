import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";

const source = await readFile(new URL("../src/run-resources-model.ts", import.meta.url), "utf8");
const javascript = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText.replace('from "./types"', "");
const model = await import(`data:text/javascript;base64,${Buffer.from(javascript).toString("base64")}`);
const event = (sequence, type, payload = {}, attempt = 1, runId = "run-a") => ({ sequence, type, payload, attempt, runId, timestamp: "2026-10-22T00:00:00Z" });
const delegated = (sequence, agentId, kind, payload = {}, attempt = 1, runId = "run-a") => event(sequence, "agent.delegated", { agentId, kind, payload }, attempt, runId);

test("resource selection deduplicates replayed events and scopes attempts", () => {
  const first = event(1, "usage.updated", { requestInputTokens: 5 });
  const events = [event(3, "usage.updated", { requestInputTokens: 30 }, 2, "run-b"), first, first, event(0, "usage.updated"), event(2, "usage.updated", { requestInputTokens: 10 })];
  assert.deepEqual(model.selectedResourceEvents(events).map(e => e.sequence), [3]);
  assert.deepEqual(model.selectedResourceEvents(events, 1).map(e => e.sequence), [1, 2]);
  assert.deepEqual(model.selectedResourceEvents(events, 3), []);
  assert.deepEqual(model.selectedResourceEvents(events, 2).map(e => e.sequence), [3]);
  assert.deepEqual(model.selectedResourceEvents([event(1, "usage.updated", {})]).map(e => e.attempt ?? 1), [1]);
});

test("current run isolates timing while attempt totals retain previous paid requests", () => {
  const events = [event(1, "task.started", {}, 1, "run-a"), event(2, "usage.updated", { requestInputTokens: 10 }, 1, "run-a"), event(3, "task.started", {}, 1, "run-b"), event(4, "usage.updated", { requestInputTokens: 20 }, 1, "run-b")];
  assert.deepEqual(model.currentResourceRun(events).map(e => e.sequence), [3, 4]);
  assert.deepEqual(model.requestUsageEvents(model.selectedResourceEvents(events)).map(e => e.sequence), [2, 4]);
});

test("child request usage contributes once; cumulative completion usage is not added again", () => {
  const events = [
    event(1, "usage.updated", { requestUsageReported: true, requestInputTokens: 100, requestOutputTokens: 20, requestEstimatedCost: 0.01, costCurrency: "CNY" }),
    delegated(2, "child-1", "queued", { name: "检索" }),
    delegated(3, "child-1", "started", { name: "检索" }),
    delegated(4, "child-1", "usage.updated", { requestUsageReported: true, requestInputTokens: 40, requestOutputTokens: 10, requestEstimatedCost: 0.02, costCurrency: "CNY" }),
    delegated(5, "child-1", "completed", { status: "completed", usage: { inputTokens: 40, outputTokens: 10, reported: true } })
  ];
  const requests = model.requestUsageEvents(events);
  assert.equal(model.resourceCostSummary(requests), "0.030000 CNY");
  assert.equal(model.resourceTokenSummary(requests), "170");
  const child = model.resourceChildren(events)[0];
  assert.equal(child.status, "已完成");
  assert.equal(child.costs.length, 1);
  assert.equal(child.usage.inputTokens, 40);
});

test("unknown, invalid and mixed-currency requests remain visibly partial", () => {
  const requests = [
    event(1, "usage.updated", { requestUsageReported: true, requestInputTokens: 8, requestOutputTokens: 2, requestEstimatedCost: 0, costCurrency: "CNY" }),
    event(2, "usage.updated", { requestUsageReported: false, requestInputTokens: 0, requestOutputTokens: 0, requestEstimatedCost: -1, costCurrency: "CNY" }),
    event(3, "usage.updated", { requestUsageReported: true, requestInputTokens: 4, requestOutputTokens: 1, requestEstimatedCost: 0.5, costCurrency: "USD" }),
    event(4, "usage.updated", { requestEstimatedCost: Number.NaN, costCurrency: "CNY" })
  ];
  assert.equal(model.resourceCostSummary(requests), "多币种（见详情）");
  assert.equal(model.resourceTokenSummary(requests), "15（部分）");
  assert.equal(model.resourceCostSummary([requests[0]]), "0.000000 CNY");
  assert.equal(model.resourceCostSummary([]), "未估算");
  assert.equal(model.resourceTokenSummary([]), "未报告");
});

test("child lifecycle remains terminal after late replay and missing usage stays unknown", () => {
  const events = [
    delegated(1, "child-2", "queued", { name: "测试" }),
    delegated(2, "child-2", "completed", { status: "cancelled" }),
    delegated(3, "child-2", "started", { name: "旧事件" }),
    delegated(4, "child-2", "completed", { status: "completed" })
  ];
  const [child] = model.resourceChildren(events);
  assert.equal(child.status, "已取消");
  assert.equal(child.name, "测试");
  assert.equal(child.usage.reported, false);
});
