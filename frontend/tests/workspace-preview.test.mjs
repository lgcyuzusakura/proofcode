import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";

const source = await readFile(new URL("../src/workspace-preview.ts", import.meta.url), "utf8");
const javascript = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText.replace('from "./types"', "");
const { latestWorkspacePreview } = await import(`data:text/javascript;base64,${Buffer.from(javascript).toString("base64")}`);
const event = (sequence, type, payload, attempt = 1, runId = "run-a") => ({ sequence, type, payload, attempt, runId, timestamp: `2026-10-22T00:00:${String(sequence).padStart(2, "0")}Z` });
const patch = "diff --git a/a.txt b/a.txt\n+new\n";
const ready = (sequence, value = patch, runId = "run-a") => event(sequence, "workspace.updated", { state: "ready", patch: value, sha256: "a".repeat(64) }, 1, runId);

test("uses the latest valid preview in the current runner and attempt", () => {
  const events = [event(1, "task.started", {}, 1), ready(2), ready(3, "", "run-a"), ready(4, "old", "run-old"), ready(5, "other attempt", "run-b")];
  assert.equal(latestWorkspacePreview(events, 1)?.patch, "");
  assert.equal(latestWorkspacePreview(events, 2), null);
});

test("deduplicates transport replay and never revives stale data after an invalid update", () => {
  const events = [ready(1), ready(1), event(2, "workspace.updated", { state: "unavailable" })];
  assert.equal(latestWorkspacePreview(events)?.state, "unavailable");
  assert.equal(latestWorkspacePreview([ready(1), event(2, "workspace.updated", { state: "ready", patch: "x", sha256: "bad" })])?.state, "unavailable");
});

test("rejects oversized previews and stops at the current run terminal event", () => {
  assert.equal(latestWorkspacePreview([ready(1, "x".repeat(128 * 1024 + 1))])?.state, "unavailable");
  assert.equal(latestWorkspacePreview([event(1, "task.started", {}), event(2, "task.completed", {}), ready(3)]), null);
  assert.equal(latestWorkspacePreview([event(1, "workspace.updated", { state: "oversized" })])?.state, "oversized");
});
