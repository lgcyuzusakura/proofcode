import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
const { chromium } = createRequire(import.meta.url)("playwright");
const browser = await chromium.launch({ channel: "msedge", headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
const page = await context.newPage(), errors = [], results = [];
page.on("pageerror", e => errors.push(e.message));
const project = { id: "plan-project", name: "ProofCode", defaultBranch: "main", sourceKind: "REMOTE_REPOSITORY" };
const workspace = { id: "plan-workspace", projectId: project.id, name: "开发工作区", defaultWorkspace: true };
const conversation = { id: "plan-conversation", projectId: project.id, workspaceId: workspace.id, title: "规划登录模块", defaultConversation: true };
const scope = { projectId: project.id, workspaceId: workspace.id, conversationId: conversation.id };
const tasks = [], messages = [], events = [], instructions = [], submitted = [], keys = [];
let failList = true, malformedList = false, loseResponse = true;
const at = new Date().toISOString();
const record = text => { results.push(text); console.log(`PASS ${text}`); };
const output = resolve(".proofcode/planning-qa"); await mkdir(output, { recursive: true });
await context.addInitScript(scope => { localStorage.setItem("proofcode.session", "fixture"); localStorage.setItem("proofcode.selected-scope", JSON.stringify(scope)); }, scope);
await page.route("**/api/**", async route => {
 const request = route.request(), url = new URL(request.url()), path = url.pathname;
 let body = {};
 if (path === "/api/auth/me") body = { id: "qa", username: "fixture" };
 else if (path === "/api/projects") body = [project];
 else if (path.endsWith("/default-scope")) body = scope;
 else if (path.endsWith("/workspaces")) body = [workspace];
 else if (path.endsWith("/conversations")) body = [conversation];
 else if (path.endsWith("/messages")) body = messages;
 else if (path.includes("/agent-platform/selection/")) body = { profileId: null };
 else if (path.includes("/agent-platform/")) body = [];
 else if (path.endsWith("/context")) body = { version: 0, view: { mode: "AUTO", items: [], packIds: [] }, messages: [], packs: [], excludedTurns: 0, estimatedTokens: 0 };
 else if (path === "/api/model-providers") body = [{ id: "provider", name: "本地夹具", models: [{ id: "fixture-model", name: "Fixture" }], modelSettings: {} }];
 else if (path === "/api/tasks" && request.method() === "POST") {
  const input = request.postDataJSON(); submitted.push(input);
  body = { ...scope, ...input, id: `task-${submitted.length}`, status: "RUNNING", attempt: 1, createdAt: at };
  tasks.unshift(body);
  messages.push({ id: `user-${body.id}`, taskId: body.id, attempt: 1, sequence: messages.length+1, role: "user", content: input.prompt, status: "COMPLETED", createdAt: at }, { id: `assistant-${body.id}`, taskId: body.id, attempt: 1, sequence: messages.length+2, role: "assistant", content: "", status: "PENDING", createdAt: at });
 } else if (path === "/api/tasks") body = tasks;
 else if (path.endsWith("/events")) body = events.filter(item => item.sequence > Number(url.searchParams.get("afterSequence") || 0));
 else if (path.endsWith("/instructions")) {
  if (request.method() === "GET") {
   if (failList) return route.fulfill({ status: 503, json: { message: "fixture list failure" } });
   body = malformedList ? { unexpected: "not a list" } : instructions;
  } else {
   const input = request.postDataJSON(); keys.push(input.clientKey); assert.equal(input.attempt, 1);
   let item = instructions.find(v => v.clientKey === input.clientKey);
   if (!item) { item = { ...input, id: `instruction-${instructions.length+1}`, sequence: instructions.length+1 }; instructions.push(item); }
   if (loseResponse) { loseResponse = false; return route.fulfill({ status: 503, json: { message: "response lost after store" } }); }
   body = item;
  }
 } else if (/\/(artifacts|approvals)$/.test(path)) body = [];
 return route.fulfill({ json: body });
});
const refresh = () => page.getByTitle("刷新项目与会话").click();
try {
 await page.goto(process.env.PROOFCODE_UI_URL || "http://127.0.0.1:5187");
 await page.getByRole("button", { name: "只读规划", exact: true }).click();
 await page.reload();
 assert((await page.getByRole("button", { name: "只读规划", exact: true }).getAttribute("class")).includes("active"));
 await page.getByRole("textbox", { name: "消息内容" }).fill("调查登录模块并制定测试方案");
 await page.getByRole("button", { name: "制定方案", exact: true }).click();
 await page.locator(".task-instructions").waitFor();
 assert.equal(submitted[0].executionMode, "PLAN"); record("PLAN mode survives reload and creates a real PLAN request");
 events.push({ sequence: 1, type: "plan.updated", timestamp: at, attempt: 1, payload: { version: 1, summary: "先调查再执行", steps: [{ id: "inspect", text: "核对登录与会话测试", status: "in_progress" }, { id: "change", text: "确认边界后修复", status: "pending" }] } }, { sequence: 2, type: "plan.updated", timestamp: at, attempt: 2, payload: { steps: [{ id: "wrong", text: "其他任务尝试的计划", status: "completed" }] } });
 await page.reload();
 await page.getByText("核对登录与会话测试", { exact: true }).waitFor();
 assert.equal(await page.getByText("其他任务尝试的计划", { exact: true }).count(), 0); record("durable plan reloads and ignores a different task attempt");
 await page.locator(".task-instructions > summary").click();
 await page.getByText("未能读取补充指令，请重试。", { exact: false }).waitFor();
 failList = false; malformedList = true;
 await page.locator(".task-instructions").getByRole("button", { name: "重试", exact: true }).click();
 await page.waitForTimeout(200);
 assert(await page.getByText("未能读取补充指令，请重试。", { exact: false }).isVisible());
 assert(await page.getByRole("textbox", { name: "消息内容" }).isVisible());
 assert.deepEqual(errors, []);
 malformedList = false;
 await page.locator(".task-instructions").getByRole("button", { name: "重试", exact: true }).click();
 await page.getByText("未能读取补充指令，请重试。", { exact: false }).waitFor({ state: "hidden" });
 const editor = page.getByRole("textbox", { name: "调整当前任务" });
 await editor.fill("只检查 auth，不修改数据库");
 await page.getByRole("button", { name: "发送补充", exact: true }).click();
 await page.getByText("补充指令未确认送达", { exact: false }).waitFor();
 assert.equal(await editor.inputValue(), "只检查 auth，不修改数据库");
 await page.getByRole("button", { name: "发送补充", exact: true }).click();
 await page.waitForFunction(() => document.querySelector('.instruction-editor textarea')?.value === "");
 assert.equal(keys.length, 2); assert.equal(keys[0], keys[1]); assert.equal(instructions.length, 1); record("load failure can retry; a lost send response preserves draft and reuses its idempotency key");
 instructions[0].deliveredAt = at;
 await page.getByText("已交付模型上下文", { exact: true }).waitFor(); record("queued instruction delivery is refreshed from the server");
 for (const [width, height] of [[1440,1000],[390,844],[760,480]]) {
  await page.setViewportSize({ width, height });
  const dimensions = await page.evaluate(() => { const stream=document.querySelector('.conversation-stream').getBoundingClientRect(), dock=document.querySelector('.composer-dock').getBoundingClientRect(); return { overflow: document.documentElement.scrollWidth-innerWidth, streamBottom: stream.bottom, dockTop: dock.top, dockBottom: dock.bottom, height: innerHeight }; });
  assert(dimensions.overflow <= 1 && dimensions.streamBottom <= dimensions.dockTop+1 && dimensions.dockBottom <= dimensions.height+1, JSON.stringify(dimensions));
  await page.screenshot({ path: resolve(output, `plan-${width}.png`) });
 }
 record("desktop, narrow and short viewports retain scrolling and a visible composer");
 tasks[0].status="SUCCEEDED"; tasks[0].result="核对登录与会话测试后，修复过期 session 的边界条件。";
 messages[1].content=tasks[0].result; messages[1].status="SUCCEEDED";
 instructions.push({ id:"never-delivered", sequence:2, content:"结束前追加但未交付" });
 await refresh();
 await page.getByRole("button", { name:"用此方案创建代码任务", exact:true }).waitFor();
 await page.getByText("任务已结束，未交付", { exact:true }).waitFor();
 assert.equal(await page.getByRole("textbox", { name:"调整当前任务" }).count(),0);
 await page.getByRole("textbox", { name:"消息内容" }).fill("保留我的草稿要求");
 await page.getByRole("button", { name:"用此方案创建代码任务", exact:true }).click();
 const draft=await page.getByRole("textbox", { name:"消息内容" }).inputValue();
 assert(draft.includes("保留我的草稿要求") && draft.includes(tasks[0].result) && draft.includes("方案来源：task-1"));
 assert.equal(submitted.length,1); assert.equal(await page.evaluate(() => localStorage.getItem("proofcode.execution-mode")),"CODE");
 record("finished task shows undelivered instructions; using its plan preserves draft and waits for user submission");
 await page.setViewportSize({width:1440,height:1000});
 await page.evaluate(() => document.documentElement.dataset.theme="dark");
 await page.screenshot({path:resolve(output,"plan-dark.png")});
 await page.getByRole("button", {name:"运行任务",exact:true}).click();
 await page.waitForFunction(() => document.querySelectorAll('.sidebar-task').length===2);
 assert.equal(submitted[1].executionMode,"CODE"); assert.equal(submitted[1].prompt,draft); record("explicit send creates a fresh CODE task from the selected plan");
 assert.deepEqual(errors,[]);
 await writeFile(resolve(output,"verification.json"),JSON.stringify({source:"local API fixtures, no model calls",results,errors},null,2));
} catch (error) {
 await page.screenshot({path:resolve(output,"failure.png")});
 console.error(await page.evaluate(() => ({ selects: [...document.querySelectorAll('select')].map(node => ({label:node.getAttribute('aria-label'),value:node.value,disabled:node.disabled})), alerts:[...document.querySelectorAll('[role="alert"],.agent-platform-error,.toast')].map(node=>node.textContent),text:document.body.innerText.slice(-4000) })));
 throw error;
} finally { await browser.close(); }
