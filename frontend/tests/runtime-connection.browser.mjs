import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

const { chromium } = createRequire(import.meta.url)(process.env.PROOFCODE_PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ channel: "msedge", headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await context.newPage(), errors = [], results = [];
page.on("pageerror", error => errors.push(error.message));
const output = resolve(".proofcode/runtime-connection-qa"); await mkdir(output, { recursive: true });
await context.addInitScript(() => {
  localStorage.removeItem("proofcode.session"); localStorage.removeItem("proofcode.token");
  window.savedConnection = { baseUrl: "http://127.0.0.1:8080", source: "default" }; window.connectionSaves = []; window.connectionMode = "ready";
  window.go = { main: { App: {
    CheckRuntime: async () => ({ checkedAt: "2026-10-22T06:00:00Z", checks: [{ id: "control-plane", name: "ProofCode 后端", state: "unavailable", detail: "无法连接" }] }),
    GetRuntimeConnection: async () => { if (window.connectionMode === "load-fail") throw Error("SECRET_READ_ERROR"); return structuredClone(window.savedConnection); },
    SaveRuntimeConnection: async baseUrl => {
      window.connectionSaves.push({ baseUrl, session: localStorage.getItem("proofcode.session"), scope: localStorage.getItem("proofcode.selected-scope") });
      if (window.connectionMode === "save-fail") throw Error("SECRET_WRITE_ERROR");
      if (window.connectionMode === "pending") await new Promise(resolve => { window.releaseConnectionSave = resolve; });
      window.savedConnection = { baseUrl, source: "saved" }; return structuredClone(window.savedConnection);
    }
  } } };
});
const record = label => { results.push(label); console.log(`PASS ${label}`); };
const open = () => page.getByRole("button", { name: "运行环境与本机服务", exact: true }).click();
const input = () => page.getByRole("textbox", { name: "服务地址", exact: true });
const save = () => page.getByRole("button", { name: "保存后端地址", exact: true });
try {
  await page.goto(process.env.PROOFCODE_UI_URL || "http://127.0.0.1:5187"); await open();
  await page.waitForFunction(() => document.querySelector("#runtime-backend-url")?.value === "http://127.0.0.1:8080");
  await page.locator('.runtime-environment[aria-busy="false"]').waitFor();
  assert.equal((await page.evaluate(() => window.connectionSaves)).length, 0);
  record("prelogin settings load the backend URL without sending a save or model request");

  for (const value of ["https://user:secret@example.com", "https://example.com/v1", "https://example.com?key=secret", "https://example.com#secret", "file:///tmp/file"]) {
    await input().fill(value); await save().click();
    await page.getByRole("alert").getByText("请输入 http 或 https 根地址", { exact: false }).waitFor();
  }
  assert.equal((await page.evaluate(() => window.connectionSaves)).length, 0);
  record("model endpoints, embedded credentials and query or fragment URLs are rejected before native save");

  await page.evaluate(() => { localStorage.setItem("proofcode.session","OLD_BACKEND_SESSION"); localStorage.setItem("proofcode.selected-scope","old-scope"); sessionStorage.setItem("proofcode.editor:private-draft","keep-me"); window.connectionMode = "pending"; });
  await input().fill("https://EXAMPLE.com:443/"); await save().click();
  await page.getByRole("button", { name: "处理中…", exact: true }).waitFor();
  assert(await input().isDisabled());
  const calls = await page.evaluate(() => window.connectionSaves);
  assert.deepEqual(calls, [{ baseUrl: "https://example.com", session: null, scope: null }]);
  await page.evaluate(() => window.releaseConnectionSave());
  await page.getByRole("status").getByText("后端地址已保存。", { exact: false }).waitFor();
  assert.equal(await page.locator(".runtime-check").count(), 0);
  assert.equal(await page.evaluate(() => sessionStorage.getItem("proofcode.editor:private-draft")), "keep-me");
  await page.getByRole("button", { name: "返回登录", exact: true }).click(); await open();
  await page.waitForFunction(() => document.querySelector("#runtime-backend-url")?.value === "https://example.com");
  record("changing destination removes the old account session before save, preserves editor drafts and reloads the saved URL");

  await page.evaluate(() => { window.connectionMode = "save-fail"; });
  await input().fill("http://127.0.0.1:8099"); await save().click();
  await page.getByRole("alert").getByText("未能保存后端地址", { exact: false }).waitFor();
  assert.equal(await input().inputValue(), "http://127.0.0.1:8099");
  assert(!(await page.locator("body").innerText()).includes("SECRET_WRITE_ERROR"));
  await page.evaluate(() => { window.connectionMode = "ready"; }); await save().click();
  await page.getByRole("status").getByText("后端地址已保存。", { exact: false }).waitFor();
  record("failed save retains the entered address, redacts bridge errors and supports retry");

  await page.getByRole("button", { name: "返回登录", exact: true }).click();
  await page.evaluate(() => { window.connectionMode = "load-fail"; }); await open();
  await page.getByRole("alert").getByText("无法读取已保存的后端地址", { exact: false }).waitFor();
  assert(!(await page.locator("body").innerText()).includes("SECRET_READ_ERROR"));
  await input().fill("http://127.0.0.1:8080"); await page.evaluate(() => { window.connectionMode = "ready"; }); await save().click();
  await page.getByRole("status").getByText("后端地址已保存。", { exact: false }).waitFor();
  record("an unreadable local configuration can be repaired explicitly without a silent fallback");

  for (const [width,height,theme] of [[1440,900,"light"],[390,640,"light"],[390,640,"dark"]]) {
    await page.setViewportSize({ width,height }); await page.evaluate(theme => {document.documentElement.dataset.theme=theme},theme);
    await input().focus(); assert(await input().evaluate(node => node === document.activeElement));
    assert(await page.evaluate(() => document.querySelector(".auth-runtime-page").scrollWidth <= innerWidth + 1));
    await page.locator(".auth-runtime-page").evaluate(node => {node.scrollTop=0});
    await page.screenshot({path:resolve(output,`connection-${width}-${theme}.png`)});
  }
  record("backend settings remain usable in light and dark narrow windows with keyboard focus");
  assert.deepEqual(errors,[]);
  await writeFile(resolve(output,"verification.json"),JSON.stringify({results,checks:results.length,fixture:true,modelCalls:0,errors},null,2));
} finally {await browser.close()}
