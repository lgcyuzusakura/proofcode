import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
const { chromium } = createRequire(import.meta.url)(process.env.PROOFCODE_PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({ channel: "msedge", headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, acceptDownloads: true });
const page = await context.newPage(), errors = [], requests = [], mutations = [];
page.on("pageerror", error => { errors.push(error.message); console.error(error.stack); });
const output = resolve(".proofcode/history-qa");await mkdir(output, { recursive: true });
const now = "2026-10-21T05:00:00Z";
const projects = [{ id: "project-first", name: "当前工程", defaultBranch: "main", sourceKind: "REMOTE_REPOSITORY", localHandle: "desktop:review-qa" }, { id: "project-second", name: "历史工程", defaultBranch: "main", sourceKind: "REMOTE_REPOSITORY", localHandle: "desktop:review-qa" }];
const workspaces = projects.map((project, index) => ({ id: `workspace-${index}`, projectId: project.id, name: "开发工作区", kind: project.sourceKind, defaultWorkspace: true, createdAt: now }));
const conversations = workspaces.map((workspace, index) => ({ id: `conversation-${index}`, projectId: workspace.projectId, workspaceId: workspace.id, title: index ? "上下文与接口排查" : "当前对话", defaultConversation: true, createdAt: now }));
const tasks = conversations.map((conversation, index) => ({ id: `task-${index}`, projectId: conversation.projectId, workspaceId: conversation.workspaceId, conversationId: conversation.id, sourceSnapshotId: "snapshot-source", prompt: index ? "修复上下文检索" : "当前工程任务", model: "fixture-model", executionMode: "CODE", attempt: index ? 2 : 1, status: "SUCCEEDED", createdAt: now }));
const messages = conversations.map((conversation, chat) => Array.from({length: 44}, (_, index) => ({ id: `message-${chat}-${index}`, sequence: index + 1, role: index % 2 ? "assistant" : "user", taskId: tasks[chat].id, attempt: 1, content: `历史内容 ${index}：${chat ? "上下文检索和接口分析。" : "当前工程对话。"}\n\n` + "用于验证定位后仍能上下滚动。".repeat(10), status: "COMPLETED", createdAt: now })));
const scope = { projectId: projects[0].id, workspaceId: workspaces[0].id, conversationId: conversations[0].id };
const hit = (index = 0) => ({kind: "MESSAGE",id: `message-1-${index}`,projectId: projects[1].id,projectName: projects[1].name,workspaceId: workspaces[1].id,conversationId: conversations[1].id,conversationTitle: conversations[1].title,taskId: tasks[1].id,messageId: `message-1-${index}`,role: index % 2 ? "assistant" : "user",excerpt: index === 0 ? "上下文污染 <script>window.historyInjected = true</script>" : `接口历史片段 ${index}`,createdAt: now});
const oldArtifact = {id: "old-artifact",taskId: tasks[1].id,attempt: 1,kind: "recovery",commitHash: "abcdef012345",branch: "attempt-1",createdAt: now};
const currentArtifact = {...oldArtifact,id: "current-artifact",attempt: 2,kind: "checkpoint",branch: "attempt-2"};
let failSearch = false, failHistory = false, slowQueryStarted, releaseSlow;
await context.addInitScript(({scope}) => {localStorage.setItem("proofcode.session", "fixture");if (!localStorage.getItem("proofcode.selected-scope")) localStorage.setItem("proofcode.selected-scope", JSON.stringify(scope));window.reviewBridgeCalls=[];window.go={main:{App:{GetProjectCommand:async()=>({}),ListProjectFiles:async()=>[],ReadProjectFile:async(_handle,path)=>({path,hash:"original",content:"beforeValue\n"}),CaptureProjectSource:async handle=>{window.reviewBridgeCalls.push(["capture",handle]);return {manifestHash:"current-manifest"};},ApplyProjectPatch:async(handle,manifest,patch)=>{window.reviewBridgeCalls.push(["apply",handle,manifest,patch]);return {applied:true,fileCount:1,path:"fixture"};}}}};}, {scope});
await page.route("**/api/**", async route => {
 const request = route.request(), url = new URL(request.url()), path = url.pathname;requests.push(path);
 if (request.method() !== "GET" && !path.endsWith("/default-scope")) mutations.push(path);
 let body = {};
 if (path === "/api/auth/me") body = {id: "user",username: "fixture"};
 else if (path === "/api/projects") body = projects;
 else if (path === "/api/history/search") {
  if (failSearch) return route.fulfill({status: 503,json: {message: "搜索服务暂不可用"}});
  const query = url.searchParams.get("query");
  if(query === "slow") { slowQueryStarted?.(); await new Promise(resolve => {releaseSlow = resolve;});try {return await route.fulfill({json: {items: [{...hit(),excerpt: "过期搜索结果"}],nextCursor: null}});}catch{return;} }
  body = {items: query === "empty" ? [] : url.searchParams.has("cursor") ? [hit(0),hit(2)] : [hit(0),hit(1)],nextCursor: query === "empty" || url.searchParams.has("cursor") ? null : "next-page"};
 }
 else if (path.endsWith("/default-scope")) {const i=path.includes(projects[1].id)?1:0;body={projectId:projects[i].id,workspaceId:workspaces[i].id,conversationId:conversations[i].id};}
 else if (path.endsWith("/workspaces")) body = [workspaces[path.includes(projects[1].id)?1:0]];
 else if (path.endsWith("/conversations")) body = [conversations[path.includes(projects[1].id)?1:0]];
 else if (path.endsWith("/messages")) body = messages[path.includes(projects[1].id)?1:0];
 else if (path === `/api/projects/${projects[0].id}/sources/snapshot-source`) body = {id:"snapshot-source",manifestHash:"task-manifest"};
 else if (path.endsWith("/context")) body = {version:0,view:{mode:"AUTO",items:[],packIds:[]},messages:[],packs:[],excludedTurns:0,estimatedTokens:0,selectionHash:"fixture",estimateMethod:"fixture"};
 else if (path.includes("/agent-platform/selection/")) body = {profileId:null};
 else if (path.includes("/agent-platform/") || path.endsWith("/skills") || path.endsWith("/mcp-servers")) body = [];
 else if (path === "/api/model-providers") body = [{id:"fixture",name:"Fixture",models:[{id:"fixture-model",name:"Fixture"}]}];
 else if (path === "/api/tasks") body = [tasks[url.searchParams.get("projectId") === projects[1].id?1:0]];
 else if (path.endsWith("/history")) {
  if(failHistory)return route.fulfill({status:503,json:{message:"恢复记录读取失败"}});
  const older=url.searchParams.has("beforeSequence");body={taskId:tasks[1].id,currentAttempt:2,status:"SUCCEEDED",entries:older?[{sequence:1,attempt:1,type:"task.started",title:"开始执行",timestamp:now}]:[{sequence:8,attempt:2,type:"task.completed",title:"任务完成",timestamp:now},{sequence:4,attempt:1,type:"recovery.created",title:"生成恢复补丁",timestamp:now}],artifacts:older?[]:[currentArtifact,oldArtifact],nextBeforeSequence:older?null:4};
 }
 else if(path.includes("/artifacts/by-id/"))body={...oldArtifact,patch:"diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-oldValue\n+recoveredValue\n"};
 else if(path.endsWith("/artifacts/checkpoint"))body={...currentArtifact,patch:"diff --git a/main.go b/main.go\nindex 0123456..abcdef0 100644\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-oldValue\n+currentValue\n@@ -10 +10 @@\n-beforeValue\n+secondChange\n"};
 else if(path.endsWith("/artifacts"))body=path.includes(tasks[1].id)?[currentArtifact]:[];
 else if(path.endsWith("/events"))body=path.includes(tasks[1].id)?[
  {sequence:1,attempt:2,runId:"review-run",type:"task.started",timestamp:now,payload:{}},
  {sequence:2,attempt:2,runId:"review-run",type:"agent.message.delta",timestamp:now,payload:{step:3,delta:"当前正在检查补丁 "}},
  {sequence:2,attempt:2,runId:"review-run",type:"agent.message.delta",timestamp:now,payload:{step:3,delta:"当前正在检查补丁 "}},
  {sequence:3,attempt:2,runId:"review-run",type:"agent.message.delta",timestamp:now,payload:{step:3,delta:"冲突和验证结果。"}},
  {sequence:5,attempt:2,runId:"review-run",type:"workspace.updated",timestamp:now,payload:{state:"ready",patch:"diff --git a/live.txt b/live.txt\n--- /dev/null\n+++ b/live.txt\n@@ -0,0 +1 @@\n+liveChange\n",sha256:"a".repeat(64)}},
  {sequence:4,attempt:1,runId:"old-run",type:"agent.message.completed",timestamp:now,payload:{content:"旧任务迟到的完成回复"}}
 ]:[];
 else if(path.endsWith("/approvals"))body=[];
 await route.fulfill({json:body});
});
const results = [], record = message => {results.push(message);console.log(`PASS ${message}`);};
try {
 await page.goto(process.env.PROOFCODE_UI_URL || "http://127.0.0.1:5187");await page.locator('[data-message-id="message-0-43"]').waitFor();
 await page.getByRole("textbox",{name:"消息内容",exact:true}).fill("保留当前对话草稿");
 await page.getByRole("button",{name:"搜索所有对话内容",exact:true}).click();
 const dialog=page.getByRole("dialog",{name:"搜索历史",exact:true}), query=dialog.getByRole("textbox",{name:"搜索历史内容",exact:true});
 await dialog.locator(".history-hit").first().waitFor();assert(await query.evaluate(node=>document.activeElement===node));
 assert.equal(await dialog.locator("script").count(),0);assert.equal(await page.evaluate(()=>window.historyInjected),undefined);
 await dialog.getByRole("button",{name:"加载更多结果",exact:true}).click();await page.waitForFunction(()=>document.querySelectorAll('.history-hit').length===3);record("account history results are searchable, escaped and deduplicated across pages");
 await query.fill("empty");await dialog.getByText("没有找到匹配内容，试试更短的关键词。",{exact:true}).waitFor();
 const slowStarted=new Promise(resolve=>{slowQueryStarted=resolve;});await query.fill("slow");await slowStarted;await query.fill("new");await dialog.locator(".history-hit").first().waitFor();releaseSlow();await page.waitForTimeout(100);assert.equal(await dialog.getByText("过期搜索结果",{exact:true}).count(),0);record("empty search and out-of-order responses never restore obsolete results");
 await dialog.getByRole("combobox",{name:"历史搜索工程",exact:true}).selectOption(projects[1].id);await dialog.getByRole("combobox",{name:"历史搜索类型",exact:true}).selectOption("MESSAGE");await dialog.locator(".history-hit").first().waitFor();
 await page.screenshot({path:resolve(output,"history-search-light.png")});await page.evaluate(()=>{document.documentElement.dataset.theme="dark";});await page.screenshot({path:resolve(output,"history-search-dark.png")});await page.evaluate(()=>{document.documentElement.dataset.theme="light";});
 for(const [width,height] of [[390,844],[760,480]]){await page.setViewportSize({width,height});assert(await dialog.evaluate(node=>{const r=node.getBoundingClientRect();return r.left>=0&&r.right<=innerWidth&&r.bottom<=innerHeight&&node.scrollWidth<=node.clientWidth+1;}));await page.screenshot({path:resolve(output,`history-search-${width}.png`)});}
 await page.setViewportSize({width:1440,height:900});await page.keyboard.press("Escape");await dialog.waitFor({state:"hidden"});await page.waitForFunction(()=>document.activeElement===document.querySelector('[aria-label="搜索所有对话内容"]'));record("history search supports light/dark, narrow/short layouts and keyboard focus restoration");
 await page.getByRole("button",{name:"搜索所有对话内容",exact:true}).click();await dialog.locator(".history-hit").first().waitFor();await dialog.locator(".history-hit").first().click();await dialog.waitFor({state:"hidden"});
 const target=page.locator('[data-message-id="message-1-0"]');await target.waitFor();await page.waitForFunction(()=>document.querySelector('[data-message-id="message-1-0"]')?.dataset.searchHighlight==="true");
 assert((await page.evaluate(()=>JSON.parse(localStorage.getItem("proofcode.selected-scope")))).projectId===projects[1].id);
 assert(await target.evaluate(node=>{const r=node.getBoundingClientRect(),p=document.querySelector('.conversation-stream').getBoundingClientRect();return r.top<p.bottom&&r.bottom>p.top;}));assert.equal(await page.evaluate(()=>localStorage.getItem("proofcode.chat-draft.project-first.conversation-0")),"保留当前对话草稿");record("opening a result switches project, selects source task and reveals the original message while preserving drafts");
 await page.getByRole("button",{name:"审查",exact:true}).click();await page.getByRole("region",{name:"实时模型回复",exact:true}).getByText("当前正在检查补丁 冲突和验证结果。",{exact:true}).waitFor();await page.getByRole("region",{name:"实时工作区变更",exact:true}).getByText("+liveChange",{exact:true}).waitFor();await page.screenshot({path:resolve(output,"task-review-live-diff.png")});record("review shows current-attempt text streaming and persisted workspace diff without apply controls");await page.getByRole("button",{name:"恢复记录",exact:true}).click();const history=page.getByRole("region",{name:"任务恢复记录",exact:true});await history.getByText("生成恢复补丁",{exact:true}).waitFor();
 await history.getByRole("button",{name:"加载更早记录",exact:true}).click();await history.getByText("开始执行",{exact:true}).waitFor();assert.equal(await history.getByText("生成恢复补丁",{exact:true}).count(),1);
 await page.screenshot({path:resolve(output,"task-history-light.png")});await page.evaluate(()=>{document.documentElement.dataset.theme="dark";});await page.screenshot({path:resolve(output,"task-history-dark.png")});await page.evaluate(()=>{document.documentElement.dataset.theme="light";});
 await history.getByRole("button",{name:/查看恢复补丁/}).click();await page.getByText("历史产物 · 尝试 1 · 仅预览与下载",{exact:true}).waitFor();await page.getByText("+recoveredValue",{exact:true}).waitFor();assert.equal(await page.getByRole("button",{name:"批准应用所选变更",exact:true}).count(),0);
 const downloaded=page.waitForEvent("download");await page.getByRole("button",{name:"下载所选 patch",exact:true}).click();const historicalFile=await downloaded;assert.equal(historicalFile.suggestedFilename(),"task-1-attempt-1-recovery-selected.patch");assert((await readFile(await historicalFile.path(),"utf8")).includes("+recoveredValue"));record("recovery history spans attempts and downloads the exact historical patch without applying it");
 await page.getByRole("button",{name:"返回当前产物",exact:true}).click();await page.getByText("+currentValue",{exact:true}).waitFor();
 const reviewList=page.getByRole("region",{name:"逐文件和代码块审查",exact:true});await page.getByText("2 / 2 项已选",{exact:true}).waitFor();
 await page.getByRole("checkbox",{name:"选择代码块 main.go @@ -1 +1 @@",exact:true}).uncheck();await page.getByText("1 / 2 项已选",{exact:true}).waitFor();
 const selectedButton=page.getByRole("button",{name:"下载所选 patch",exact:true});if(!(await selectedButton.isEnabled()))throw Error(`selected patch unavailable: ${await reviewList.innerText()} | alerts=${await page.getByRole("alert").allTextContents()}`);const selectedDownload=page.waitForEvent("download");await selectedButton.click();const selectedFile=await selectedDownload;const selectedText=await readFile(await selectedFile.path(),"utf8");assert(selectedText.includes("+secondChange"));assert(!selectedText.includes("+currentValue"));
 await page.getByRole("button",{name:"全选",exact:true}).click();await page.getByText("2 / 2 项已选",{exact:true}).waitFor();record("review selects individual files and hunks and downloads only approved changes");
 await page.evaluate(()=>sessionStorage.setItem("proofcode.editor:project-second",JSON.stringify({file:{path:"main.go",content:"beforeValue\n",hash:"original"},draft:"unsaved local edit\n"})));
 await page.getByRole("button",{name:"批准应用所选变更",exact:true}).click();await page.getByRole("alert").getByText(/还有未保存修改/).waitFor();
 assert.equal(await page.evaluate(()=>window.reviewBridgeCalls.filter(call=>call[0]==="apply").length),0);assert.equal(await page.evaluate(()=>JSON.parse(sessionStorage.getItem("proofcode.editor:project-second")).draft),"unsaved local edit\n");
 await page.getByRole("button",{name:"打开代码页",exact:true}).click();await page.getByRole("heading",{name:"代码",exact:true}).waitFor();await page.getByText("已恢复此工程未保存的编辑草稿",{exact:false}).waitFor();record("review refuses to apply over a same-file editor draft, preserves it and opens the editor");
 await page.evaluate(()=>sessionStorage.removeItem("proofcode.editor:project-second"));await page.getByRole("button",{name:"审查",exact:true}).click();await page.getByRole("button",{name:"批准应用所选变更",exact:true}).click();await page.getByRole("button",{name:"已应用到工程",exact:true}).waitFor();
 const applyCall=await page.evaluate(()=>window.reviewBridgeCalls.find(call=>call[0]==="apply"));assert.equal(applyCall[2],"current-manifest");assert(applyCall[3].includes("+currentValue"));record("saving/removing the draft allows a retry against the current desktop manifest");
 await page.getByRole("button",{name:"恢复记录",exact:true}).click();await history.getByText("生成恢复补丁",{exact:true}).waitFor();failHistory=true;await history.getByRole("button",{name:"刷新记录",exact:true}).click();await history.getByRole("alert").waitFor();failHistory=false;await history.getByRole("button",{name:"刷新记录",exact:true}).click();await history.getByText("生成恢复补丁",{exact:true}).waitFor();
 for(const [width,height] of [[390,844],[760,480]]){await page.setViewportSize({width,height});await history.getByRole("button",{name:/查看恢复补丁/}).scrollIntoViewIfNeeded();assert(await history.evaluate(node=>node.scrollWidth<=node.clientWidth+1));await page.screenshot({path:resolve(output,`task-history-${width}.png`)});}await page.setViewportSize({width:1440,height:900});record("history errors are retryable and timeline/artifact controls remain reachable in narrow windows");
 await page.getByRole("button",{name:"搜索所有对话内容",exact:true}).click();await dialog.locator(".history-hit").first().waitFor();failSearch=true;await query.fill("failure");await dialog.getByRole("alert").waitFor();failSearch=false;await dialog.getByRole("button",{name:"重新搜索",exact:true}).click();await dialog.locator(".history-hit").first().waitFor();
 await page.evaluate(()=>{window.blockHistory=event=>event.preventDefault();window.addEventListener("proofcode:before-navigation",window.blockHistory);});await dialog.locator(".history-hit").first().click();assert(await dialog.isVisible());await page.evaluate(()=>window.removeEventListener("proofcode:before-navigation",window.blockHistory));await page.keyboard.press("Escape");record("search failures recover and navigation veto keeps the source page intact");
 tasks[1].attempt=3;tasks[1].status="QUEUED";
 const retryEvents=page.waitForResponse(response=>new URL(response.url()).pathname===`/api/tasks/${tasks[1].id}/events`);
 await page.reload();await page.getByRole("button",{name:"审查",exact:true}).click();await retryEvents;
 await page.getByRole("heading",{name:"代码审查",exact:true}).waitFor();
 await page.waitForFunction(()=>document.querySelector('.review-task-picker select')?.value==="task-1");
 assert.equal(await page.getByRole("region",{name:"实时模型回复",exact:true}).count(),0);
 assert.equal(await page.getByText("当前正在检查补丁 冲突和验证结果。",{exact:true}).count(),0);
 record("a queued retry never displays the previous attempt stream; replayed deltas and late old completions do not corrupt current text");
 assert.deepEqual(mutations,[]);assert.deepEqual(errors,[]);await writeFile(resolve(output,"verification.json"),JSON.stringify({results,modelCalls:0,mutations,errors,requests:requests.length},null,2));
}catch(error){await page.screenshot({path:resolve(output,"failure.png")});console.error(await page.getByRole("alert").allTextContents());throw error;}finally{releaseSlow?.();await browser.close();}
