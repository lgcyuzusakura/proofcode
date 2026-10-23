import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createHash } from "node:crypto";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

// End-to-end composer/IDE fixture. It uses a saved source snapshot and never
// contacts a model provider or starts a Runner task.
const { chromium } = createRequire(import.meta.url)("playwright");
const browser = await chromium.launch({ channel: "msedge", headless: true, ignoreDefaultArgs: ["--hide-scrollbars"] });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await context.newPage();
const errors = [];
page.on("pageerror", error => errors.push(error.message));
const origin = process.env.PROOFCODE_UI_URL || "http://127.0.0.1:5187";
const project = { id: "project-source", name: "Attachment QA", defaultBranch: "main", sourceKind: "REMOTE_REPOSITORY" };
const workspace = { id: "workspace-source", projectId: project.id, name: "工作区", kind: "REMOTE_REPOSITORY", defaultWorkspace: true };
const conversation = { id: "chat-source", projectId: project.id, workspaceId: workspace.id, title: "源码附件验收", defaultConversation: true };
const scope = { projectId: project.id, workspaceId: workspace.id, conversationId: conversation.id };
const content = "package demo\n\nfunc greet() string {\n\treturn \"你好，ProofCode\"\n}\n";
const bytes = Buffer.from(content, "utf8");
const sourceFile = { path: "internal/demo/greet.go", sha256: createHash("sha256").update(bytes).digest("hex"), content: bytes.toString("base64") };
const sourceFiles = [sourceFile, ...Array.from({ length: 9 }, (_, index) => ({ ...sourceFile, path: `src/module-${index}/示例文件.go` }))];
let tamperPreview = false;
const task = { id: "task-source", ...scope, workspaceId: workspace.id, sourceSnapshotId: "snapshot-source", resultSourceSnapshotId: "snapshot-source", prompt: "检查源码", model: "fixture-model", executionMode: "CODE", status: "SUCCEEDED", createdAt: "2026-10-21T10:00:00Z" };
const provider = { id: "provider-source", name: "Fixture provider", baseUrl: "http://127.0.0.1:1/v1", hasKey: true, wireApi: "responses", models: [{ id: "fixture-model", name: "Fixture model" }] };
let submitted = [];
let failNextSubmission = false;
let submitCount = 0;
const submissionKeys = [], results = [];
const record = label => { results.push(label); console.log(`PASS ${label}`); };
await context.addInitScript(({ scope }) => {
  localStorage.setItem("proofcode.session", "local-source-fixture");
  localStorage.setItem("proofcode.selected-scope", JSON.stringify(scope));
}, { scope });
await page.route("**/api/**", async route => {
  const request = route.request(), url = new URL(request.url()), path = url.pathname;
  let body;
  if (path === "/api/auth/me") body = { id: "qa", username: "local-qa" };
  else if (path === "/api/projects") body = [project];
  else if (path.endsWith("/default-scope")) body = scope;
  else if (path.endsWith("/workspaces")) body = [workspace];
  else if (path.endsWith("/conversations")) body = [conversation];
  else if (path.endsWith("/messages")) body = [];
  else if (path.includes("/agent-platform/selection/")) body = { profileId: null };
  else if (path.includes("/agent-platform/") || path.endsWith("/skills")) body = [];
  else if (path.endsWith("/context")) body = { version: 0, view: { mode: "AUTO", items: [], packIds: [] }, messages: [], packs: [], excludedTurns: 0, estimatedTokens: 0, selectionHash: "fixture", estimateMethod: "fixture" };
  else if (path === "/api/model-providers") body = [provider];
  else if (path === "/api/tasks" && request.method() === "POST") {
    submitCount++;
    submitted = request.postDataJSON();
    submissionKeys.push(request.headers()["idempotency-key"]);
    if (failNextSubmission) { failNextSubmission = false; return route.fulfill({ status: 503, json: { message: "Fixture: simulated task creation failure" } }); }
    body = { ...task, id: "task-submitted", prompt: submitted.prompt, status: "QUEUED" };
  }
  else if (path === "/api/tasks") body = [task];
  else if (path.endsWith("/events") || path.endsWith("/artifacts") || path.endsWith("/approvals") || path.endsWith("/instructions")) body = [];
  else if (path.endsWith("/sources/snapshot-source/files")) body = { id: "snapshot-source", files: sourceFiles.map(file => tamperPreview ? { ...file, content: Buffer.from("tampered private contents").toString("base64") } : file) };
  else body = {};
  await route.fulfill({ json: body });
});
const output = resolve(process.env.PROOFCODE_UI_EVIDENCE || ".proofcode/source-attachments-qa");
await mkdir(output, { recursive: true });
try {
  await page.goto(origin);
  await page.getByRole("button", { name: "文件", exact: true }).click();
  await page.getByRole("button", { name: sourceFile.path, exact: true }).waitFor();
  await page.getByRole("button", { name: sourceFile.path, exact: true }).click();
  await page.getByRole("button", { name: "附加文件或选区到聊天", exact: true }).waitFor();

  // A clean editor attaches the exact file from the task's immutable snapshot.
  await page.getByRole("button", { name: "附加文件或选区到聊天", exact: true }).click();
  await page.getByRole("button", { name: sourceFile.path, exact: true }).waitFor();
  assert.equal(await page.locator(".source-attachment").count(), 1);
  await page.getByRole("button", { name: sourceFile.path, exact: true }).click();
  await page.getByRole("dialog", { name: "源码附件预览" }).getByText("你好，ProofCode", { exact: false }).waitFor();
  await page.getByRole("button", { name: "关闭附件预览", exact: true }).click();
  await page.getByRole("button", { name: `移除附件 ${sourceFile.path}`, exact: true }).click();
  assert.equal(await page.locator(".source-attachment").count(), 0);
  await page.getByRole("button", { name: "文件", exact: true }).click();
  await page.getByRole("button", { name: sourceFile.path, exact: true }).click();
  await page.getByRole("button", { name: "附加文件或选区到聊天", exact: true }).click();
  await page.getByRole("button", { name: sourceFile.path, exact: true }).waitFor();
  record("file attachment can be previewed and removed, then reattached");

  tamperPreview = true;
  await page.getByRole("button", { name: sourceFile.path, exact: true }).click();
  await page.getByRole("dialog", { name: "源码附件预览" }).getByRole("alert").getByText("源码快照校验失败", { exact: false }).waitFor();
  assert.equal(await page.locator(".source-preview-dialog pre").count(), 0);
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: sourceFile.path, exact: true }).waitFor({ state: "visible" });
  assert(await page.getByRole("button", { name: sourceFile.path, exact: true }).evaluate(node => node === document.activeElement));
  tamperPreview = false;
  record("tampered snapshot bytes are rejected and closing restores preview focus");

  // Use Monaco keyboard selection rather than calling its model API.
  await page.getByRole("button", { name: "文件", exact: true }).click();
  await page.getByRole("button", { name: sourceFile.path, exact: true }).click();
  const editorInput = page.locator(".monaco-editor textarea.inputarea");
  await editorInput.focus();
  await page.keyboard.press("Control+Home");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("Shift+ArrowDown");
  await page.keyboard.press("Shift+ArrowDown");
  await page.getByRole("button", { name: "附加文件或选区到聊天", exact: true }).click();
  const rangeAttachment = page.locator(".source-attachment-preview").filter({ hasText: "L3–4" });
  await rangeAttachment.waitFor();
  await rangeAttachment.click();
  assert.equal(await page.locator(".source-preview-dialog pre").innerText(), 'func greet() string {\n\treturn "你好，ProofCode"');
  await page.keyboard.press("Escape");
  record("editor selection attaches exactly lines 3–4 and excludes column-one ending line");

  // Fill the draft through the IDE; the ninth distinct item must be rejected.
  for (let index = 0; index < 6; index++) {
    await page.getByRole("button", { name: "文件", exact: true }).click();
    await page.getByRole("button", { name: sourceFiles[index + 1].path, exact: true }).click();
    await page.getByRole("button", { name: "附加文件或选区到聊天", exact: true }).click();
    await page.locator(".source-attachment").nth(index + 2).waitFor();
  }
  assert.equal(await page.locator(".source-attachment").count(), 8);
  await page.getByRole("button", { name: "文件", exact: true }).click();
  await page.getByRole("button", { name: sourceFiles[7].path, exact: true }).click();
  await page.getByRole("button", { name: "附加文件或选区到聊天", exact: true }).click();
  await page.getByRole("alert").getByText("最多附加 8 个文件或选区", { exact: false }).waitFor();
  await page.getByRole("button", { name: "对话", exact: true }).click();
  assert.equal(await page.locator(".source-attachment").count(), 8);
  await page.reload();
  await page.locator(".source-attachment").nth(7).waitFor();
  record("eight real IDE attachments survive reload and the ninth is rejected");

  await page.setViewportSize({ width: 390, height: 640 });
  await page.locator(".source-attachment-preview").last().click();
  await page.locator(".source-preview-dialog pre").waitFor();
  const closePreview = page.getByRole("button", { name: "关闭附件预览", exact: true });
  await closePreview.focus();
  await page.keyboard.press("Shift+Tab");
  assert(await page.locator(".source-preview-dialog pre").evaluate(node => node === document.activeElement));
  await page.keyboard.press("Tab");
  assert(await closePreview.evaluate(node => node === document.activeElement));
  const bounds = await page.getByRole("dialog", { name: "源码附件预览" }).boundingBox();
  assert(bounds.x >= 0 && bounds.x + bounds.width <= 391 && bounds.y >= 0 && bounds.y + bounds.height <= 641);
  await page.screenshot({ path: resolve(output, "source-attachment-narrow.png") });
  await page.keyboard.press("Escape");
  await page.locator(".source-attachment-list").evaluate(node => { node.scrollTop = node.scrollHeight; });
  assert(await page.locator(".source-attachment-list").evaluate(node => node.scrollHeight > node.clientHeight && node.scrollTop > 0));
  await page.setViewportSize({ width: 1440, height: 900 });
  for (let count = 8; count > 1; count--) await page.getByRole("button", { name: /^移除附件 / }).last().click();
  record("narrow viewport keeps preview within screen, traps focus and scrolls attachment list");

  await page.getByRole("button", { name: "对话", exact: true }).click();
  await page.getByRole("textbox", { name: "消息内容" }).fill("请检查这个源码文件");
  failNextSubmission = true;
  await page.getByRole("button", { name: "运行任务", exact: true }).click();
  await page.getByText("发送失败，可重试同一请求", { exact: false }).waitFor();
  assert.equal(await page.locator(".source-attachment").count(), 1, "failure must preserve the attachment draft");
  assert.equal(await page.getByRole("textbox", { name: "消息内容" }).inputValue(), "请检查这个源码文件");
  assert.equal(submitted.sourceAttachments.length, 1);
  assert.equal(submitted.sourceAttachments[0].path, sourceFile.path);
  assert.equal(submitted.sourceAttachments[0].sha256, sourceFile.sha256);
  record("task creation failure preserves prompt and exact snapshot attachment");

  await page.getByRole("button", { name: "运行任务", exact: true }).click();
  await page.getByText("当前回合运行中", { exact: true }).waitFor();
  assert.equal(submitCount, 2);
  assert(submissionKeys[0] && submissionKeys[0] === submissionKeys[1], "retry must reuse the original idempotency key");
  assert.equal(submitted.sourceAttachments.length, 1);
  assert.equal(await page.locator(".source-attachment").count(), 0, "successful creation clears the attachment draft");
  assert.equal(await page.getByRole("textbox", { name: "消息内容" }).inputValue(), "");
  record("successful retry sends attachment with original idempotency key and clears draft");

  await page.screenshot({ path: resolve(output, "source-attachment-success.png") });
  assert.deepEqual(errors, []);
  record("no browser runtime errors");
  await writeFile(resolve(output, "results.json"), JSON.stringify({ checks: results.length, results, submissions: submitCount }, null, 2));
  console.log(JSON.stringify({ checks: results.length, submissions: submitCount, evidence: output }));
} catch (error) {
  try { await page.screenshot({ path: resolve(output, "failure.png") }); } catch {}
  console.error({ url: page.url(), errors, body: (await page.locator("body").innerText().catch(() => "")).slice(-3000) });
  throw error;
} finally { await browser.close(); }
