import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
const { chromium } = createRequire(import.meta.url)(process.env.PROOFCODE_PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ channel: "msedge", headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await context.newPage(), errors = [], results = [];
page.on("pageerror", error => errors.push(error.message));
const output = resolve(".proofcode/standalone-startup-qa"); await mkdir(output, { recursive: true });
await context.addInitScript(() => {
  localStorage.setItem("proofcode.session", "STALE_TEAM_SESSION");
  window.callsBeforeReady = 0; window.nativeReady = false;
  window.go = { main: { App: {
    GetStandaloneSession: async () => new Promise(resolve => { window.openLocal = () => { window.nativeReady = true; resolve({ token: "LOCAL_SESSION", user: { id: "local", username: "proofcode-local" } }); }; }),
    ProxyRequest: async (path, method, body, authorization) => {
      if (!window.nativeReady) window.callsBeforeReady++;
      window.lastAuthorization = authorization;
      let value = [];
      if (path === "/api/auth/me") value = { id: "local", username: "proofcode-local" };
      return { status: 200, body: JSON.stringify(value) };
    },
    GetRuntimeConnection: async () => ({ baseUrl: "http://127.0.0.1:39999", source: "standalone" }),
    SaveRuntimeConnection: async () => {},
    CheckRuntime: async () => ({ checkedAt: new Date().toISOString(), checks: [{ id: "git", name: "Git", state: "ready", detail: "bundled" }, { id: "control-plane", name: "ProofCode 后端", state: "ready", detail: "local" }] }),
    GetRuntimeManagement: async () => ({ configured: false, busy: false, services: [] }),
    RefreshRuntimeManagement: async () => ({}), SelectRuntimeDirectory: async () => ({}), ManageRuntime: async () => ({}),
  } } };
});
try {
  await page.goto(process.env.PROOFCODE_UI_URL || "http://127.0.0.1:5187");
  await page.getByRole("heading", { name: "正在打开本机工作区", exact: true }).waitFor();
  assert.equal(await page.evaluate(() => window.callsBeforeReady), 0);
  results.push("native startup finishes before account or project requests");
  await page.evaluate(() => window.openLocal());
  await page.getByText("新工程对话", { exact: true }).first().waitFor();
  assert.equal(await page.evaluate(() => localStorage.getItem("proofcode.session")), "LOCAL_SESSION");
  assert.equal(await page.evaluate(() => window.lastAuthorization), "Bearer LOCAL_SESSION");
  results.push("native local session replaces a stale team session before loading projects");
  await page.getByRole("button", { name: "运行环境", exact: true }).click();
  await page.getByRole("heading", { name: "本机独立运行", exact: true }).waitFor();
  assert.equal(await page.getByRole("button", { name: "启动服务", exact: true }).count(), 0);
  assert.equal(await page.getByText("Docker Desktop", { exact: true }).count(), 0);
  results.push("standalone mode shows local health without Compose deployment controls");
  for (const [width, height] of [[1440, 900], [390, 700]]) {
    await page.setViewportSize({ width, height });
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
    await page.screenshot({ path: resolve(output, `standalone-${width}.png`) });
  }
  assert.deepEqual(errors, []);
  await writeFile(resolve(output, "verification.json"), JSON.stringify({ fixture: true, modelCalls: 0, results, errors }, null, 2));
  for (const result of results) console.log(`PASS ${result}`);
} finally { await browser.close(); }
