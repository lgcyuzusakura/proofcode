import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

const { chromium } = createRequire(import.meta.url)(process.env.PROOFCODE_PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ channel: "msedge", headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await context.newPage(), errors = [], requests = [], results = [];
const output = resolve(process.env.PROOFCODE_UI_EVIDENCE || ".proofcode/runtime-setup-qa");
await mkdir(output, { recursive: true });
page.on("pageerror", error => errors.push(error.message));
await page.route("**/api/**", async route => { requests.push(route.request().url()); await route.fulfill({ status: 503, json: { error: "offline" } }); });
await context.addInitScript(() => {
  localStorage.removeItem("proofcode.session"); localStorage.removeItem("proofcode.token");
  window.prepares = []; window.prepareMode = "cancel"; window.bundleMode = "ready";
  window.runtimeState = { directory: "", configured: false, busy: false, observed: false, running: false, phase: "idle", message: "", services: [], checkedAt: "" };
  window.go = { main: { App: {
    CheckRuntime: async () => ({ checkedAt: "2026-10-22T06:00:00Z", checks: [{ id: "docker", name: "Docker Desktop", state: "missing", detail: "请安装 Docker Desktop" }] }),
    GetRuntimeManagement: async () => structuredClone(window.runtimeState),
    SelectRuntimeDirectory: async () => structuredClone(window.runtimeState),
    RefreshRuntimeManagement: async () => structuredClone(window.runtimeState),
    ManageRuntime: async () => { throw Error("Services must never start in this fixture"); },
    GetBundledRuntime: async () => {
      if (window.bundleMode === "corrupt") throw Error("PRIVATE_ARCHIVE_PATH");
      return { available: window.bundleMode !== "missing", release: "0.4.0", files: 273, bytes: 1716826, sha256: "fixture" };
    },
    PrepareBundledRuntime: async (write, execute) => {
      window.prepares.push({ write, execute });
      if (window.prepareMode === "cancel") return structuredClone(window.runtimeState);
      window.runtimeState = { ...window.runtimeState, busy: true, phase: "preparing", message: "" };
      if (window.prepareMode === "pending") await new Promise(resolve => { window.releasePrepare = resolve; });
      if (window.prepareMode === "fail") {
        window.runtimeState = { ...window.runtimeState, busy: false, phase: "error", message: "所选位置已有 ProofCode-runtime，已有文件不会覆盖" };
        throw Error("PRIVATE_RAW_SECRET");
      }
      window.runtimeState = { ...window.runtimeState, configured: true, busy: false, phase: "idle", directory: "D:\\本机部署\\ProofCode-runtime", message: "运行文件已准备，尚未启动服务" };
      return structuredClone(window.runtimeState);
    },
  } } };
});
const record = text => { results.push(text); console.log("PASS " + text); };
const prepare = () => page.getByRole("button", { name: "准备本机运行文件", exact: true });
const open = () => page.getByRole("button", { name: "运行环境与本机服务", exact: true }).click();
const remount = async () => { await page.getByRole("button", { name: "返回登录", exact: true }).click(); await open(); };
const idle = () => page.locator('.runtime-environment[aria-busy="false"]').waitFor();
try {
  await page.goto(process.env.PROOFCODE_UI_URL || "http://127.0.0.1:5177"); await open(); await idle(); await prepare().waitFor();
  assert.deepEqual(await page.evaluate(() => window.prepares), []);
  assert(await page.getByRole("checkbox", { name: "允许生成并应用代码补丁" }).isChecked());
  assert(!(await page.getByRole("checkbox", { name: "允许执行命令和测试" }).isChecked()));
  assert(await page.getByRole("button", { name: "启动服务", exact: true }).isDisabled());
  for (const link of await page.getByRole("link", { name: "安装说明", exact: true }).all()) {
    assert.match(await link.getAttribute("href"), /^https:\/\/(docs\.docker\.com|git-scm\.com)\//);
    assert.equal(await link.getAttribute("rel"), "noopener noreferrer");
  }
  record("before login: bundled metadata and dependency links are passive; permissions and start state are explicit");
  await prepare().click(); await idle();
  assert.deepEqual(await page.evaluate(() => window.prepares), [{ write: true, execute: false }]);
  assert(await page.getByRole("button", { name: "启动服务", exact: true }).isDisabled());
  assert.equal(await page.getByRole("alert").count(), 0);
  record("cancelling folder selection keeps the previous configuration");
  await page.getByRole("checkbox", { name: "允许生成并应用代码补丁" }).uncheck();
  await page.getByRole("checkbox", { name: "允许执行命令和测试" }).check();
  await page.evaluate(() => { window.prepareMode = "pending"; }); await prepare().click();
  await page.getByText("正在准备本机运行文件…", { exact: true }).waitFor();
  assert(await prepare().isDisabled());
  assert(await page.getByRole("checkbox", { name: "允许执行命令和测试" }).isDisabled());
  await remount(); await page.getByText("正在准备本机运行文件…", { exact: true }).waitFor();
  assert(await prepare().isDisabled()); await page.evaluate(() => window.releasePrepare());
  await page.getByText("运行文件已准备，尚未启动服务", { exact: true }).waitFor(); await idle();
  assert.deepEqual(await page.evaluate(() => window.prepares), [{ write: true, execute: false }, { write: false, execute: true }]);
  assert(await page.getByRole("button", { name: "启动服务", exact: true }).isEnabled());
  assert.match(await page.locator(".runtime-directory").innerText(), /ProofCode-runtime/);
  record("permission choices reach the bridge; pending preparation survives remount with a single call and no implicit service start");
  await page.locator(".runtime-setup summary").click();
  await page.evaluate(() => { window.prepareMode = "fail"; }); await prepare().click(); await idle();
  await page.getByRole("alert").getByText("所选位置已有 ProofCode-runtime", { exact: false }).waitFor();
  assert(!(await page.locator("body").innerText()).includes("PRIVATE_RAW_SECRET"));
  assert.match(await page.locator(".runtime-directory").innerText(), /本机部署/);
  assert(await page.getByRole("button", { name: "启动服务", exact: true }).isEnabled());
  record("preparation failure preserves the configured directory and renders only the safe native error");
  await page.evaluate(() => { window.bundleMode = "corrupt"; }); await remount();
  await page.locator(".runtime-setup summary").click(); await page.getByText("内置运行包无法读取", { exact: false }).waitFor();
  assert(!(await page.locator("body").innerText()).includes("PRIVATE_ARCHIVE_PATH"));
  assert.equal(await prepare().count(), 0);
  await page.evaluate(() => { window.bundleMode = "missing"; }); await remount();
  await page.locator(".runtime-setup summary").click(); await page.getByText("此开发版本没有内置服务文件", { exact: false }).waitFor();
  assert(await page.getByRole("button", { name: "更换部署目录", exact: true }).isEnabled());
  record("corrupt and source-only builds offer existing-deployment fallback without exposing internal paths");
  await page.evaluate(() => { window.bundleMode = "ready"; }); await remount(); await page.locator(".runtime-setup summary").click();
  for (const [width, height, theme] of [[1440,900,"light"],[390,640,"light"],[390,640,"dark"],[760,480,"dark"]]) {
    await page.setViewportSize({ width, height }); await page.evaluate(theme => { document.documentElement.dataset.theme = theme; }, theme);
    await prepare().scrollIntoViewIfNeeded(); await prepare().focus();
    assert(await prepare().evaluate(node => node === document.activeElement));
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1 && document.querySelector(".auth-runtime-page").scrollWidth <= innerWidth + 1));
    await page.screenshot({ path: resolve(output, `setup-${width}-${height}-${theme}.png`) });
  }
  record("setup remains keyboard accessible and scrollable in desktop/mobile light/dark windows");
  assert.deepEqual(errors, []); assert.deepEqual(requests, []);
  await writeFile(resolve(output,"verification.json"),JSON.stringify({ results, errors, requests, actualDockerMutations: 0 },null,2));
} catch (error) {
  await page.screenshot({ path: resolve(output,"failure.png") }).catch(() => {}); throw error;
} finally { await browser.close(); }
