import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";

const source = await readFile(new URL("../src/streaming-review.ts", import.meta.url), "utf8");
const javascript = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText.replace('from "./types"', "");
const { latestStreamingReview } = await import(`data:text/javascript;base64,${Buffer.from(javascript).toString("base64")}`);
const event = (sequence, type, payload, attempt = 1, runId = "run-a") => ({ sequence, type, payload, attempt, runId, timestamp: "2026-10-21T00:00:00Z" });

test("reassembles interleaved model steps and returns the newest live response", () => {
  const result = latestStreamingReview([
    event(5, "agent.message.delta", { step: 2, delta: "new" }),
    event(2, "agent.message.delta", { step: 1, delta: "older " }),
    event(3, "agent.message.delta", { step: 2, delta: "stream " }),
    event(4, "agent.message.delta", { step: 1, delta: " done" })
  ]);
  assert.deepEqual(result, { text: "stream new", step: 2, runId: "run-a", attempt: 1, sequence: 5 });
});

test("completed responses, invalid deltas and older sequence events are not shown as live", () => {
  assert.equal(latestStreamingReview([
    event(1, "agent.message.delta", { step: 1, delta: "complete" }),
    event(2, "agent.message.completed", { content: "complete" }),
    event(3, "agent.message.delta", { step: 0, delta: "bad" })
  ]), null);
  assert.equal(latestStreamingReview([
    event(4, "agent.message.delta", { step: 1, delta: "new attempt" }, 2, "run-b"),
    event(3, "agent.message.delta", { step: 1, delta: "old attempt" }, 1, "run-a")
  ]).text, "new attempt");
});

test("polling and socket replays do not duplicate streamed text", () => {
  const first = event(1, "agent.message.delta", { step: 1, delta: "one " });
  const second = event(2, "agent.message.delta", { step: 1, delta: "two" });
  assert.equal(latestStreamingReview([second, first, first, second]).text, "one two");
});

test("the selected attempt excludes old completions and cannot fall back while queued", () => {
  const events = [
    event(1, "agent.message.delta", { step: 1, delta: "previous" }),
    event(2, "agent.message.delta", { step: 1, delta: "current" }, 2, "run-b"),
    event(3, "agent.message.completed", { content: "late old completion" })
  ];
  assert.equal(latestStreamingReview(events, 2).text, "current");
  assert.equal(latestStreamingReview(events, 3), null);
});

test("a new runner start closes the previous stream even before any new text", () => {
  const events = [
    event(1, "task.started", {}),
    event(2, "agent.message.delta", { step: 1, delta: "abandoned" }),
    event(3, "task.started", {}, 1, "run-b")
  ];
  assert.equal(latestStreamingReview(events), null);
  events.push(event(4, "agent.message.delta", { step: 1, delta: "fresh" }, 1, "run-b"));
  events.push(event(5, "agent.message.completed", { content: "late" }));
  assert.equal(latestStreamingReview(events).text, "fresh");
});

test("completion only closes its own step and blocks late tokens for that response", () => {
  const events = [
    event(1, "agent.message.delta", { step: 1, delta: "finished" }),
    event(2, "agent.message.delta", { step: 2, delta: "still live" }),
    event(3, "agent.message.completed", { step: 1, content: "finished" }),
    event(4, "agent.message.delta", { step: 1, delta: "late" })
  ];
  assert.equal(latestStreamingReview(events).text, "still live");
});

test("success closes text, while failure and cancellation preserve only received partial text", () => {
  for (const terminal of ["task.completed", "task.failed", "task.cancelled"]) {
    const result = latestStreamingReview([
      event(1, "agent.message.delta", { step: 1, delta: "partial" }),
      event(2, terminal, {}),
      event(3, "agent.message.delta", { step: 1, delta: "impossible late text" })
    ]);
    assert.equal(result?.text ?? null, terminal === "task.completed" ? null : "partial");
  }
});

test("legacy events stay usable without mixing missing identities into modern runs", () => {
  const legacy = { ...event(1, "agent.message.delta", { step: 1, delta: "legacy" }), attempt: undefined, runId: undefined };
  assert.equal(latestStreamingReview([legacy]).text, "legacy");
  assert.equal(latestStreamingReview([legacy], 2), null);
  assert.equal(latestStreamingReview([legacy, event(2, "agent.message.delta", { step: 1, delta: "modern" })]).text, "modern");
  assert.equal(latestStreamingReview([event(1, "agent.message.delta", { step: "1", delta: "invalid" })]), null);
});
