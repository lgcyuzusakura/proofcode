import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { createHash } from "node:crypto";
const { chromium } = createRequire(import.meta.url)(process.env.PROOFCODE_PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ channel: "msedge", headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await context.newPage(), errors = [];
page.on("pageerror", e => errors.push(e.message));
const project = { id: "project-qa", name: "ProofCode", defaultBranch: "main", sourceKind: "REMOTE_REPOSITORY" };
const workspace = { id: "workspace-qa", projectId: project.id, name: "工作区", kind: "REMOTE_REPOSITORY", defaultWorkspace: true };
const conversation = { id: "chat-qa", projectId: project.id, workspaceId: workspace.id, title: "项目开发", defaultConversation: true };
const scope = { projectId: project.id, workspaceId: workspace.id, conversationId: conversation.id };
const agents = [], memories = [], mcpServers = [], skills = [];
let accountPreference = { version: 0, values: { responseLanguage: "", answerStyle: "", codeStyle: "", collaborationStyle: "" } };
let failPreferenceLoad = false, failSkillsLoad = false;
const probeRequests = [], mcpWrites = [];
const oauthRequests = [], oauthStates = new Map();
let probeCode = "ok", failMcpLoad = false;
let providers = [{ id: "provider", name: "Fixture", models: [{ id: "fixture-model", name: "Fixture" }] }, { id: "specialist", name: "Specialist", models: [{ id: "review-model", name: "Review" }, { id: "code-model", name: "Code" }] }];
const submitted = [];
let selection = null, failSelection = false;
const requests = [];
await context.addInitScript(({ scope }) => { localStorage.setItem("proofcode.session", "fixture"); localStorage.setItem("proofcode.selected-scope", JSON.stringify(scope)); }, { scope });
await context.addInitScript(() => { window.open = () => null; });
await page.route("**/api/**", async route => {
 const request = route.request(), url = new URL(request.url()), path = url.pathname, method = request.method();
 let body = {};
 if (path === "/api/account/preferences") {
  if (method === "GET") {
   if (failPreferenceLoad) return route.fulfill({ status: 503, json: { message: "preference load unavailable" } });
   body = accountPreference;
  } else {
   const input = request.postDataJSON();
   if (input.version !== accountPreference.version) return route.fulfill({ status: 409, json: { message: "personal preference conflict" } });
   accountPreference = { version: accountPreference.version + 1, values: input.values }; body = accountPreference;
  }
 } else if (path.endsWith("/skills") || path.includes("/skills/")) {
  if (method === "GET") {
   if (failSkillsLoad) return route.fulfill({status:503,json:{message:"Skills unavailable"}});
   body=skills;
  } else if (path.endsWith("/delete")) {
   const id=path.split("/skills/")[1].split("/")[0], index=skills.findIndex(value=>value.id===id);
   if(index<0||skills[index].version!==Number(url.searchParams.get("version")))return route.fulfill({status:409,json:{message:"Skill changed"}});
   skills.splice(index,1);return route.fulfill({status:204});
  } else {
   const input=request.postDataJSON(), importing=path.endsWith("/import"), id=path.match(/\/skills\/([^/]+)$/)?.[1], old=skills.find(value=>value.id===id);
   if(old&&old.version!==input.version)return route.fulfill({status:409,json:{message:"Skill changed; reload"}});
   if(importing&&!input.markdown.startsWith("---\n"))return route.fulfill({status:400,json:{message:"Invalid SKILL.md"}});
   const value=importing?{name:"imported-review",description:"Imported API review workflow",content:"# Imported review\nInspect permissions and test results.",source:"SKILL.md",enabled:true}:{...input,source:"editor"};
   if(skills.some(skill=>skill.name===value.name&&skill.id!==id))return route.fulfill({status:409,json:{message:"Duplicate skill"}});
   value.id=old?.id||`skill-${skills.length+1}`;value.version=old?old.version+1:0;value.hash=createHash("sha256").update(value.content).digest("hex");
   if(old)skills[skills.indexOf(old)]=value;else skills.push(value);body=value;
  }
 } else if (path.includes("/agent-platform/")) {
  requests.push([method, path]);
  const suffix = path.split("/agent-platform/")[1];
  if (suffix.startsWith("selection/")) {
   if (method === "PATCH") {
    if (failSelection) return route.fulfill({ status: 409, json: { message: "selection conflict" } });
    selection = request.postDataJSON().profileId;
   }
   body = { profileId: selection };
  } else if (suffix === "preview") body = memories.filter(v => v.memory.enabled && (v.memory.pinned || url.searchParams.get("query").includes("测试"))).map(v => ({ id: v.id, name: v.name, content: v.memory.content, version: v.version, scope: "project", score: 2, reason: v.memory.pinned ? "你将它设为始终参考" : "匹配 2 个词项" }));
  else {
   const [kind, id, operation] = suffix.split("/"), values = kind === "AGENT" ? agents : memories;
   if (operation === "delete") { values.splice(values.findIndex(v => v.id === id), 1); return route.fulfill({ status: 204 }); }
   if (method === "GET") body = values;
   else { const input = request.postDataJSON(); const item = { ...input, id: id || `resource-${agents.length + memories.length}`, version: id ? input.version + 1 : 0 }; const index = values.findIndex(v => v.id === id); if (index >= 0) values[index] = item; else values.push(item); body = item; }
  }
 } else if (path.endsWith("/mcp-servers") || path.includes("/mcp-servers/")) {
  if (path.includes("/oauth")) {
   const id = path.split("/mcp-servers/")[1].split("/")[0], saved = mcpServers.find(value => value.id === id);
   oauthRequests.push({ method, path, body: request.postData() });
   if (!saved || saved.version !== Number(url.searchParams.get("version"))) return route.fulfill({ status: 409, json: { message: "OAuth configuration changed; reload" } });
   let state = oauthStates.get(id) || { configured: false, clientId: "", scope: "", hasClientSecret: false, redirectUri: "http://localhost:8080/api/mcp/oauth/callback", state: "NOT_CONNECTED" };
   if (method === "PUT") {
    const input = request.postDataJSON();
    state = { ...state, configured: !!input.clientId, clientId: input.clientId, scope: input.scope, hasClientSecret: input.clearClientSecret ? false : !!input.clientSecret || state.hasClientSecret, state: "NOT_CONNECTED" };
    saved.version++;
   } else if (path.endsWith("/begin")) {
    state = { ...state, state: "PENDING" }; oauthStates.set(id, state);
    return route.fulfill({ json: { authorizationUrl: "https://oauth.example.test/authorize?state=fixture" } });
   } else if (path.endsWith("/cancel")) state = { ...state, state: "CANCELLED" };
   oauthStates.set(id, state); body = { ...state, serverVersion: saved.version };
  }
  else if (method === "GET") { if(failMcpLoad)return route.fulfill({status:503,json:{message:"MCP services unavailable"}});body = mcpServers; }
  else if (path.endsWith("/probe")) {
   const id=path.split("/mcp-servers/")[1].split("/")[0], saved=mcpServers.find(value=>value.id===id);
   probeRequests.push({path,version:Number(url.searchParams.get("version")),body:request.postData()});
   if(!saved||saved.version!==Number(url.searchParams.get("version")))return route.fulfill({status:409,json:{message:"服务配置已变化，请重新加载后诊断"}});
   body={savedVersion:saved.version,result:{ok:probeCode==="ok",code:probeCode,stage:probeCode==="ok"?"complete":"initialize",checkedAt:new Date().toISOString(),elapsedMs:82,httpStatus:probeCode==="forbidden"?403:null,protocol:probeCode==="ok"?"2025-03-26":null,tools:probeCode==="ok"?[{name:"search_docs",qualifiedName:`mcp__${saved.name}__search_docs`,description:"搜索项目文档 <script>window.mcpInjected = true</script>",inputSchema:{type:"object",properties:{query:{type:"string",description:"关键词"}},required:["query"]}},{name:"read_file",qualifiedName:`mcp__${saved.name}__read_file`,description:"读取项目文件",inputSchema:{type:"object",properties:{path:{type:"string"}}}}]:[]}};
  }
  else if (path.endsWith("/delete")) { const id = path.split("/mcp-servers/")[1].split("/")[0]; mcpServers.splice(mcpServers.findIndex(v => v.id === id), 1); return route.fulfill({ status: 204 }); }
  else { const input = request.postDataJSON(), id = path.match(/\/mcp-servers\/([^/]+)$/)?.[1], old = mcpServers.find(v => v.id === id); mcpWrites.push(input); const value = { id: id || `mcp-${mcpServers.length + 1}`, name: input.name, url: input.url, hasBearer: input.clearBearer ? false : input.bearer ? true : old?.hasBearer || false, headerName: input.clearHeader ? null : input.headerName || old?.headerName || null, hasHeader: input.clearHeader ? false : input.headerValue ? true : old?.hasHeader || false, enabled: input.enabled, version: old ? old.version + 1 : 0 }; const index = mcpServers.findIndex(v => v.id === value.id); if (index >= 0) mcpServers[index] = value; else mcpServers.push(value); body = value; }
 } else if (path === "/api/auth/me") body = { id: "user", username: "fixture" };
 else if (path === "/api/projects") body = [project];
 else if (path.endsWith("/default-scope")) body = scope;
 else if (path.endsWith("/workspaces")) body = [workspace];
 else if (path.endsWith("/conversations")) body = [conversation];
 else if (path.endsWith("/messages")) body = [];
 else if (path.endsWith("/context")) body = { version: 0, view: { mode: "AUTO", items: [], packIds: [] }, messages: [], packs: [], excludedTurns: 0, estimatedTokens: 0, selectionHash: "fixture", estimateMethod: "fixture" };
 else if (path === "/api/model-providers") body = providers;
 else if (path === "/api/tasks" && method === "POST") { submitted.push(request.postDataJSON()); return route.fulfill({ status: 503, json: { message: "Fixture: recorded submission without running a model" } }); }
 else if (path === "/api/tasks" || /\/(events|artifacts|approvals)$/.test(path)) body = [];
 return route.fulfill({ json: body });
});
const results = [], record = message => { results.push(message); console.log(`PASS ${message}`); };
try {
 await page.goto(process.env.PROOFCODE_UI_URL || "http://127.0.0.1:5177");
 await page.getByRole("button", { name: "智能体与记忆", exact: true }).click();
 const dialog = page.getByRole("dialog", { name: "智能体、偏好与项目记忆" });
 await dialog.getByLabel("名称", { exact: true }).fill("代码审查员");
 await dialog.getByLabel("角色与工作方式").fill("先检查现有测试，再给出改进建议。");
 await dialog.getByLabel("只读调查，禁止修改与执行命令").check();
 await dialog.getByLabel("允许的工具").fill("read_file, search_code, git_diff");
 await dialog.getByLabel("MCP 服务别名").fill("docs, issues");
 await dialog.getByLabel("默认模型服务", { exact: true }).selectOption("specialist");
 await dialog.getByLabel("默认模型", { exact: true }).selectOption("review-model");
 await dialog.getByRole("button", { name: "保存", exact: true }).click();
 await page.waitForFunction(() => document.querySelector(".agent-platform-body aside strong")?.textContent === "代码审查员");
 assert.deepEqual(agents[0].profile.allowedTools, ["read_file", "search_code", "git_diff"]);
 assert.deepEqual(agents[0].profile.mcpServers, ["docs", "issues"]);
 assert(agents[0].profile.readOnly); record("agent role, read-only restriction and multiple tool/server names persist");
 assert.deepEqual(agents[0].profile.modelChoice, { providerId: "specialist", model: "review-model" });
 const output = resolve(".proofcode/agent-platform-qa"); await mkdir(output, { recursive: true });
 failPreferenceLoad = true;
 await dialog.getByRole("button", { name: "个人偏好", exact: true }).click();
 await dialog.getByRole("alert").waitFor();
 assert(await dialog.getByRole("button", { name: "保存个人偏好", exact: true }).isDisabled());
 failPreferenceLoad = false;
 await dialog.getByRole("button", { name: "重新加载个人偏好" }).click();
 await dialog.getByLabel("回答语言", { exact: true }).fill("中文，代码标识符保留英文");
 await dialog.getByLabel("回答方式", { exact: true }).fill("先讲结论，再给依据和验证结果。");
 await dialog.getByLabel("编码偏好", { exact: true }).fill("优先遵循项目现有规范。");
 await dialog.getByLabel("协作方式", { exact: true }).fill("完成后说明改动和测试结果。");
 await dialog.getByRole("button", { name: "保存个人偏好", exact: true }).click();
 await dialog.getByText("个人偏好已保存，新任务会使用这份设置。", { exact: true }).waitFor();
 assert.equal(accountPreference.version, 1); assert.equal(accountPreference.values.responseLanguage, "中文，代码标识符保留英文");
 record("explicit account preferences save; failed initial load cannot overwrite settings");
 await dialog.locator("form").evaluate(node => { node.scrollTop = 0; });
 await page.screenshot({ path: resolve(output, "personal-preferences-light.png") });
 await page.evaluate(() => { document.documentElement.dataset.theme = "dark"; });
 await page.screenshot({ path: resolve(output, "personal-preferences-dark.png") });
 await page.evaluate(() => { document.documentElement.dataset.theme = "light"; });
 for (const [width, height] of [[390,844],[760,480]]) {
  await page.setViewportSize({width,height});
  await dialog.getByLabel("协作方式", { exact: true }).scrollIntoViewIfNeeded();
  const visible = await dialog.getByLabel("协作方式", { exact: true }).evaluate(node => { const r = node.getBoundingClientRect(); return node.contains(document.elementFromPoint(r.left+r.width/2,r.top+r.height/2)); });
  assert(visible, "preferences field reachable in short/narrow window");
  await page.screenshot({path:resolve(output,`personal-preferences-${width}.png`)});
 }
 await page.setViewportSize({width:1440,height:900});
 accountPreference = { version: 2, values: { ...accountPreference.values, answerStyle: "外部更新版本" } };
 await dialog.getByLabel("回答方式", { exact: true }).fill("这次提交使用过期版本");
 await dialog.getByRole("button", { name: "保存个人偏好", exact: true }).click();
 await dialog.getByRole("alert").waitFor();
 assert.equal(accountPreference.values.answerStyle, "外部更新版本");
 await dialog.getByRole("button", { name: "重新加载个人偏好" }).click();
 await page.waitForFunction(() => document.querySelector('.personal-preferences-body textarea')?.value === "外部更新版本");
 record("stale preference save rejects overwrite and reloads concurrent version");
 await dialog.getByRole("button", { name: "清空内容", exact: true }).click();
 await dialog.getByRole("button", { name: "保存个人偏好", exact: true }).click();
 await dialog.getByText("个人偏好已保存，新任务会使用这份设置。", { exact: true }).waitFor();
 assert(Object.values(accountPreference.values).every(value => value === ""));
 await dialog.getByRole("button", {name:"Skills",exact:true}).click();
 assert(await dialog.getByLabel("导入 Skill 文件",{exact:true}).isHidden(),"native file picker stays hidden behind import action");
 await dialog.getByLabel("Skill 名称",{exact:true}).fill("api-review");
 await dialog.getByLabel("适用场景",{exact:true}).fill("Review API changes, permissions and tests.");
 await dialog.getByLabel("工作流程",{exact:true}).fill("# API review\nInspect existing tests before changing code.\nReport verification evidence.");
 await dialog.getByRole("button",{name:"保存 Skill",exact:true}).click();
 await dialog.getByText("Skill 已保存，新任务会使用这份版本。",{exact:true}).waitFor();
 assert.equal(skills.length,1);assert(skills[0].enabled);record("editable workflow saves as a project skill with version and hash");
 await dialog.locator("form").evaluate(node=>{node.scrollTop=0;});
 await page.screenshot({path:resolve(output,"skills-light.png")});
 await page.evaluate(()=>{document.documentElement.dataset.theme="dark";});
 await page.screenshot({path:resolve(output,"skills-dark.png")});
 await page.evaluate(()=>{document.documentElement.dataset.theme="light";});
 for(const [width,height] of [[390,844],[760,480]]){
  await page.setViewportSize({width,height});
  await page.waitForFunction(()=>{const node=document.querySelector('.agent-platform-panel nav button[aria-pressed="true"]');if(!node)return false;const r=node.getBoundingClientRect(),p=node.parentElement.getBoundingClientRect();return r.left>=p.left&&r.right<=p.right;});
  await dialog.getByLabel("工作流程",{exact:true}).scrollIntoViewIfNeeded();
  const visible=await dialog.getByLabel("工作流程",{exact:true}).evaluate(node=>{const r=node.getBoundingClientRect();return node.contains(document.elementFromPoint(r.left+r.width/2,r.top+r.height/2));});assert(visible,"skill workflow remains reachable");
  const geometry=await dialog.evaluate(node=>{const r=node.getBoundingClientRect();return r.left>=0&&r.right<=innerWidth&&r.bottom<=innerHeight&&node.scrollWidth<=node.clientWidth+1;});assert(geometry,"skill settings fit viewport");
  await page.screenshot({path:resolve(output,`skills-${width}.png`)});
 }
 await page.setViewportSize({width:1440,height:900});record("skill editor scrolls within light/dark narrow and short windows");
 const download=page.waitForEvent("download");await dialog.getByRole("button",{name:"导出",exact:true}).click();assert.equal((await download).suggestedFilename(),"api-review-SKILL.md");
 await dialog.getByLabel("导入 Skill 文件",{exact:true}).setInputFiles({name:"SKILL.md",mimeType:"text/markdown",buffer:Buffer.from("invalid workflow")});
 await dialog.getByRole("alert").waitFor();assert.equal(skills.length,1);
 await dialog.getByLabel("导入 Skill 文件",{exact:true}).setInputFiles({name:"SKILL.md",mimeType:"text/markdown",buffer:Buffer.from("---\nname: imported-review\ndescription: Imported API review workflow\n---\n\n# Imported review\nInspect permissions and test results.")});
 await dialog.getByText("SKILL.md 已导入，可以编辑或分配给智能体。",{exact:true}).waitFor();assert.equal(skills.length,2);assert.equal(skills[1].source,"SKILL.md");record("markdown import rejects invalid input without replacing saved workflows and supports export");
 skills[1].version++;skills[1].content="Concurrent update";
 await dialog.getByLabel("工作流程",{exact:true}).fill("Stale edit");await dialog.getByRole("button",{name:"保存 Skill",exact:true}).click();await dialog.getByRole("alert").waitFor();assert.equal(skills[1].content,"Concurrent update");
 await dialog.getByRole("button",{name:"重新加载 Skills",exact:true}).click();await dialog.getByText("已加载最新 Skills。",{exact:true}).waitFor();assert.equal(await dialog.getByLabel("工作流程",{exact:true}).inputValue(),"Concurrent update");record("stale skill edits are rejected and reload restores saved version");
 await dialog.getByLabel("启用 Skill",{exact:true}).uncheck();await dialog.getByRole("button",{name:"保存 Skill",exact:true}).click();await dialog.getByText("Skill 已保存，新任务会使用这份版本。",{exact:true}).waitFor();assert(!skills[1].enabled);
 failSkillsLoad=true;await dialog.getByRole("button",{name:"重新加载 Skills",exact:true}).click();await dialog.getByRole("alert").waitFor();assert(await dialog.getByRole("button",{name:"保存 Skill",exact:true}).isDisabled());
 failSkillsLoad=false;await dialog.getByRole("button",{name:"重新加载 Skills",exact:true}).click();await dialog.getByText("已加载最新 Skills。",{exact:true}).waitFor();record("disabled skills persist and failed catalog loads block editing until reload");
 await dialog.getByRole("button", { name: "智能体", exact: true }).click();
 await dialog.locator("aside button").filter({ hasText: "代码审查员" }).click();
 await dialog.getByLabel("api-review",{exact:true}).check();assert(await dialog.getByLabel("imported-review（已停用）",{exact:true}).isDisabled());
 await dialog.getByLabel("允许的工具").fill("read_file, search_code, git_diff, skill_read");await dialog.getByRole("button",{name:"保存",exact:true}).click();
 await page.waitForFunction(()=>!document.querySelector('.agent-platform-body fieldset')?.disabled);assert.deepEqual(agents[0].profile.skillIds,[skills[0].id]);record("custom agent binds selected skills and excludes disabled workflows");
 await dialog.getByRole("button",{name:"Skills",exact:true}).click();await dialog.locator("aside button").filter({hasText:"api-review"}).click();
 page.once("dialog",value=>value.accept());await dialog.getByRole("button",{name:"删除 Skill",exact:true}).click();await dialog.getByText("Skill 已删除。",{exact:true}).waitFor();
 await dialog.getByRole("button",{name:"智能体",exact:true}).click();await dialog.locator("aside button").filter({hasText:"代码审查员"}).click();
 await dialog.getByLabel("Skill 已不可用，取消勾选后保存",{exact:true}).click();await dialog.getByLabel("Skill 已不可用，取消勾选后保存",{exact:true}).waitFor({state:"hidden"});await dialog.getByRole("button",{name:"保存",exact:true}).click();await page.waitForFunction(()=>!document.querySelector('.agent-platform-body fieldset')?.disabled);assert.deepEqual(agents[0].profile.skillIds,[]);record("deleted skill bindings remain visible until explicitly removed from agent");
 await dialog.locator("form").evaluate(node => { node.scrollTop = 0; });
 await page.screenshot({ path: resolve(output, "agent-model-light.png") });
 await page.evaluate(() => { document.documentElement.dataset.theme = "dark"; });
 await page.screenshot({ path: resolve(output, "agent-model-dark.png") });
 await page.evaluate(() => { document.documentElement.dataset.theme = "light"; });
 for (const [width, height] of [[390, 844], [760, 480]]) {
  await page.setViewportSize({ width, height });
  await dialog.locator("form").evaluate(node => { node.scrollTop = 0; });
  const geometry = await dialog.evaluate(node => { const r = node.getBoundingClientRect(); return { left: r.left, right: r.right, bottom: r.bottom, width: innerWidth, height: innerHeight, scroll: node.scrollWidth, client: node.clientWidth }; });
  assert(geometry.left >= 0 && geometry.right <= geometry.width && geometry.bottom <= geometry.height && geometry.scroll <= geometry.client + 1);
  const visible = await dialog.getByLabel("默认模型服务", { exact: true }).evaluate(node => { const r = node.getBoundingClientRect(); return node.contains(document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2)); });
  assert(visible, "model selector must not be covered by sidebar");
  await page.screenshot({ path: resolve(output, `agent-model-${width}.png`) });
 }
 await page.setViewportSize({ width: 1440, height: 900 });
 await dialog.getByRole("button", { name: "MCP 服务", exact: true }).click();
 const customGuide = dialog.getByRole("region", { name: "自定义工具接入指南", exact: true });
 assert(await customGuide.isHidden(), "custom service guide stays collapsed until requested");
 await dialog.locator(".mcp-service-guide summary").focus(); await page.keyboard.press("Enter");
 const guideCommands = await customGuide.getByLabel("创建 MCP 服务命令", { exact: true }).textContent();
 assert(guideCommands.includes("npm --prefix mcp-service run init"));
 assert(guideCommands.includes("$env:USERPROFILE\\my-mcp-tools"));
 await page.screenshot({path:resolve(output,"mcp-service-guide-light.png")});
 await page.evaluate(()=>{document.documentElement.dataset.theme="dark";});
 await page.screenshot({path:resolve(output,"mcp-service-guide-dark.png")});
 await page.evaluate(()=>{document.documentElement.dataset.theme="light";});
 for (const [width,height] of [[390,844],[760,480]]) {
  await page.setViewportSize({width,height});
  const networkNote=customGuide.locator(".mcp-service-guide-network");
  await networkNote.scrollIntoViewIfNeeded();
  assert(await networkNote.evaluate(node=>{const r=node.getBoundingClientRect();return node.contains(document.elementFromPoint(r.left+r.width/2,r.top+r.height/2));}),"guide networking instructions remain reachable");
  assert(await dialog.evaluate(node=>node.scrollWidth<=node.clientWidth+1),"guide commands must fit narrow windows");
  await page.screenshot({path:resolve(output,`mcp-service-guide-${width}.png`)});
 }
 await page.setViewportSize({width:1440,height:900});
 await dialog.locator(".mcp-service-guide summary").focus(); await page.keyboard.press("Enter");
 assert(await customGuide.isHidden());
 assert.equal(mcpWrites.length,0); assert.equal(probeRequests.length,0);
 record("custom tool onboarding expands by keyboard with real scaffold commands, Docker guidance and accessible narrow layouts without requests");
 assert(await dialog.getByRole("button",{name:"诊断连接",exact:true}).isDisabled());
 const mcpForm=dialog.locator(".mcp-servers-body form");
 const formScroll=await mcpForm.evaluate(node=>({client:node.clientHeight,scroll:node.scrollHeight}));
 assert(formScroll.scroll>formScroll.client,"MCP settings form must expose its own vertical scroll area");
 await dialog.getByRole("button",{name:"诊断连接",exact:true}).scrollIntoViewIfNeeded();
 const diagnosisButtonVisible=await dialog.getByRole("button",{name:"诊断连接",exact:true}).evaluate(node=>{const r=node.getBoundingClientRect(),x=Math.max(0,Math.min(innerWidth-1,r.left+r.width/2)),y=Math.max(0,Math.min(innerHeight-1,r.top+r.height/2));return r.bottom<=innerHeight&&r.top>=0&&node===document.elementFromPoint(x,y)});
 assert(diagnosisButtonVisible,"MCP diagnosis action must be reachable by scrolling the form");
 await mcpForm.evaluate(node=>{node.scrollTop=0});
 await dialog.getByLabel("服务别名", { exact: true }).fill("project_docs");
 await dialog.getByLabel("服务地址", { exact: true }).fill("https://tools.example.com/mcp");
 await dialog.getByLabel("访问令牌（可选）", { exact: true }).fill("fixture-secret-token");
 await dialog.getByLabel("自定义认证头（可选）", { exact: true }).fill("X-Api-Key");
 await dialog.getByLabel("认证头值（可选）", { exact: true }).fill("fixture-header-secret");
 await dialog.getByRole("button", { name: "保存服务", exact: true }).click();
 await dialog.locator('aside button[aria-pressed="true"]').filter({ hasText: "project_docs" }).waitFor();
 assert.equal(mcpServers.length, 1); assert(mcpServers[0].hasBearer); assert(!JSON.stringify(mcpServers[0]).includes("fixture-secret-token"));
 assert.equal(await dialog.getByLabel("访问令牌（可选）", { exact: true }).inputValue(), "");
 assert.equal(await dialog.getByLabel("认证头值（可选）", { exact: true }).inputValue(), "");
 assert.equal(mcpWrites.at(-1).headerName, "X-Api-Key");assert.equal(mcpWrites.at(-1).headerValue, "fixture-header-secret");
 assert(mcpServers[0].hasHeader);assert(!JSON.stringify(mcpServers[0]).includes("fixture-header-secret"));
 assert(!await page.evaluate(()=>Object.values(localStorage).some(value=>value.includes("fixture-header-secret"))));
 await dialog.getByRole("button", { name: "保存服务", exact: true }).click();
 await page.waitForFunction(()=>!document.querySelector('.agent-platform-body fieldset')?.disabled);
 assert.equal(mcpWrites.at(-1).headerValue, "");assert(mcpServers[0].hasHeader);
 record("custom authentication header submits only on save, clears the secret input and preserves the saved key on blank edit");
 assert.equal(probeRequests.length,0,"saving does not automatically connect");
 await dialog.getByRole("button",{name:"诊断连接",exact:true}).click();
 const diagnosis=dialog.getByRole("region",{name:"MCP 连接诊断",exact:true});
 await diagnosis.getByText("连接成功，已读取工具目录",{exact:true}).waitFor();
 assert.equal(probeRequests.length,1);assert.equal(probeRequests[0].version,mcpServers[0].version);assert.equal(probeRequests[0].body,null);
 await diagnosis.getByLabel("搜索 MCP 工具",{exact:true}).fill("read");assert.equal(await diagnosis.locator("details").count(),1);
 await diagnosis.getByLabel("搜索 MCP 工具",{exact:true}).fill("missing");await diagnosis.getByText("没有匹配的工具。",{exact:true}).waitFor();
 await diagnosis.getByLabel("搜索 MCP 工具",{exact:true}).fill("");
 await diagnosis.locator("summary").first().focus();await page.keyboard.press("Enter");
 await diagnosis.getByLabel("search_docs 输入参数",{exact:true}).waitFor();assert((await diagnosis.getByLabel("search_docs 输入参数",{exact:true}).textContent()).includes('"required"'));
 assert.equal(await page.evaluate(()=>window.mcpInjected),undefined);assert.equal(await diagnosis.locator("script").count(),0);
 record("explicit MCP diagnosis pins saved version and previews escaped searchable tool schemas without executing tools");
 await diagnosis.scrollIntoViewIfNeeded();await page.screenshot({path:resolve(output,"mcp-inspection-light.png")});
 await page.evaluate(()=>{document.documentElement.dataset.theme="dark";});await page.screenshot({path:resolve(output,"mcp-inspection-dark.png")});await page.evaluate(()=>{document.documentElement.dataset.theme="light";});
 for(const [width,height] of [[390,844],[760,480]]){
  await page.setViewportSize({width,height});await diagnosis.getByLabel("search_docs 输入参数",{exact:true}).scrollIntoViewIfNeeded();
  const geometry=await dialog.evaluate(node=>{const r=node.getBoundingClientRect();return r.left>=0&&r.right<=innerWidth&&r.bottom<=innerHeight&&node.scrollWidth<=node.clientWidth+1;});assert(geometry,"MCP diagnostic fits narrow and short windows");
  await page.screenshot({path:resolve(output,`mcp-inspection-${width}.png`)});
 }
 await page.setViewportSize({width:1440,height:900});record("MCP diagnostic keyboard expansion and light/dark narrow layouts remain usable");
 await dialog.getByLabel("服务地址",{exact:true}).fill("https://changed.example.com/mcp");assert(await dialog.getByRole("button",{name:"诊断连接",exact:true}).isDisabled());assert.equal(await diagnosis.count(),0);
 await dialog.getByLabel("服务地址",{exact:true}).fill(mcpServers[0].url);probeCode="forbidden";
 await dialog.getByRole("button",{name:"诊断连接",exact:true}).click();await diagnosis.getByText("服务拒绝访问，请检查权限或服务端策略。",{exact:true}).waitFor();assert(await page.evaluate(()=>!!localStorage.getItem("proofcode.session")));assert.equal(await diagnosis.locator("details").count(),0);probeCode="ok";
 record("unsaved MCP edits disable diagnosis and remote 403 does not log the user out");
 mcpServers[0].version++;
 await dialog.getByRole("button",{name:"诊断连接",exact:true}).click();await dialog.getByRole("alert").waitFor();assert.equal(await diagnosis.count(),0);
 await dialog.getByRole("button",{name:"重新加载服务",exact:true}).click();await page.waitForFunction(()=>!document.querySelector('.agent-platform-body fieldset')?.disabled);
 failMcpLoad=true;await dialog.getByRole("button",{name:"重新加载服务",exact:true}).click();await dialog.getByRole("alert").waitFor();assert(await dialog.getByRole("button",{name:"保存服务",exact:true}).isDisabled());assert(await dialog.getByRole("button",{name:"诊断连接",exact:true}).isDisabled());
 failMcpLoad=false;await dialog.getByRole("button",{name:"重新加载服务",exact:true}).click();await page.waitForFunction(()=>!document.querySelector('.agent-platform-body fieldset')?.disabled);
 await dialog.getByRole("button",{name:"诊断连接",exact:true}).click();await diagnosis.getByText("连接成功，已读取工具目录",{exact:true}).waitFor();assert.equal(probeRequests.at(-1).version,mcpServers[0].version);
 record("stale MCP diagnostics require reload and failed reload blocks editing until recovery");
 await dialog.locator("form").evaluate(node => { node.scrollTop = 0; });
 await page.screenshot({ path: resolve(output, "mcp-services-light.png") });
 for (const [width,height] of [[390,844],[760,480]]) {
  await page.setViewportSize({width,height});
  await dialog.getByLabel("服务地址",{exact:true}).scrollIntoViewIfNeeded();
  const visible=await dialog.getByLabel("服务地址",{exact:true}).evaluate(node=>{const r=node.getBoundingClientRect();return node.contains(document.elementFromPoint(r.left+r.width/2,r.top+r.height/2));});assert(visible);
  await page.screenshot({path:resolve(output,`mcp-services-${width}.png`)});
 }
 await page.setViewportSize({width:1440,height:900});
 record("user-defined HTTP MCP service saves without returning its token");
 assert.equal(oauthRequests.length, 0, "collapsed OAuth settings must not load or connect automatically");
 const oauthPanel = dialog.getByRole("region", { name: "MCP OAuth 设置", exact: true });
 await dialog.locator(".mcp-oauth-settings summary").focus(); await page.keyboard.press("Enter");
 await oauthPanel.getByLabel("OAuth 客户端 ID", { exact: true }).fill("fixture-client");
 await oauthPanel.getByLabel("OAuth 客户端密钥（可选）", { exact: true }).fill("fixture-oauth-secret");
 await oauthPanel.getByLabel("OAuth 权限范围（可选）", { exact: true }).fill("docs.read offline_access");
 assert(await oauthPanel.getByRole("button", { name: "连接 OAuth", exact: true }).isDisabled());
 await oauthPanel.getByRole("button", { name: "保存 OAuth 配置", exact: true }).click();
 await page.waitForFunction(() => document.querySelector('.mcp-oauth-settings input[type="password"]')?.placeholder === "已保存，留空保留");
 await page.waitForFunction(() => document.querySelector('.mcp-oauth-settings input[type="password"]')?.value === "");
 assert.equal(oauthRequests.filter(value => value.method === "PUT").length, 1);
 assert(oauthRequests.find(value => value.method === "PUT").body.includes("fixture-oauth-secret"));
 assert(!await page.evaluate(() => Object.values(localStorage).some(value => value.includes("fixture-oauth-secret"))));
 assert(!JSON.stringify(oauthStates.get(mcpServers[0].id)).includes("fixture-oauth-secret"));
 assert.equal(await oauthPanel.getByLabel("OAuth 回调地址", { exact: true }).inputValue(), "http://localhost:8080/api/mcp/oauth/callback");
 record("OAuth configuration saves a versioned client and scope, clears secret input and never stores credentials in browser storage");
 await oauthPanel.getByRole("button", { name: "连接 OAuth", exact: true }).click();
 await oauthPanel.getByRole("link", { name: "打开授权页面", exact: true }).waitFor();
 assert.equal(await oauthPanel.getByRole("link", { name: "打开授权页面", exact: true }).getAttribute("href"), "https://oauth.example.test/authorize?state=fixture");
 await oauthPanel.getByRole("button", { name: "取消授权", exact: true }).click();
 await oauthPanel.getByText("已取消授权", { exact: true }).waitFor();
 assert.equal(await oauthPanel.getByRole("link").count(), 0);
 await oauthPanel.getByRole("button", { name: "连接 OAuth", exact: true }).click();
 oauthStates.get(mcpServers[0].id).state = "AUTHORIZED";
 await oauthPanel.getByText("已连接", { exact: true }).waitFor();
 assert.equal(await oauthPanel.getByRole("link").count(), 0);
 assert.equal(oauthRequests.filter(value => value.path.endsWith("/begin")).length, 2);
 record("OAuth popup fallback, cancellation, reconnection and successful status polling work without external authorization requests");
 await page.screenshot({ path: resolve(output, "mcp-oauth-light.png") });
 await page.evaluate(() => { document.documentElement.dataset.theme = "dark"; });
 await page.screenshot({ path: resolve(output, "mcp-oauth-dark.png") });
 await page.evaluate(() => { document.documentElement.dataset.theme = "light"; });
 for (const [width, height] of [[390,844], [760,480]]) {
  await page.setViewportSize({ width, height });
  const connect = oauthPanel.getByRole("button", { name: "连接 OAuth", exact: true });
  await connect.scrollIntoViewIfNeeded();
  assert(await connect.evaluate(node => { const r = node.getBoundingClientRect(); return node.contains(document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2)); }), "OAuth connection remains reachable");
  assert(await dialog.evaluate(node => node.scrollWidth <= node.clientWidth + 1), "OAuth settings must not overflow horizontally");
  await page.screenshot({ path: resolve(output, `mcp-oauth-${width}.png`) });
 }
 await page.setViewportSize({ width:1440, height:900 });
 await dialog.getByLabel("服务地址", { exact: true }).fill("https://unsaved.example.test/mcp");
 assert(await oauthPanel.getByRole("button", { name: "连接 OAuth", exact: true }).isDisabled());
 await dialog.getByLabel("服务地址", { exact: true }).fill(mcpServers[0].url);
 record("OAuth settings support keyboard expansion and light/dark narrow layouts; unsaved server edits block authorization");
 await dialog.getByRole("button", { name: "智能体", exact: true }).click();
 await dialog.locator("aside button").filter({ hasText: "代码审查员" }).click();
 await dialog.getByLabel("project_docs", { exact: false }).check();
 await dialog.getByRole("button", { name: "保存", exact: true }).click();
 assert.deepEqual(agents[0].profile.remoteMcpIds, [mcpServers[0].id]);
 record("custom MCP can be selected into a user agent profile");
 await dialog.getByRole("button", { name: "关闭智能体设置" }).click();
 await page.getByRole("combobox", { name: "当前智能体" }).selectOption(agents[0].id);
 await page.waitForFunction(() => !document.querySelector('[aria-label="当前智能体"]').disabled);
 assert.equal(selection, agents[0].id);
 const providerSelect = page.getByRole("combobox", { name: "模型服务", exact: true });
 const modelSelect = page.getByRole("combobox", { name: "模型", exact: true });
 await page.waitForFunction(() => document.querySelector('[aria-label="模型"]')?.value === "review-model");
 assert(await providerSelect.isDisabled() && await modelSelect.isDisabled());
 assert.equal(await providerSelect.inputValue(), "specialist");
 record("bound agent visibly locks composer to its own provider and model");
 await page.reload();
 await page.waitForFunction(() => document.querySelector('[aria-label="当前智能体"]')?.value.startsWith("resource-"));
 record("conversation agent selection survives reload");
 await page.waitForFunction(() => document.querySelector('[aria-label="模型"]')?.value === "review-model");
 await page.getByRole("textbox", { name: "消息内容" }).fill("验证智能体绑定请求");
 await page.locator(".run-button").click();
 await page.waitForFunction(() => document.body.textContent.includes("Fixture: recorded submission"));
 assert.equal(submitted.length, 1);
 assert.equal(submitted[0].model, "review-model"); assert.equal(submitted[0].providerId, "specialist");
 record("task POST contains bound model/provider (fixture records request; no model execution)");
 await page.getByRole("combobox", { name: "当前智能体" }).selectOption("");
 await page.waitForFunction(() => !document.querySelector('[aria-label="模型"]').disabled && document.querySelector('[aria-label="模型"]').value === "fixture-model");
 assert.equal(await providerSelect.inputValue(), "provider");
 await page.getByRole("combobox", { name: "当前智能体" }).selectOption(agents[0].id);
 await page.getByRole("button", { name: "智能体与记忆", exact: true }).click();
 await dialog.locator("aside button").filter({ hasText: "代码审查员" }).click();
 await dialog.getByLabel("默认模型", { exact: true }).selectOption("code-model");
 await dialog.getByRole("button", { name: "保存", exact: true }).click();
 await dialog.getByRole("button", { name: "关闭智能体设置" }).click();
 await page.waitForFunction(() => document.querySelector('[aria-label="模型"]')?.value === "code-model");
 record("inherit restores manual model; editing selected agent updates effective binding");
 const completeProviders = structuredClone(providers);
 providers[1].models = providers[1].models.filter(v => v.id !== "code-model");
 await page.evaluate(() => window.dispatchEvent(new Event("proofcode:providers-changed")));
 await page.getByText("智能体模型不可用，请在“智能体与记忆”重新配置").waitFor();
 assert(await page.locator(".run-button").isDisabled()); assert.equal(await modelSelect.inputValue(), "code-model");
 providers = providers.filter(v => v.id !== "specialist");
 await page.evaluate(() => window.dispatchEvent(new Event("proofcode:providers-changed")));
 await providerSelect.getByRole("option", { name: "智能体模型服务已不可用" }).waitFor({ state: "attached" });
 assert(await page.locator(".run-button").isDisabled()); assert.equal(submitted.length, 1);
 providers = completeProviders;
 await page.evaluate(() => window.dispatchEvent(new Event("proofcode:providers-changed")));
 await page.waitForFunction(() => !document.querySelector(".run-button").disabled);
 record("deleted provider/catalog entry blocks send without substituting another model");
 failSelection = true;
 await page.getByRole("combobox", { name: "当前智能体" }).selectOption("");
 await page.getByRole("alert").waitFor();
 assert.equal(await page.getByRole("combobox", { name: "当前智能体" }).inputValue(), agents[0].id);
 failSelection = false; record("failed selection preserves confirmed agent");
 await page.getByRole("button", { name: "智能体与记忆", exact: true }).click();
 await dialog.getByRole("button", { name: "MCP 服务", exact: true }).click();
 await dialog.locator("aside button").filter({ hasText: "project_docs" }).click();
 assert.equal(await dialog.getByLabel("访问令牌（可选）", { exact: true }).inputValue(), "");
 await dialog.getByLabel("清除已保存的访问令牌", { exact: true }).check();
 await dialog.getByLabel("清除已保存的自定义认证头", { exact: true }).check();
 await dialog.getByLabel("启用服务", { exact: true }).uncheck();
 await dialog.getByRole("button", { name: "保存服务", exact: true }).click();
 await dialog.locator('aside button[aria-pressed="true"]').filter({ hasText: "已停用" }).waitFor();
 assert(!mcpServers[0].enabled && !mcpServers[0].hasBearer);
 assert(!mcpServers[0].hasHeader);assert.equal(mcpServers[0].headerName,null);
 await dialog.getByRole("button", { name: "智能体", exact: true }).click();
 await dialog.locator("aside button").filter({ hasText: "代码审查员" }).click();
 const disabledService = dialog.getByLabel("project_docs（已停用）", { exact: false });
 assert(await disabledService.isChecked()); await disabledService.uncheck();
 assert(await disabledService.isDisabled());
 await dialog.getByRole("button", { name: "保存", exact: true }).click();
 await dialog.getByRole("button", { name: "MCP 服务", exact: true }).click();
 await dialog.locator("aside button").filter({ hasText: "project_docs" }).click();
 page.once("dialog", value => value.accept());
 await dialog.getByRole("button", { name: "删除服务", exact: true }).click();
 await dialog.locator("aside").getByText("连接自己的工具服务，再在智能体中选择。").waitFor();
 assert.equal(mcpServers.length, 0); assert.deepEqual(agents[0].profile.remoteMcpIds, []);
 record("MCP edit clears credentials, disables selection and supports unbinding/deletion");
 await dialog.getByRole("button", { name: "关闭智能体设置" }).click();
 await page.getByRole("textbox", { name: "消息内容" }).fill("检查测试接口");
 await page.getByRole("button", { name: "智能体与记忆", exact: true }).click();
 await dialog.getByRole("button", { name: "项目记忆", exact: true }).click();
 await dialog.getByLabel("名称", { exact: true }).fill("项目测试规范");
 await dialog.getByLabel("记忆内容").fill("新增接口使用 pytest，并验证参数错误的响应。");
 await dialog.getByLabel("检索关键词").fill("测试 接口 pytest");
 await dialog.getByLabel("始终参考这条偏好").check();
 await dialog.getByRole("button", { name: "保存", exact: true }).click();
 await dialog.getByRole("button", { name: "预览本次引用" }).click();
 await dialog.getByRole("region", { name: "本次记忆引用" }).getByText("项目测试规范").waitFor();
 assert(memories[0].memory.enabled && memories[0].memory.pinned); record("preferences save with enabled/pinned state and preview exact source");
 await page.screenshot({ path: resolve(output, "memories-light.png") });
 await page.evaluate(() => { document.documentElement.dataset.theme = "dark"; });
 await page.screenshot({ path: resolve(output, "memories-dark.png") });
 for (const [width, height] of [[390, 844], [760, 480]]) {
  await page.setViewportSize({ width, height });
  const geometry = await dialog.evaluate(node => { const rect = node.getBoundingClientRect(); return { left: rect.left, right: rect.right, bottom: rect.bottom, width: innerWidth, height: innerHeight, scroll: node.scrollWidth, client: node.clientWidth }; });
  assert(geometry.left >= 0 && geometry.right <= geometry.width && geometry.bottom <= geometry.height && geometry.scroll <= geometry.client + 1);
  await page.screenshot({ path: resolve(output, `memories-${width}.png`) });
 }
 record("light/dark and narrow/short windows keep settings within viewport");
 await dialog.getByLabel("启用", { exact: true }).uncheck();
 await dialog.getByRole("button", { name: "保存", exact: true }).click();
 await dialog.getByRole("button", { name: "预览本次引用" }).click();
 await dialog.getByText("没有匹配的已启用记忆。").waitFor();
 record("disabled memory disappears from preview");
 page.once("dialog", d => d.accept());
 await dialog.getByRole("button", { name: "删除", exact: true }).click();
 await page.waitForFunction(() => !document.querySelector(".agent-platform-body aside strong"));
 assert.equal(memories.length, 0); record("memory deletion removes stored item");
 await page.keyboard.press("Escape");
 await dialog.waitFor({ state: "hidden" });
 assert.equal(await page.evaluate(() => document.activeElement.textContent), "智能体与记忆");
 assert.equal(errors.length, 0, errors.join("\n")); record("keyboard close restores focus and no React errors occur");
 const report = { results, requests: requests.length, recordedTaskSubmissions: submitted.length, remainingMcpServices: mcpServers.length, modelCalls: 0, screenshots: output };
 await writeFile(resolve(output, "verification.json"), JSON.stringify(report, null, 2));
 console.log(JSON.stringify(report));
} catch (error) { await page.screenshot({path:resolve('.proofcode/agent-platform-qa/failure.png')}); console.error(await page.locator('[role=dialog]').allTextContents()); throw error; } finally { await browser.close(); }
