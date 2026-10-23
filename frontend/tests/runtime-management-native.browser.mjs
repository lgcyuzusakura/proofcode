import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

const native = JSON.parse(process.env.PROOFCODE_RUNTIME_MANAGEMENT_NATIVE);
const { chromium } = createRequire(import.meta.url)(process.env.PROOFCODE_PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ channel: "msedge", headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await context.newPage(), errors = [];
page.on("pageerror", error => errors.push(error.message));
await context.addInitScript(native => {
  localStorage.removeItem("proofcode.session"); localStorage.removeItem("proofcode.token");
  const invoke = async action => {
    const response = await fetch(native.url + "/" + action, { method: "POST", headers: { Authorization: `Bearer ${native.token}` } });
    if (!response.ok) throw Error("native binding failed");
    return response.json();
  };
  window.go = { main: { App: {
    CheckRuntime: async () => ({ checkedAt: "2026-10-22T06:00:00Z", checks: [] }),
    GetRuntimeManagement: () => invoke("get"),
    RefreshRuntimeManagement: () => invoke("check"),
    SelectRuntimeDirectory: () => { throw Error("native picker requires an interactive Wails window"); },
    ManageRuntime: action => invoke(action)
  } } };
  window.releaseNativeRuntime = () => invoke("release");
}, native);
const output = resolve(".proofcode/runtime-management-qa");
await mkdir(output, { recursive: true });
try {
  await page.goto(process.env.PROOFCODE_UI_URL || "http://127.0.0.1:5187");
  await page.getByRole("button", { name: "运行环境与本机服务", exact: true }).click();
  const start = page.getByRole("button", { name: "启动服务", exact: true });
  await page.getByRole("button", { name: "更换部署目录", exact: true }).waitFor();
  await start.click();
  await page.getByText("正在构建并启动服务，首次运行可能需要数分钟…", { exact: true }).waitFor();
  await page.getByRole("button", { name: "返回登录", exact: true }).click();
  await page.getByRole("button", { name: "运行环境与本机服务", exact: true }).click();
  await page.getByText("正在构建并启动服务，首次运行可能需要数分钟…", { exact: true }).waitFor();
  assert(await start.isDisabled());
  await page.evaluate(() => window.releaseNativeRuntime());
  await page.getByText("全部就绪", { exact: true }).waitFor();
  assert.equal(await page.locator(".runtime-service-list > div").count(), 6);
  await page.getByRole("button", { name: "停止服务", exact: true }).click();
  await page.getByText("尚未全部就绪", { exact: true }).waitFor();
  assert.equal(await page.getByText("已停止", { exact: true }).count(), 6);
  await page.getByRole("button", { name: "重新检查", exact: true }).click();
  await page.locator('.runtime-environment[aria-busy="false"]').waitFor();
  assert.deepEqual(errors, []);
  await page.screenshot({ path: resolve(output,"management-native-bindings.png") });
  await writeFile(resolve(output,"native-verification.json"),JSON.stringify({ realGoBindings: true, dockerCommands: "injected fixtures", realWailsWindow: false, modelCalls: 0, errors },null,2));
  console.log("PASS real Go management bindings restore configuration, reject duplicate UI actions, retain pending operation across remount, and report observed start/stop state; Docker is mocked");
} finally { await browser.close(); }
