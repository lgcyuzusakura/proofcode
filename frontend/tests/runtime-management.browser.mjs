import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

const { chromium } = createRequire(import.meta.url)(process.env.PROOFCODE_PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ channel: "msedge", headless: true, ignoreDefaultArgs: ["--hide-scrollbars"] });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await context.newPage(), errors = [], results = [], apiRequests = [];
const output = resolve(process.env.PROOFCODE_UI_EVIDENCE || ".proofcode/runtime-management-qa");
await mkdir(output, { recursive: true });
page.on("pageerror", error => errors.push(error.message));
await page.route("**/api/**", async route => { apiRequests.push(route.request().url()); await route.fulfill({ status: 503, json: { error: "fixture backend offline" } }); });
await context.addInitScript(() => {
  localStorage.removeItem("proofcode.session"); localStorage.removeItem("proofcode.token");
  window.runtimeActions = []; window.runtimeGetters = 0; window.runtimeOperationMode = "pending";
  window.runtimeState = { directory: "", configured: false, busy: false, observed: false, running: false, phase: "idle", message: "", services: [], checkedAt: "" };
  window.go = { main: { App: {
    CheckRuntime: async () => ({ checkedAt: "2026-10-22T06:00:00Z", checks: [
      { id: "git", name: "Git", state: "ready", detail: "检查通过" },
      { id: "docker", name: "Docker CLI", state: "ready", detail: "检查通过" },
      { id: "docker-engine", name: "Docker 引擎", state: "ready", detail: "检查通过" },
      { id: "compose", name: "Docker Compose", state: "ready", detail: "检查通过" },
      { id: "control-plane", name: "ProofCode 后端", state: "unavailable", detail: "无法连接，请启动服务" }
    ] }),
    GetRuntimeManagement: async () => { window.runtimeGetters++; return structuredClone(window.runtimeState); },
    SelectRuntimeDirectory: async () => {
      window.runtimeActions.push("select");
      if (window.runtimeOperationMode === "cancel") return structuredClone(window.runtimeState);
      window.runtimeState = { ...window.runtimeState, directory: "D:\\ProofCode-deployment\\" + "较长的部署目录".repeat(9), configured: true, message: "", phase: "idle" };
      return structuredClone(window.runtimeState);
    },
    RefreshRuntimeManagement: async () => {
      window.runtimeActions.push("check");
      window.runtimeState = { ...window.runtimeState, observed: true, message: "", phase: "idle", checkedAt: "2026-10-22T06:01:00Z", services: [
        { name: "postgres", state: "exited", health: "" }, { name: "redis", state: "exited", health: "" },
        { name: "artemis", state: "exited", health: "" }, { name: "control-plane", state: "exited", health: "" },
        { name: "runner", state: "exited", health: "" }, { name: "web", state: "exited", health: "" }
      ] };
      return structuredClone(window.runtimeState);
    },
    ManageRuntime: async action => {
      if (window.runtimeState.busy) throw Error("busy");
      window.runtimeActions.push(action);
      window.runtimeState = { ...window.runtimeState, busy: true, phase: action === "start" ? "starting" : "stopping", message: "" };
      if (window.runtimeOperationMode === "fail") {
        window.runtimeState = { ...window.runtimeState, busy: false, observed: false, running: false, phase: "error", message: "发现来自其他部署目录的 proofcode 容器；请选择原部署目录，未操作任何服务" };
        throw Error("PRIVATE_RAW_DOCKER_TOKEN");
      }
      await new Promise(resolve => { window.releaseRuntimeOperation = resolve; });
      window.runtimeState = { ...window.runtimeState, busy: false, observed: true, running: false, phase: "idle", message: "启动命令已结束，部分服务尚未就绪，请稍后重新检查。", checkedAt: "2026-10-22T06:02:00Z", services: [{ name: "postgres", state: "running", health: "starting" }] };
      return structuredClone(window.runtimeState);
    }
  } } };
});
const record = label => { results.push(label); console.log(`PASS ${label}`); };
const start = () => page.getByRole("button", { name: "启动服务", exact: true });
const refresh = () => page.getByRole("button", { name: "重新检查", exact: true });
const open = () => page.getByRole("button", { name: "运行环境与本机服务", exact: true }).click();
try {
  await page.goto(process.env.PROOFCODE_UI_URL || "http://127.0.0.1:5187");
  await page.getByRole("heading", { name: "登录工作区", exact: true }).waitFor();
  await open();
  await page.getByRole("region", { name: "本机服务管理" }).waitFor();
  await page.locator('.runtime-environment[aria-busy="false"]').waitFor();
  assert(await start().isDisabled());
  assert.deepEqual(apiRequests, []);
  assert.deepEqual(await page.evaluate(() => window.runtimeActions), []);
  record("runtime management is reachable before login with the backend offline; opening it does not mutate services");

  await page.evaluate(() => { window.runtimeOperationMode = "cancel"; });
  await page.getByRole("button", { name: "选择部署目录", exact: true }).click();
  await page.locator('.runtime-environment[aria-busy="false"]').waitFor();
  assert(await start().isDisabled());
  assert.equal(await page.getByRole("alert").count(), 0);
  await page.evaluate(() => { window.runtimeOperationMode = "pending"; });
  await page.getByRole("button", { name: "选择部署目录", exact: true }).click();
  await page.getByRole("button", { name: "更换部署目录", exact: true }).waitFor();
  assert(await start().isEnabled());
  await page.getByRole("button", { name: "返回登录", exact: true }).click(); await open();
  await page.getByRole("button", { name: "更换部署目录", exact: true }).waitFor();
  assert((await page.locator(".runtime-directory").innerText()).includes("D:\\ProofCode-deployment"));
  assert.equal((await page.evaluate(() => window.runtimeActions)).filter(value => value === "select").length, 2);
  record("cancel leaves the directory unchanged, and selected deployment survives page remount");

  await start().click();
  await page.getByText("正在构建并启动服务，首次运行可能需要数分钟…", { exact: true }).waitFor();
  for (const name of ["启动服务", "停止服务", "更换部署目录", "重新检查"]) assert(await page.getByRole("button", { name, exact: true }).isDisabled());
  await page.getByRole("button", { name: "返回登录", exact: true }).click(); await open();
  await page.getByText("正在构建并启动服务，首次运行可能需要数分钟…", { exact: true }).waitFor();
  assert(await start().isDisabled());
  await page.evaluate(() => window.releaseRuntimeOperation());
  await page.getByText("启动命令已结束，部分服务尚未就绪，请稍后重新检查。", { exact: true }).waitFor();
  await page.locator('.runtime-environment[aria-busy="false"]').waitFor();
  assert.equal((await page.evaluate(() => window.runtimeActions)).filter(value => value === "start").length, 1);
  assert((await page.locator(".runtime-service-list").innerText()).includes("准备中"));
  assert.equal(await page.getByText("全部就绪", { exact: true }).count(), 0);
  record("pending start survives navigation without duplicate calls; completed command does not fabricate readiness");

  await page.evaluate(() => { window.runtimeOperationMode = "fail"; });
  await start().click();
  await page.getByRole("alert").getByText("发现来自其他部署目录的 proofcode 容器", { exact: false }).waitFor();
  assert(!(await page.locator("body").innerText()).includes("PRIVATE_RAW_DOCKER_TOKEN"));
  await page.getByRole("button", { name: "返回登录", exact: true }).click(); await open();
  await page.getByRole("alert").getByText("发现来自其他部署目录的 proofcode 容器", { exact: false }).waitFor();
  await refresh().click();
  await page.getByRole("alert").waitFor({ state: "hidden" });
  await page.locator(".runtime-service-list > div").nth(5).waitFor();
  assert.equal((await page.evaluate(() => window.runtimeActions)).filter(value => value === "check").length, 1);
  record("redacted ownership failure remains visible across navigation; refresh recovers using actual state");

  await page.getByRole("button", { name: "停止服务", exact: true }).click();
  await page.getByRole("alert").waitFor();
  assert.equal((await page.evaluate(() => window.runtimeActions)).filter(value => value === "stop").length, 1);
  await refresh().click();
  await page.getByRole("alert").waitFor({ state: "hidden" });
  record("stop has a separate explicit action and shares failure recovery");

  for (const [width, height, theme] of [[1440,900,"light"],[390,640,"light"],[390,640,"dark"],[760,480,"dark"]]) {
    await page.setViewportSize({ width, height });
    await page.evaluate(theme => { document.documentElement.dataset.theme = theme; }, theme);
    const last = page.locator(".runtime-environment-note").last();
    await last.scrollIntoViewIfNeeded();
    assert(await last.evaluate(node => { const r = node.getBoundingClientRect(); return r.top < innerHeight && r.bottom > 0; }));
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1 && document.querySelector(".auth-runtime-page").scrollWidth <= innerWidth + 1));
    assert(await page.locator(".runtime-management h2").evaluate(node => {
      const luminance = color => color.match(/[\d.]+/g).slice(0,3).map(Number).map(v => v / 255).map(v => v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4).reduce((sum,v,i) => sum + v * [.2126,.7152,.0722][i],0);
      const fg = luminance(getComputedStyle(node).color), bg = luminance(getComputedStyle(document.querySelector(".auth-runtime-page")).backgroundColor);
      return (Math.max(fg,bg)+.05)/(Math.min(fg,bg)+.05) >= 4.5;
    }));
    await start().focus(); assert(await start().evaluate(node => node === document.activeElement));
    await page.locator(".auth-runtime-page").evaluate(node => { node.scrollTop = 0; });
    await page.screenshot({ path: resolve(output, `management-${width}-${height}-${theme}.png`) });
  }
  record("prelogin management scrolls in narrow and short windows, wraps long paths and keeps light/dark contrast and keyboard access");
  assert.deepEqual(errors, []); assert.deepEqual(apiRequests, []);
  await writeFile(resolve(output,"verification.json"),JSON.stringify({ results, checks: results.length, actualDockerMutations: 0, modelCalls: 0, fixture: true, errors },null,2));
} catch (error) {
  await page.screenshot({ path: resolve(output,"failure.png") }).catch(() => {});
  console.error({ errors, body: (await page.locator("body").innerText()).slice(-2500) }); throw error;
} finally { await browser.close(); }
