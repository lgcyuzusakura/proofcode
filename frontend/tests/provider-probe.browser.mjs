import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
const { chromium } = createRequire(import.meta.url)("playwright");
const browser = await chromium.launch({ channel: "msedge", headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
const page = await context.newPage(), errors = [], results = [], saved = [], probes = [];
page.on("pageerror", error => errors.push(error.message));
let providers = [], failProbe = false;
const record = message => { results.push(message); console.log(`PASS ${message}`); };
const output = resolve(".proofcode/provider-probe-qa"); await mkdir(output, { recursive: true });
await context.addInitScript(() => localStorage.setItem("proofcode.session", "fixture"));
await page.route("**/api/**", async route => {
  const request = route.request(), path = new URL(request.url()).pathname;
  let body = [];
  if (path === "/api/auth/me") body = { id: "fixture", username: "qa" };
  else if (path === "/api/model-providers") {
    if (request.method() === "POST") {
      const input = request.postDataJSON(); saved.push(input);
      body = { ...input, apiKey: undefined, id: "provider-fixture", hasKey: true, models: [], version: 0 };
      providers = [body];
    } else body = providers;
  } else if (path.endsWith("/models/refresh")) {
    providers = providers.map(item => ({ ...item, models: [{ id: "native-model", name: "Native model" }] }));
    body = providers[0];
  } else if (path.endsWith("/probe")) {
    probes.push(request.postDataJSON());
    body = { success: !failProbe, model: "native-model", protocol: "anthropic", outcome: failProbe ? "permission_or_policy" : "tool_call_validated", durationMs: 48, httpStatus: failProbe ? 403 : 200 };
  }
  await route.fulfill({ status: path === "/api/model-providers" && request.method() === "POST" ? 201 : 200, json: body });
});
try {
  await page.goto(process.env.PROOFCODE_UI_URL || "http://127.0.0.1:5187");
  await page.getByRole("button", { name: "设置", exact: true }).click();
  let dialog = page.getByRole("dialog", { name: "Agent 设置", exact: true });
  await dialog.getByLabel("服务名称").fill("Native fixture");
  await dialog.getByLabel("API Base URL").fill("https://fixture.invalid/v1");
  await dialog.getByLabel("API Key").fill("fixture-key");
  await dialog.getByLabel(/^协议/).selectOption("anthropic");
  assert.equal(probes.length, 0);
  await dialog.getByRole("button", { name: "保存服务", exact: true }).click();
  await dialog.getByRole("button", { name: "读取可用模型", exact: true }).click();
  await dialog.getByLabel(/^测试模型/).selectOption("native-model");
  assert.equal(saved[0].wireApi, "anthropic"); assert.equal(probes.length, 0);
  assert.equal(await dialog.getByLabel("API Key").inputValue(), "");
  record("native protocol persists, models load, key clears and no probe runs automatically");
  await dialog.getByRole("button", { name: "验证工具调用", exact: true }).click();
  await dialog.getByText(/最小工具调用测试通过/).waitFor();
  assert.deepEqual(probes, [{ model: "native-model" }]);
  record("explicit probe sends only the selected model and displays measured diagnostic time");
  await page.screenshot({ path: resolve(output, "provider-light.png") });
  failProbe = true;
  await dialog.getByRole("button", { name: "验证工具调用", exact: true }).click();
  await dialog.getByText(/测试未通过：服务拒绝访问或策略拦截（HTTP 403）/).waitFor();
  assert.equal(await page.evaluate(() => localStorage.getItem("proofcode.session")), "fixture");
  record("provider policy failure displays as a diagnostic and preserves account login");
  await dialog.getByLabel("服务名称").fill("Unsaved");
  assert.equal(await dialog.getByRole("button", { name: "验证工具调用", exact: true }).isDisabled(), true);
  record("unsaved connection edits disable testing stale configuration");
  for (const [width, height] of [[1440, 1000], [390, 844], [760, 480]]) {
    await page.setViewportSize({ width, height });
    const geometry = await dialog.evaluate(node => { const r = node.getBoundingClientRect(); return { left: r.left, right: r.right, bottom: r.bottom, width: innerWidth, height: innerHeight, scroll: node.scrollWidth, client: node.clientWidth }; });
    assert(geometry.left >= 0 && geometry.right <= geometry.width + 1 && geometry.bottom <= geometry.height + 1 && geometry.scroll <= geometry.client + 1, JSON.stringify(geometry));
    await page.screenshot({ path: resolve(output, `provider-${width}.png`) });
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.evaluate(() => { document.documentElement.dataset.theme = "dark"; });
  await page.screenshot({ path: resolve(output, "provider-dark.png") });
  record("light, dark, narrow and short settings remain inside the viewport");
  await page.reload();
  await page.getByRole("button", { name: "设置", exact: true }).click();
  dialog = page.getByRole("dialog", { name: "Agent 设置", exact: true });
  await dialog.getByText("Native fixture", { exact: true }).waitFor();
  assert.equal(await dialog.getByLabel(/^协议/).inputValue(), "anthropic");
  assert.equal(probes.length, 2); assert.deepEqual(errors, []);
  record("protocol survives reload without triggering paid requests or React errors");
  await writeFile(resolve(output, "verification.json"), JSON.stringify({ source: "local API fixtures; zero real model calls", results, errors }, null, 2));
} finally { await browser.close(); }
