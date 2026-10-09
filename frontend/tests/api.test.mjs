import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";

const source = await readFile(new URL("../src/api.ts", import.meta.url), "utf8");
const javascript = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const { api, createScratchProject, pendingBootstrap } = await import(`data:text/javascript;base64,${Buffer.from(javascript).toString("base64")}`);
const saved = new Map();
globalThis.localStorage = { getItem: (key) => saved.get(key) ?? null, setItem: (key, value) => saved.set(key, value), removeItem: (key) => saved.delete(key) };
const bridge = (app = {}) => { saved.clear(); globalThis.window = { go: { main: { App: app } } }; };

test("native scratch chat creates a folder then registers only an opaque handle", async () => {
  let localCall, request;
  bridge({
    BootstrapScratchProject: async (bootstrapId, title) => { localCall = { bootstrapId, title }; return { bootstrapId, name: title, localHandle: "opaque-handle", path: "C:/Desktop/private-project" }; },
    ProxyRequest: async (path, method, body, authorization, key) => { request = { path, method, body: JSON.parse(body), authorization, key }; return { status: 200, body: JSON.stringify({ id: "project", name: "scratch", sourceKind: "LOCAL_FOLDER" }) }; }
  });
  assert.equal((await createScratchProject("scratch")).id, "project");
  assert.equal(request.body.sourceKind, "LOCAL_FOLDER");
  assert.equal(request.body.localHandle, "opaque-handle");
  assert.equal(request.body.bootstrapId, localCall.bootstrapId);
  assert.equal(request.key, localCall.bootstrapId);
  assert.equal(Object.hasOwn(request.body, "path"), false);
  assert.equal(pendingBootstrap(), null);
});

test("failed registration preserves bootstrap identity across retry", async () => {
  const calls = [];
  let fail = true;
  bridge({
    BootstrapScratchProject: async (bootstrapId, title) => { calls.push({ bootstrapId, title }); return { bootstrapId, name: title, localHandle: "opaque", path: "C:/Desktop/project" }; },
    ProxyRequest: async () => fail ? { status: 503, body: "offline" } : { status: 200, body: '{"id":"project"}' }
  });
  await assert.rejects(createScratchProject("first title"));
  const pending = pendingBootstrap();
  assert.equal(pending.title, "first title");
  fail = false;
  await createScratchProject("different title");
  assert.deepEqual(calls[0], calls[1]);
  assert.equal(pendingBootstrap(), null);
});

test("Web scratch registration works without importing a folder", async () => {
  let request;
  bridge();
  globalThis.fetch = async (path, init) => { request = { path, body: JSON.parse(init.body), headers: init.headers }; return { ok: true, status: 200, json: async () => ({ id: "web-project", sourceKind: "SCRATCH" }) }; };
  assert.equal((await createScratchProject("web scratch")).sourceKind, "SCRATCH");
  assert.equal(request.body.localHandle, null);
  assert.equal(request.headers.get("Idempotency-Key"), request.body.bootstrapId);
  assert.equal(request.body.sourceKind, "SCRATCH");
});

test("native API forwards PATCH, authorization and idempotency headers", async () => {
  let request;
  bridge({ ProxyRequest: async (...args) => { request = args; return { status: 204, body: "" }; } });
  saved.set("proofcode.token", "test-token");
  assert.equal(await api("/api/projects/p/data/resources/r", { method: "PATCH", body: '{"expectedVersion":2}', headers: { "Idempotency-Key": "retry-key" } }), undefined);
  assert.deepEqual(request, ["/api/projects/p/data/resources/r", "PATCH", '{"expectedVersion":2}', "Bearer test-token", "retry-key"]);
});
