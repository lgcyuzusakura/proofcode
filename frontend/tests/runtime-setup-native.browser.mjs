import assert from "node:assert/strict";
import { createRequire } from "node:module";

const config = JSON.parse(process.env.PROOFCODE_RUNTIME_SETUP_NATIVE || "null");
assert(config?.url && config?.token, "native test bridge is required");
const { chromium } = createRequire(import.meta.url)(process.env.PROOFCODE_PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ channel: "msedge", headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await context.newPage(), errors = [], api = [];
page.on("pageerror", error => errors.push(error.message));
await page.route("**/api/**", async route => { api.push(route.request().url()); await route.fulfill({ status: 503, json: { error: "offline" } }); });
await context.addInitScript(config => {
  localStorage.removeItem("proofcode.session"); localStorage.removeItem("proofcode.token");
  const call = async (path, body) => {
    const response = await fetch(config.url + path, { method: "POST", headers: { Authorization: "Bearer " + config.token, "Content-Type": "application/json" }, body: JSON.stringify(body || {}) });
    if (!response.ok) throw Error("native operation failed");
    return response.json();
  };
  window.cancelNativePrepare = () => call("/cancel");
  window.go = { main: { App: {
    CheckRuntime: async () => ({ checkedAt: "2026-10-22T06:00:00Z", checks: [] }),
    GetRuntimeManagement: () => call("/get"), GetBundledRuntime: () => call("/bundle"),
    PrepareBundledRuntime: (write, execute) => call("/prepare", { write, execute }),
    SelectRuntimeDirectory: () => call("/get"), RefreshRuntimeManagement: () => call("/get"),
    ManageRuntime: () => { throw Error("This test must never start or stop Docker"); },
  } } };
}, config);
try {
  await page.goto(process.env.PROOFCODE_UI_URL || "http://127.0.0.1:5177");
  const open = () => page.getByRole("button", { name: "运行环境与本机服务", exact: true }).click();
  const prepare = () => page.getByRole("button", { name: "准备本机运行文件", exact: true });
  await open(); await prepare().waitFor();
  await page.getByRole("checkbox", { name: "允许生成并应用代码补丁" }).uncheck();
  await page.getByRole("checkbox", { name: "允许执行命令和测试" }).check();
  await prepare().click(); await page.getByRole("button", { name: "更换部署目录", exact: true }).waitFor();
  await page.locator('.runtime-environment[aria-busy="false"]').waitFor();
  assert.match(await page.locator(".runtime-directory").innerText(), /ProofCode-runtime/);
  assert(await page.getByRole("button", { name: "启动服务", exact: true }).isEnabled());
  assert.equal(await page.getByText("全部就绪", { exact: true }).count(), 0);
  await page.getByRole("button", { name: "返回登录", exact: true }).click(); await open();
  await page.getByRole("button", { name: "更换部署目录", exact: true }).waitFor();
  await page.locator(".runtime-setup summary").click(); await prepare().click();
  await page.getByRole("alert").filter({ hasText: "已有 ProofCode-runtime" }).waitFor();
  assert(await page.getByRole("button", { name: "启动服务", exact: true }).isEnabled());
  await page.evaluate(() => window.cancelNativePrepare()); await prepare().click();
  await page.locator('.runtime-environment[aria-busy="false"]').waitFor();
  assert.match(await page.locator(".runtime-directory").innerText(), /ProofCode-runtime/);
  assert.equal(await page.getByRole("alert").count(), 0);
  assert.deepEqual(api, []); assert.deepEqual(errors, []);
  console.log("PASS browser uses actual Go bundle metadata/extraction/config persistence; permission flags, refusal to overwrite and cancel pass without Docker or model calls");
} finally { await browser.close(); }
