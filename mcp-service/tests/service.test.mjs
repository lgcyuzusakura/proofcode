import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { request as httpRequest } from "node:http";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { promisify } from "node:util";
import { execFile } from "node:child_process";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import { loadConfig } from "../src/config.mjs";
import { createService } from "../src/service.mjs";
import { listenHTTP } from "../src/http.mjs";
import { scaffold } from "../src/init.mjs";

const packageRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const token = "fixture-http-token-012345678901234567890";
const plugin = `import { writeFile } from "node:fs/promises";
import { join } from "node:path";
export const tools = [
{name:"custom_note",description:"Save an approved project note",inputSchema:{type:"object",properties:{text:{type:"string",maxLength:100}},required:["text"],additionalProperties:false},execute:async({text},{root,signal})=>{await writeFile(join(root,"note.txt"),text,{signal});return {saved:true,text}}},
{name:"echo",description:"Echo arguments",inputSchema:{type:"object",properties:{text:{type:"string"}},required:["text"],additionalProperties:false},execute:({text})=>text},
{name:"fail",description:"Error without credential leakage",inputSchema:{type:"object"},execute:()=>{throw new Error("fixture-secret-value")}},
{name:"slow",description:"Cooperative cancellation",inputSchema:{type:"object"},execute:async(_, {signal,root})=>{await writeFile(join(root,"slow-started.txt"),"started",{signal});signal.throwIfAborted();return new Promise((_,reject)=>signal.addEventListener("abort",()=>reject(new Error("abort")),{once:true}));}},
{name:"uncooperative",description:"Keeps capacity occupied until done",inputSchema:{type:"object"},execute:()=>new Promise(resolve=>setTimeout(()=>resolve("finished"),600))}
];`;

