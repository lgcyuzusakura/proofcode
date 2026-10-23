import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir } from "node:fs/promises";
import { resolve } from "node:path";

// Run with NODE_PATH pointing at an installed Playwright package. All API
// calls are local fixtures; this check cannot start a model/runner task.
const { chromium } = createRequire(import.meta.url)("playwright");
const browser = await chromium.launch({ channel: "msedge", headless: true, ignoreDefaultArgs: ["--hide-scrollbars"] });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, permissions: ["clipboard-read", "clipboard-write"] });
const page = await context.newPage();
const errors = [];
page.on("pageerror", error => errors.push(error.message));
const origin = process.env.PROOFCODE_UI_URL || "http://127.0.0.1:5177";
const timestamp = "2026-10-12T07:00:00Z";
const project = { id: "project-qa", name: "proofcode", defaultBranch: "main", sourceKind: "REMOTE_REPOSITORY" };
const workspace = { id: "workspace-qa", projectId: project.id, name: "开发工作区", kind: "REMOTE_REPOSITORY", defaultWorkspace: true };
const conversation = { id: "chat-qa", projectId: project.id, workspaceId: workspace.id, title: "修复聊天区滚动", defaultConversation: true };
const secondChat = { ...conversation, id: "chat-other", title: "另一段对话", defaultConversation: false };
const scope = { projectId: project.id, workspaceId: workspace.id, conversationId: conversation.id };
const task = { id: "task-qa", ...scope, prompt: "检查对话滚动与底部输入区", model: "local-fixture", executionMode: "CODE", status: "SUCCEEDED", createdAt: timestamp };
let messages = Array.from({ length: 44 }, (_, index) => ({ id: `message-${index}`, sequence: index + 1, taskId: task.id, role: index % 2 ? "assistant" : "user", content: index % 2 ? `第 ${index + 1} 条回复。\n\n已检查消息容器，长回复应当能够完整阅读。\n\n\`\`\`go\npackage main\n\nfunc main() {\n    println("Hello, World!")\n}\n\`\`\`\n\n验证结果：运行成功。` : `第 ${index + 1} 条请求：检查界面与运行记录。`, status: "COMPLETED", createdAt: timestamp }));
let events = [];
let submitted = null;
let provider = { id: "provider-qa", name: "本地验收服务", baseUrl: "http://127.0.0.1:1/v1", hasKey: true, wireApi: "responses", models: [{ id: "local-fixture", name: "本地验收模型" }], aliases: {}, modelSettings: {}, version: 1 };
let savedProvider = null;
await context.addInitScript(({ scope }) => {
  localStorage.setItem("proofcode.session", "local-test-fixture");
  localStorage.setItem("proofcode.selected-scope", JSON.stringify(scope));
}, { scope });
await page.route("**/api/**", async route => {
  const url = new URL(route.request().url());
  const path = url.pathname;
  let body;
  if (path === "/api/auth/me") body = { id: "qa", username: "local-qa" };
  else if (path === "/api/projects") body = [project];
  else if (path.endsWith("/default-scope")) body = scope;
  else if (path.endsWith("/workspaces")) body = [workspace];
  else if (path.endsWith("/conversations")) body = [conversation, secondChat];
  else if (path.endsWith("/messages")) body = path.includes(secondChat.id) ? [] : messages;
  else if (path.includes("/agent-platform/selection/")) body = { profileId: null };
  else if (path.includes("/agent-platform/")) body = [];
  else if (path.endsWith("/context")) body = { version: 0, view: { mode: "AUTO", items: [], packIds: [] }, messages: [], packs: [], excludedTurns: 0, estimatedTokens: 0, selectionHash: "fixture", estimateMethod: "fixture" };
  else if (path === "/api/model-providers") body = [provider];
  else if (path === "/api/model-providers/provider-qa" && route.request().method() === "PATCH") { savedProvider = route.request().postDataJSON(); provider = { ...provider, ...savedProvider, hasKey: true }; body = provider; }
  else if (path === "/api/tasks" && route.request().method() === "POST") { submitted = route.request().postDataJSON(); body = { ...task, id: "submitted-task", prompt: submitted.prompt }; }
  else if (path === "/api/tasks") body = url.searchParams.get("conversationId") === secondChat.id ? [] : [task];
  else if (path.endsWith("/events")) body = events.filter(value => value.sequence > Number(url.searchParams.get("afterSequence") || 0));
  else if (path.endsWith("/artifacts") || path.endsWith("/approvals") || path.endsWith("/instructions")) body = [];
  else if (path.includes("/source-snapshots")) body = { id: "snapshot-qa" };
  else body = {};
  await route.fulfill({ json: body });
});
const results = [];
const output = resolve(process.env.PROOFCODE_UI_EVIDENCE || "../.proofcode/workbench-qa");
await mkdir(output, { recursive: true });
const record = label => { results.push(label); console.log(`PASS ${label}`); };
const metrics = () => page.locator(".conversation-stream").evaluate(node => {
  const stream = node.getBoundingClientRect(), composer = document.querySelector(".composer-dock").getBoundingClientRect();
  return { top: node.scrollTop, max: node.scrollHeight - node.clientHeight, height: node.clientHeight, streamBottom: stream.bottom, composerTop: composer.top, composerBottom: composer.bottom, viewportHeight: innerHeight, documentWidth: document.documentElement.scrollWidth, viewportWidth: innerWidth, overflow: getComputedStyle(node).overflowY };
});
try {
  await page.goto(origin);
  await page.locator(".message-row").nth(43).waitFor();
  await page.waitForTimeout(200);
  let geometry = await metrics();
  assert(geometry.max > 1000 && geometry.height > 250);
  assert.equal(geometry.overflow, "scroll");
  assert(geometry.streamBottom <= geometry.composerTop + 1);
  assert(geometry.composerBottom <= geometry.viewportHeight);
  assert(geometry.max - geometry.top < 2);
  record("long history scrolls and composer never overlaps messages");

  await page.getByRole("button", { name: "滚动到最早消息", exact: true }).click();
  assert((await metrics()).top < 2);
  await page.getByRole("button", { name: "回到最新消息", exact: true }).waitFor();
  await page.locator(".conversation-stream").press("PageDown");
  await page.waitForTimeout(250);
  assert((await metrics()).top > 20);
  record("earliest/latest controls and keyboard paging work");

  await page.getByRole("button", { name: "搜索聊天内容", exact: true }).click();
  await page.getByRole("textbox", { name: "查找聊天内容" }).fill("第 1 条请求");
  assert.equal(await page.locator(".message-row.search-match").count(), 1);
  await page.getByRole("button", { name: "复制消息", exact: true }).first().click();
  assert((await page.evaluate(() => navigator.clipboard.readText())).includes("第 1 条请求"));
  await page.getByRole("button", { name: "关闭聊天搜索", exact: true }).click();
  await page.getByRole("button", { name: "修改后重发", exact: true }).first().click();
  assert((await page.getByRole("textbox", { name: "消息内容" }).inputValue()).includes("第 1 条请求"));
  record("conversation search, message copying and edit-before-resend work");

  await page.getByRole("button", { name: "滚动到最早消息", exact: true }).click();
  const before = (await metrics()).top;
  messages.push({ ...messages[43], id: "new-message", sequence: 45, content: "实时新增回复。" });
  await page.getByTitle("刷新项目与会话").click();
  await page.getByText("实时新增回复。", { exact: true }).waitFor();
  await page.waitForTimeout(100);
  assert(Math.abs((await metrics()).top - before) < 2);
  await page.getByRole("button", { name: "回到最新消息", exact: true }).click();
  assert((await metrics()).max - (await metrics()).top < 2);
  record("new output preserves reader position until jump-to-latest");

  await page.getByRole("textbox", { name: "消息内容" }).fill("保留这个对话的草稿");
  await page.getByRole("button", { name: "滚动到最早消息", exact: true }).click();
  await page.locator(".tree-conversation").filter({ hasText: secondChat.title }).click();
  await page.getByRole("textbox", { name: "消息内容" }).waitFor();
  assert.equal(await page.getByRole("textbox", { name: "消息内容" }).inputValue(), "");
  await page.locator(".tree-conversation").filter({ hasText: conversation.title }).click();
  await page.locator(".message-row").nth(44).waitFor();
  assert.equal(await page.getByRole("textbox", { name: "消息内容" }).inputValue(), "保留这个对话的草稿");
  assert((await metrics()).top < 2);
  record("drafts and reading positions stay isolated by conversation");

  await page.getByRole("textbox", { name: "消息内容" }).press("Enter");
  assert.equal(submitted, null);
  assert((await page.getByRole("textbox", { name: "消息内容" }).inputValue()).includes("\n"));
  await page.getByRole("button", { name: "普通聊天", exact: true }).click();
  await page.getByRole("textbox", { name: "消息内容" }).press("Control+Enter");
  await page.waitForTimeout(100);
  assert.equal(submitted?.executionMode, "CHAT");
  assert.equal(submitted?.providerId, "provider-qa");
  record("Enter inserts a newline and Ctrl+Enter submits to the selected provider");

  await page.getByRole("button", { name: "运行详情", exact: true }).click();
  await page.getByText("上下文、缓存与速度", { exact: true }).waitFor();
  assert((await metrics()).streamBottom <= (await metrics()).composerTop + 1);
  await page.getByTitle("关闭运行详情").click();
  task.status = "RUNNING";
  messages.push({ id: "stream-message", sequence: 46, taskId: task.id, role: "assistant", content: "", status: "STREAMING", createdAt: timestamp });
  events = [{ sequence: 1, type: "agent.message.delta", timestamp, payload: { step: 1, delta: "流式输出第一段" } }];
  await page.getByTitle("刷新项目与会话").click();
  await page.getByText("流式输出第一段", { exact: true }).waitFor({ timeout: 10000 });
  await page.getByRole("button", { name: "滚动到最新消息", exact: true }).click();
  await page.getByRole("button", { name: "滚动到最早消息", exact: true }).click();
  events.push({ sequence: 2, type: "agent.message.delta", timestamp, payload: { step: 1, delta: "\n\n流式输出第二段" } });
  await page.getByText("流式输出第二段", { exact: true }).waitFor({ timeout: 10000 });
  assert((await metrics()).top < 2);
  assert(await page.locator(".current-activity").getByRole("button", { name: "停止", exact: true }).isVisible());
  record("streaming updates preserve old-history position and show a reachable stop control");
  task.status = "FAILED";
  task.error = "403 Forbidden: 本地模拟拒绝响应";
  await page.getByTitle("刷新项目与会话").click();
  await page.getByText("模型服务拒绝了本次请求", { exact: true }).waitFor();
  await page.getByRole("button", { name: "模型设置", exact: true }).click();
  await page.locator(".provider-settings").waitFor();
  await page.getByTitle("关闭").click();
  record("provider rejection shows manual recovery and opens model settings");
  events.push({ sequence: 3, type: "usage.updated", timestamp, payload: { requestUsageReported: true, usageReported: true, requestInputTokens: 10000, requestOutputTokens: 200, requestCachedInputTokens: 8000, requestCacheReported: true, requestCacheWriteTokens: 1000, requestCacheWriteReported: true, totalTokens: 10200, requestFirstTokenMs: 400, requestDurationMs: 2400, requestTokensPerSecond: 100, requestEstimatedCost: 0, costCurrency: "CNY", contextCeiling: 1000000, defaultInputBudget: 900000, maxOutputTokens: 32000, compression: "aggressive" } });
  await page.getByTitle("刷新项目与会话").click();
  await page.locator(".run-usage-summary").getByText("100 t/s", { exact: true }).waitFor();
  assert((await page.locator(".run-usage-summary").innerText()).includes("80%"));
  assert((await page.locator(".run-usage-summary").innerText()).includes("0.000000 CNY"));
  events.push(
    { sequence: 4, type: "agent.delegated", timestamp, payload: { agentId: "child-qa", kind: "queued", payload: { name: "检查工具配置", status: "queued" } } },
    { sequence: 5, type: "agent.delegated", timestamp, payload: { agentId: "child-qa", kind: "started", payload: { name: "检查工具配置", status: "running" } } },
    { sequence: 6, type: "agent.delegated", timestamp, payload: { agentId: "child-qa", kind: "usage.updated", payload: { requestUsageReported: true, requestInputTokens: 40, requestOutputTokens: 10, requestEstimatedCost: 0.001, costCurrency: "CNY" } } },
    { sequence: 7, type: "agent.delegated", timestamp, payload: { agentId: "child-qa", kind: "completed", payload: { status: "completed", result: "已检查", usage: { inputTokens: 40, outputTokens: 10, reported: true } } } }
  );
  await page.getByTitle("刷新项目与会话").click();
  await page.locator(".run-usage-summary").getByText("10,250", { exact: true }).waitFor();
  assert((await page.locator(".run-usage-summary").innerText()).includes("0.001000 CNY"));
  await page.getByRole("button", { name: "运行详情", exact: true }).click();
  await page.getByText("1m 上限 · 输入 900k · 输出 32k · 压缩 强力", { exact: true }).waitFor();
  assert((await page.locator(".run-resource-metrics").innerText()).includes("8,000 / 1,000"));
  await page.locator(".child-task-list").getByText("检查工具配置", { exact: true }).waitFor();
  assert((await page.locator(".child-task-list").innerText()).includes("已检查"));
  await page.getByTitle("关闭运行详情").click();
  await page.getByRole("button", { name: "模型设置", exact: true }).click();
  await page.getByLabel("上下文最大 Token", { exact: true }).fill("1000000");
  await page.getByLabel("输入最大 Token", { exact: true }).fill("900000");
  await page.getByLabel("输出最大 Token", { exact: true }).fill("32000");
  await page.getByLabel("上下文压缩", { exact: true }).selectOption("aggressive");
  await page.getByRole("button", { name: "增加区间", exact: true }).click();
  await page.locator(".price-row").nth(0).getByLabel("输入上限", { exact: true }).fill("10000");
  await page.locator(".price-row").nth(0).getByLabel("输入", { exact: true }).fill("2");
  await page.locator(".price-row").nth(0).getByLabel("输出", { exact: true }).fill("8");
  await page.locator(".price-row").nth(0).getByLabel("缓存读", { exact: true }).fill("0.5");
  await page.getByRole("button", { name: "增加区间", exact: true }).click();
  await page.locator(".price-row").nth(1).getByLabel("输入上限", { exact: true }).fill("24000");
  await page.getByRole("button", { name: "增加区间", exact: true }).click();
  await page.getByRole("button", { name: "保存服务", exact: true }).click();
  await page.getByText("服务已保存。点击「读取可用模型」后即可在对话中选择。", { exact: true }).waitFor();
  assert.equal(savedProvider.modelSettings["local-fixture"].contextTokens, 1000000);
  assert.equal(savedProvider.modelSettings["local-fixture"].compression, "aggressive");
  assert.deepEqual(savedProvider.modelSettings["local-fixture"].prices.map(row => row.upToInputTokens), [10000, 24000, 0]);
  assert.equal(savedProvider.modelSettings["local-fixture"].prices[0].cacheRead, 0.5);
  await page.screenshot({ path: resolve(process.env.PROOFCODE_UI_EVIDENCE || "../.proofcode/workbench-qa", "usage-settings.png") });
  await page.getByTitle("关闭").click();
  await page.getByRole("button", { name: "模型设置", exact: true }).click();
  assert.equal(await page.getByLabel("上下文最大 Token", { exact: true }).inputValue(), "1000000");
  assert.equal(await page.locator(".price-row").count(), 3);
  for (const label of ["上下文最大 Token", "输入最大 Token", "输出最大 Token"]) await page.getByLabel(label, { exact: true }).fill("");
  for (let i=0; i<3; i++) await page.getByRole("button", { name: "删除价格区间", exact: true }).first().click();
  await page.getByRole("button", { name: "保存服务", exact: true }).click();
  await page.getByText("服务已保存。点击「读取可用模型」后即可在对话中选择。", { exact: true }).waitFor();
  assert.equal(savedProvider.modelSettings["local-fixture"].contextTokens, null);
  assert.equal(savedProvider.modelSettings["local-fixture"].maxInputTokens, null);
  assert.equal(savedProvider.modelSettings["local-fixture"].maxOutputTokens, null);
  assert.deepEqual(savedProvider.modelSettings["local-fixture"].prices, []);
  assert.equal(await page.getByLabel("上下文最大 Token", { exact: true }).getAttribute("placeholder"), "默认 128000");
  for (const width of [390, 760]) {
    await page.setViewportSize({ width, height: 844 });
    assert(await page.locator(".provider-panel").evaluate(node => node.scrollWidth <= node.clientWidth + 1));
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.getByTitle("关闭").click();
  record("request timing, cache hit rate, zero cost, custom limits and tiered prices survive save and reopen");
  task.status = "SUCCEEDED";
  delete task.error;
  messages = messages.filter(value => value.id !== "stream-message");
  events = [];
  await page.getByTitle("刷新项目与会话").click();
  await page.waitForTimeout(100);
  const latest = page.getByRole("button", { name: "滚动到最新消息", exact: true });
  if (await latest.isEnabled()) await latest.click();
  await page.screenshot({ path: resolve(output, "chat-wide.png") });
  for (const size of [{ width: 962, height: 860 }, { width: 1024, height: 600 }, { width: 390, height: 844 }, { width: 760, height: 480 }]) {
    await page.setViewportSize(size);
    await page.waitForTimeout(100);
    geometry = await metrics();
    assert(geometry.height > 100, JSON.stringify({ size, geometry }));
    assert(geometry.streamBottom <= geometry.composerTop + 1, JSON.stringify({ size, geometry }));
    assert(geometry.composerBottom <= geometry.viewportHeight, JSON.stringify({ size, geometry }));
    assert(geometry.documentWidth <= geometry.viewportWidth + 1, JSON.stringify({ size, geometry }));
    if (size.width <= 760) assert(await page.locator(".nav-rail").evaluate(node => node.getBoundingClientRect().width) <= 60);
    await page.screenshot({ path: resolve(output, `chat-${size.width}x${size.height}.png`) });
  }
  record("details panel and four compact/mobile layouts keep chat reachable");
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.getByRole("textbox", { name: "搜索工程、对话和任务" }).fill("另一段");
  assert.equal(await page.locator(".tree-conversation").count(), 1);
  await page.getByRole("textbox", { name: "搜索工程、对话和任务" }).fill("");
  record("sidebar search filters real conversations without changing the selection");

  messages = [
    { ...messages[0], id: "design-user", sequence: 1, content: "检查聊天滚动问题，把修复方案和验证结果说明清楚。" },
    { ...messages[1], id: "design-assistant", sequence: 2, content: "已定位到聊天容器的高度约束：消息区随着内容增长，输入框会覆盖后面的回复。\n\n### 修复方案\n\n让聊天记录独立滚动，并把输入框放在固定的底部区域。你向上阅读时，新回复会保留当前阅读位置。\n\n```css\n.conversation-stream {\n  flex: 1;\n  min-height: 0;\n  overflow-y: scroll;\n}\n```\n\n**验证结果**：长对话、窗口缩放和流式输出均通过界面回归检查。修改可以在右侧审查入口查看。" }
  ];
  events = [
    { sequence: 4, type: "tool.requested", timestamp, payload: { callId: "read", tool: "read_files", arguments: { paths: ["frontend/src/Transcript.tsx"] } } },
    { sequence: 5, type: "tool.started", timestamp, payload: { callId: "read", tool: "read_files" } },
    { sequence: 6, type: "tool.completed", timestamp, payload: { callId: "read", tool: "read_files", result: "已读取 Transcript.tsx 和相关布局样式。" } },
    { sequence: 7, type: "tool.completed", timestamp, payload: { callId: "verify", tool: "run_command", result: "npm test\n22 tests passed" } }
  ];
  // Start this independent fixture history with a fresh event cursor.
  await page.reload();
  await page.locator(".execution-timeline").waitFor();
  await page.waitForFunction(() => document.querySelectorAll(".execution-entry").length === 3);
  assert.equal(await page.locator(".execution-entry").count(), 3);
  const readResult = page.locator(".execution-entry").filter({ hasText: "已完成：批量读取文件" });
  await readResult.locator(":scope > summary").click();
  assert((await readResult.locator(".execution-detail").innerText()).includes("已读取 Transcript.tsx 和相关布局样式。"));
  await readResult.locator(":scope > summary").click();
  await page.getByRole("button", { name: "复制代码", exact: true }).click();
  assert((await page.evaluate(() => navigator.clipboard.readText())).includes("overflow-y: scroll"));
  const earliestDesign = page.getByRole("button", { name: "滚动到最早消息", exact: true });
  if (await earliestDesign.isEnabled()) await earliestDesign.click();
  await page.screenshot({ path: resolve(output, "design-light.png") });
  await page.getByTitle("切换深色外观").click();
  assert.equal(await page.locator("html").getAttribute("data-theme"), "dark");
  await page.screenshot({ path: resolve(output, "design-dark.png") });
  await page.reload();
  await page.locator(".message-row").nth(1).waitFor();
  assert.equal(await page.locator("html").getAttribute("data-theme"), "dark");
  await page.getByTitle("切换浅色外观").click();
  record("tool lifecycle events become visible expandable milestones and theme survives reload");
  await page.getByRole("button", { name: "运行详情", exact: true }).click();
  await page.screenshot({ path: resolve(output, "design-details.png") });
  await page.getByTitle("关闭运行详情").click();
  await page.locator(".tree-conversation").filter({ hasText: secondChat.title }).click();
  await page.getByText("从一个想法，开始构建。", { exact: true }).waitFor();
  await page.waitForFunction(() => document.querySelector('select[aria-label="模型服务"]')?.value === "provider-qa");
  await page.screenshot({ path: resolve(output, "design-empty.png") });
  await page.locator(".starter-prompts button").first().click();
  assert((await page.getByRole("textbox", { name: "消息内容" }).inputValue()).includes("梳理项目结构"));
  assert.equal(submitted.executionMode, "CHAT");
  record("empty-state starters fill the composer without sending a task");
  assert.deepEqual(errors, []);
  record("no browser runtime errors");
  console.log(JSON.stringify({ checks: results.length, evidence: output }));
} catch (error) {
  try { await page.screenshot({ path: resolve(output, "failure.png") }); } catch {}
  let transcript = "";
  try { transcript = (await page.locator(".conversation-stream").innerText({ timeout: 1000 })).slice(-2500); } catch {}
  console.error({ errors, url: page.url(), title: await page.title().catch(() => ""), body: (await page.locator("body").innerText().catch(() => "")).slice(-2500), transcript, selectedTask: await page.locator(".sidebar-task.active").allTextContents().catch(() => []) });
  throw error;
} finally { await browser.close(); }
