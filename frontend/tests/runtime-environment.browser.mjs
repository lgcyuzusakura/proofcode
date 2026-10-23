import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

const { chromium } = createRequire(import.meta.url)(process.env.PROOFCODE_PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ channel: "msedge", headless: true, ignoreDefaultArgs: ["--hide-scrollbars"] });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await context.newPage(), errors = [], results = [];
page.on("pageerror", error => errors.push(error.message));
const output = resolve(process.env.PROOFCODE_UI_EVIDENCE || ".proofcode/runtime-environment-qa");
await mkdir(output, { recursive: true });
const project = { id: "project-runtime", name: "Environment QA", defaultBranch: "main", sourceKind: "REMOTE_REPOSITORY" };
const workspace = { id: "workspace-runtime", projectId: project.id, name: "工作区", defaultWorkspace: true };
const conversation = { id: "chat-runtime", projectId: project.id, workspaceId: workspace.id, title: "环境检查", defaultConversation: true };
const scope = { projectId: project.id, workspaceId: workspace.id, conversationId: conversation.id };
await context.addInitScript(scope => { localStorage.setItem("proofcode.session", "fixture"); localStorage.setItem("proofcode.selected-scope", JSON.stringify(scope)); }, scope);
await page.route("**/api/**", async route => {
  const path = new URL(route.request().url()).pathname;
  let body = [];
  if (path === "/api/auth/me") body = { id: "qa", username: "fixture" };
  else if (path === "/api/projects") body = [project];
  else if (path.endsWith("/default-scope")) body = scope;
  else if (path.endsWith("/workspaces")) body = [workspace];
  else if (path.endsWith("/conversations")) body = [conversation];
  else if (path.includes("/agent-platform/selection/")) body = { profileId: null };
  else if (path.endsWith("/context")) body = { version: 0, view: { mode: "AUTO", items: [], packIds: [] }, messages: [], packs: [], excludedTurns: 0, estimatedTokens: 0 };
  await route.fulfill({ json: body });
});
const record = label => { results.push(label); console.log(`PASS ${label}`); };
const nav = () => page.getByRole("button", { name: "运行环境", exact: true }).click();
try {
  await page.goto(process.env.PROOFCODE_UI_URL || "http://127.0.0.1:5187");
  await nav();
  await page.getByText("运行环境检查仅在 Windows 桌面版中提供。", { exact: true }).waitFor();
  assert(await page.getByRole("button", { name: "重新检查", exact: true }).isDisabled());
  record("Web explains native-only checks without reporting fabricated status");

  const native = process.env.PROOFCODE_RUNTIME_NATIVE ? JSON.parse(process.env.PROOFCODE_RUNTIME_NATIVE) : null;
  await page.evaluate(native => {
    window.runtimeCalls = 0; window.runtimeMode = "ready";
    window.go = { main: { App: { CheckRuntime: async () => {
      window.runtimeCalls++;
      if (window.runtimeMode === "fail") throw Error("secret bridge failure");
      if (window.runtimeMode === "pending") await new Promise(resolve => { window.releaseRuntime = resolve; });
      if (native) {
        const response = await fetch(native.url, { method: "POST", headers: { Authorization: `Bearer ${native.token}` } });
        if (!response.ok) throw Error("native check failed");
        return response.json();
      }
      return { checkedAt: "2026-10-21T08:00:00Z", checks: [
        { id: "git", name: "Git", state: "ready", detail: "检查通过" },
        { id: "docker", name: "Docker CLI", state: "ready", detail: "检查通过" },
        { id: "docker-engine", name: "Docker 引擎", state: "unavailable", detail: "程序存在，但检查命令失败。" },
        { id: "compose", name: "Docker Compose", state: "ready", detail: "检查通过" },
        { id: "control-plane", name: "ProofCode 后端", state: "ready", detail: "健康检查通过" }
      ] };
    } } } };
  }, native);
  await page.getByRole("button", { name: "对话", exact: true }).click(); await nav();
  await page.locator(".runtime-check").nth(4).waitFor();
  assert.equal(await page.locator(".runtime-check").count(), 5);
  const first = await page.locator(".runtime-check-list").innerText();
  assert(first.includes("Git") && first.includes("Docker CLI") && first.includes("Docker Compose") && first.includes("健康检查通过"));
  record(native ? "React calls the real Windows CheckRuntime through a temporary loopback bridge" : "desktop bridge report renders all five checks (fixture)");

  await page.evaluate(() => { window.runtimeMode = "pending"; });
  const before = await page.evaluate(() => window.runtimeCalls);
  const refresh = page.getByRole("button", { name: "重新检查", exact: true });
  await refresh.click();
  await page.getByText("正在检查本机环境…", { exact: true }).waitFor();
  assert(await refresh.isDisabled());
  assert.equal(await page.locator(".runtime-check-list").innerText(), first);
  assert((await page.locator(".runtime-environment-status").innerText()).includes("上次检查时间"));
  assert.equal(await page.evaluate(() => window.runtimeCalls), before + 1);
  await page.evaluate(() => window.releaseRuntime());
  await page.locator('.runtime-environment[aria-busy="false"]').waitFor();
  record("refresh is single-flight and labels preserved report as the previous check");

  await page.evaluate(() => { window.runtimeMode = "fail"; }); await refresh.click();
  await page.getByRole("alert").getByText("下方保留上次检查结果，当前状态尚未确认。", { exact: false }).waitFor();
  assert.equal(await page.getByRole("alert").count(), 1);
  assert(!(await page.locator("body").innerText()).includes("secret bridge failure"));
  assert.equal(await page.locator(".runtime-check-list").innerText(), first);
  await page.evaluate(() => { window.runtimeMode = "ready"; }); await refresh.click();
  await page.getByRole("alert").waitFor({ state: "hidden" });
  await page.locator('.runtime-environment[aria-busy="false"]').waitFor();
  record("failed refresh preserves old results with one warning and retries without leaking bridge errors");

  for (const [width, height, theme] of [[1440,900,"light"],[390,640,"light"],[390,640,"dark"],[760,480,"dark"]]) {
    await page.setViewportSize({ width, height });
    await page.evaluate(theme => { document.documentElement.dataset.theme = theme; }, theme);
    assert(await page.locator(".runtime-check strong").first().evaluate(node => {
      const luminance = color => color.match(/[\d.]+/g).slice(0, 3).map(Number).map(v => v / 255).map(v => v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4).reduce((sum, v, index) => sum + v * [.2126, .7152, .0722][index], 0);
      const foreground = luminance(getComputedStyle(node).color), background = luminance(getComputedStyle(document.querySelector(".route-overlay")).backgroundColor);
      return (Math.max(foreground, background) + .05) / (Math.min(foreground, background) + .05) >= 4.5;
    }), "environment names must have readable contrast");
    const last = page.locator(".runtime-environment-note").last();
    await last.scrollIntoViewIfNeeded();
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
    assert(await last.evaluate(node => { const r = node.getBoundingClientRect(); return r.top < innerHeight && r.bottom > 0; }));
    await page.screenshot({ path: resolve(output, `runtime-${width}-${height}-${theme}.png`) });
  }
  await refresh.focus(); assert(await refresh.evaluate(node => node === document.activeElement));
  record("light/dark narrow and short layouts scroll to every status and retain keyboard access");
  assert.deepEqual(errors, []);
  await writeFile(resolve(output, "verification.json"), JSON.stringify({ results, checks: results.length, nativeBridge: !!native, modelCalls: 0, errors }, null, 2));
} catch (error) {
  await page.screenshot({ path: resolve(output, "failure.png") }).catch(() => {});
  console.error({ errors, body: (await page.locator("body").innerText()).slice(-2500) });
  throw error;
} finally { await browser.close(); }