async function fixture(t, changes = {}, text = plugin) {
  const directory = await mkdtemp(join(tmpdir(), "proofcode-custom-mcp-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  await writeFile(join(directory, "tools.mjs"), text);
  const input = { version: 1, name: "custom_tools", root: ".", modules: ["./tools.mjs"], timeoutMs: 15000, concurrency: 4, secretEnv: ["FIXTURE_SECRET"], transport: { type: "http", host: "127.0.0.1", port: 0, tokenEnv: "FIXTURE_TOKEN", allowedHosts: ["127.0.0.1"], allowedOrigins: [] }, ...changes };
  const configPath = join(directory, "config.local.json");
  await writeFile(configPath, JSON.stringify(input));
  const config = await loadConfig(configPath, { FIXTURE_TOKEN: token, FIXTURE_SECRET: "fixture-secret-value" });
  return { config, directory, configPath, input };
}
async function connectHTTP(t, config) {
  const service = await createService(config), endpoint = await listenHTTP(config, service);
  t.after(() => endpoint.close());
  const client = new Client({ name: "proofcode-test", version: "1" }, { capabilities: {} });
  await client.connect(new StreamableHTTPClientTransport(new URL(endpoint.url), { requestInit: { headers: { Authorization: `Bearer ${token}` } } }));
  t.after(() => client.close());
  return { client, endpoint };
}
const content = result => result.content.map(value => value.text || "").join("\n");

test("custom modules expose schemas and execute through actual HTTP transport", async t => {
  const { config, directory } = await fixture(t), { client } = await connectHTTP(t, config);
  const list = await client.listTools();
  assert.deepEqual(list.tools.map(value => value.name), ["custom_note", "echo", "fail", "slow", "uncooperative"]);
  assert.equal(list.tools[0].inputSchema.additionalProperties, false);
  const saved = await client.callTool({ name: "custom_note", arguments: { text: "actual local tool output" } });
  assert.equal(saved.isError, false); assert.equal(await readFile(join(directory, "note.txt"), "utf8"), "actual local tool output");
  const invalid = await client.callTool({ name: "custom_note", arguments: { text: 42, injected: true } });
  assert.equal(invalid.isError, true); assert.equal(await readFile(join(directory, "note.txt"), "utf8"), "actual local tool output");
  assert.equal((await client.callTool({ name: "missing", arguments: {} })).isError, true);
  const redacted = await client.callTool({ name: "echo", arguments: { text: `fixture-secret-value ${token}` } });
  assert.equal(content(redacted), "[redacted] [redacted]");
  assert(!content(await client.callTool({ name: "fail", arguments: {} })).includes("fixture-secret-value"));
});

test("HTTP listener rejects untrusted hosts, origins, missing credentials and oversized or malformed bodies", async t => {
  const { config } = await fixture(t), { endpoint } = await connectHTTP(t, config);
  const post = (body = "{}", headers = {}) => fetch(endpoint.url, { method: "POST", headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json", ...headers }, body });
  assert.equal((await post("{}", { Authorization: "Bearer wrong" })).status, 401);
  assert.equal((await post("{}", { Origin: "https://untrusted.example" })).status, 403);
  const withHost = host => new Promise((resolve, reject) => { const request = httpRequest(endpoint.url, { method: "POST", headers: { Host: host, Authorization: `Bearer ${token}`, "Content-Type": "application/json" } }, response => { response.resume(); response.once("end", () => resolve(response.statusCode)); }); request.once("error", reject); request.end("{}"); });
  assert.equal(await withHost("attacker.example"), 403);
  assert.equal(await withHost("evil@127.0.0.1"), 400);
  assert.equal((await post("not-json")).status, 400);
  assert.equal((await post("[]")).status, 400);
  assert.equal((await post("x".repeat(1024 * 1024 + 1))).status, 413);
  assert.equal((await fetch(endpoint.url, { headers: { Authorization: `Bearer ${token}` } })).status, 405);
});

test("timeout releases a cooperative tool, but unfinished work continues to occupy capacity", async t => {
  const { config } = await fixture(t, { timeoutMs: 100, concurrency: 1 }), { client } = await connectHTTP(t, config);
  assert.match(content(await client.callTool({ name: "slow", arguments: {} })), /cancelled or timed out/);
  assert.equal(content(await client.callTool({ name: "echo", arguments: { text: "slot available" } })), "slot available");
  assert.match(content(await client.callTool({ name: "uncooperative", arguments: {} })), /cancelled or timed out/);
  assert.match(content(await client.callTool({ name: "echo", arguments: { text: "blocked" } })), /busy/);
  await new Promise(resolve => setTimeout(resolve, 650));
  assert.equal(content(await client.callTool({ name: "echo", arguments: { text: "recovered" } })), "recovered");
});

test("HTTP sessions preserve cancellation across requests and reject missing or expired session references", async t => {
  const { config, directory } = await fixture(t, { timeoutMs: 5000, concurrency: 1 });
  const { client, endpoint } = await connectHTTP(t, config);
  const controller = new AbortController();
  const call = client.callTool({ name: "slow", arguments: {} }, undefined, { signal: controller.signal });
  const rejected = assert.rejects(call);
  for (let attempt = 0; attempt < 50; attempt++) {
    try { await readFile(join(directory, "slow-started.txt")); break; } catch { if (attempt === 49) assert.fail("tool did not begin"); await new Promise(resolve => setTimeout(resolve, 10)); }
  }
  controller.abort(); await rejected;
  let recovered = false;
  for (let attempt = 0; attempt < 50; attempt++) {
    const result = await client.callTool({ name: "echo", arguments: { text: "recovered after cancel" } });
    if (!result.isError) { recovered = true; break; }
    await new Promise(resolve => setTimeout(resolve, 10));
  }
  assert(recovered, "cancellation must reach the existing server and release the cooperative tool");
  const headers = { Authorization: `Bearer ${token}`, "Content-Type": "application/json", Accept: "application/json, text/event-stream" };
  const body = JSON.stringify({ jsonrpc: "2.0", id: 99, method: "tools/list", params: {} });
  assert.equal((await fetch(endpoint.url, { method: "POST", headers, body })).status, 400);
  assert.equal((await fetch(endpoint.url, { method: "POST", headers: { ...headers, "Mcp-Session-Id": "00000000-0000-0000-0000-000000000000" }, body })).status, 404);
  const transport = client.transport, session = transport.sessionId;
  assert.match(session, /^[a-f0-9-]{36}$/);
  await transport.terminateSession();
  assert.equal((await fetch(endpoint.url, { method: "POST", headers: { ...headers, "Mcp-Session-Id": session }, body })).status, 404);
});

test("stdio CLI discovers and calls the same custom tools without polluting protocol stdout", async t => {
  const { configPath, directory } = await fixture(t, { transport: { type: "stdio" } });
  const transport = new StdioClientTransport({ command: process.execPath, args: [join(packageRoot, "src/cli.mjs"), "--config", configPath], stderr: "pipe" });
  const client = new Client({ name: "proofcode-test", version: "1" }, { capabilities: {} });
  t.after(() => client.close());
  await client.connect(transport);
  assert.equal((await client.listTools()).tools.length, 5);
  await client.callTool({ name: "custom_note", arguments: { text: "stdio delivery" } });
  assert.equal(await readFile(join(directory, "note.txt"), "utf8"), "stdio delivery");
});

test("invalid configuration, duplicate tools and invalid input schemas fail before serving", async t => {
  const { config, configPath, input } = await fixture(t);
  await assert.rejects(loadConfig(configPath, {}), /token environment/);
  await writeFile(configPath, JSON.stringify({ ...input, transport: { ...input.transport, allowedHosts: ["*"] } }));
  await assert.rejects(loadConfig(configPath, { FIXTURE_TOKEN: token }), /host names/);
  const duplicate = await fixture(t, {}, `export const tools=[{name:"same",description:"One",inputSchema:{type:"object"},execute:()=>1},{name:"same",description:"Two",inputSchema:{type:"object"},execute:()=>2}];`);
  await assert.rejects(createService(duplicate.config), /duplicate/);
  const invalid = await fixture(t, {}, `export const tools=[{name:"bad",description:"Bad",inputSchema:{type:"object",properties:{bad:{type:"fictional"}}},execute:()=>1}];`);
  await assert.rejects(createService(invalid.config), /schema/);
  assert(config.secrets.includes(token));
});

test("bundled knowledge tool retrieves actual path and line evidence from the selected directory", async t => {
  const { config, directory } = await fixture(t, { modules: [join(packageRoot, "tools/knowledge.mjs")] });
  await writeFile(join(directory, "guide.md"), "Project decisions\nUse PostgreSQL for durable task state.\nKeep local paths private.");
  await mkdir(join(directory, "thesis")); await writeFile(join(directory, "thesis/private.md"), "PostgreSQL private manuscript");
  const { client } = await connectHTTP(t, config);
  const result = JSON.parse(content(await client.callTool({ name: "knowledge_search", arguments: { query: "PostgreSQL", limit: 3 } })));
  assert.equal(result.results.length, 1); assert.equal(result.results[0].path, "guide.md"); assert.equal(result.results[0].line, 2); assert.equal(result.complete, true);
  assert(!JSON.stringify(result).includes("private manuscript"));
});

test("scaffold creates an independent service with fresh credentials and refuses to overwrite it", async t => {
  const parent = await mkdtemp(join(tmpdir(), "proofcode-mcp-scaffold-"));
  t.after(() => rm(parent, { recursive: true, force: true }));
  const target = await scaffold(join(parent, "custom-service"), "http", "my_team");
  const input = JSON.parse(await readFile(join(target, "config.local.json"), "utf8"));
  assert.equal(input.name, "my_team"); assert.deepEqual(input.modules, ["./tools/knowledge.mjs", "./tools/custom.mjs"]);
  const env = await readFile(join(target, ".env"), "utf8"), key = env.trim().split("=")[1];
  assert.match(key, /^[a-f0-9]{64}$/); assert(!JSON.stringify(input).includes(key));
  const ignored = await readFile(join(target, ".gitignore"), "utf8");
  assert(ignored.includes(".env")); assert(ignored.includes("node_modules/"));
  await assert.rejects(scaffold(target), /already exists/);
  assert.equal(await readFile(join(target, ".env"), "utf8"), env);
  const other = await scaffold(join(parent, "stdio-service"), "stdio");
  assert.equal(JSON.parse(await readFile(join(other, "config.local.json"), "utf8")).transport.type, "stdio");
  assert.notEqual(await readFile(join(other, ".env"), "utf8"), env);
  assert(process.env.npm_execpath, "run this suite with npm test to verify standalone dependency installation");
  const execute = promisify(execFile);
  await execute(process.execPath, [process.env.npm_execpath, "ci", "--offline", "--no-audit", "--no-fund"], { cwd: target, timeout: 60000 });
  const checked = await execute(process.execPath, ["--env-file=.env", "src/cli.mjs", "--config", "config.local.json", "--check"], { cwd: target, timeout: 10000 });
  assert.equal(checked.stdout, "");
  const report = JSON.parse(checked.stderr);
  assert.equal(report.status, "valid"); assert.deepEqual(report.tools, ["knowledge_search", "project_brief"]);
  assert(!checked.stderr.includes(key));
});
