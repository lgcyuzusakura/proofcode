import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
const { chromium } = createRequire(import.meta.url)("playwright");
const browser = await chromium.launch({ channel: "msedge", headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await context.newPage(), errors = [], results = [], writes = [];
page.on("pageerror", error => errors.push(error.message));
page.on("dialog", async dialog => { errors.push(`Unexpected native dialog: ${dialog.type()}`); await dialog.dismiss(); });
const output = resolve(".proofcode/conversation-management-qa"); await mkdir(output, {recursive:true});
const at = "2026-10-21T07:00:00Z";
const project = {id:"project",name:"对话管理验收",defaultBranch:"main",sourceKind:"REMOTE_REPOSITORY"};
const workspace = {id:"workspace",projectId:project.id,name:"开发",defaultWorkspace:true};
const chats = [
 {id:"default-chat",projectId:project.id,workspaceId:workspace.id,title:"默认对话",defaultConversation:true,createdAt:at},
 {id:"feature-chat",projectId:project.id,workspaceId:workspace.id,title:"上下文改造",defaultConversation:false,createdAt:at,archivedAt:null}
];
const scope = {projectId:project.id,workspaceId:workspace.id,conversationId:chats[1].id};
const task = {id:"task",...scope,prompt:"修复上下文选择",model:"fixture-model",executionMode:"CODE",status:"CANCELLED",attempt:1,createdAt:at};
const messages = [
 {id:"question",sequence:1,role:"user",content:task.prompt,taskId:task.id,attempt:1,status:"COMPLETED",createdAt:at},
 {id:"answer",sequence:2,role:"assistant",content:"已保存调查结果与检查点。",taskId:task.id,attempt:1,status:"CANCELLED",createdAt:at}
];
let failRename = false, blockArchive = false, delayMutation = false, releaseMutation, mutationStarted;
await context.addInitScript(scope => {localStorage.setItem("proofcode.session","fixture");localStorage.setItem("proofcode.selected-scope",JSON.stringify(scope));}, scope);
await page.route("**/api/**", async route => {
 const request=route.request(), url=new URL(request.url()), path=url.pathname;
 let body={};
 if(request.method()==="PATCH" && /\/conversations\/[^/]+$/.test(path)) {
  const input=request.postDataJSON();writes.push(input);
  if(delayMutation){mutationStarted?.();await new Promise(resolve=>releaseMutation=resolve);delayMutation=false;}
  if(input.title && failRename)return route.fulfill({status:503,json:{message:"名称保存失败"}});
  if(input.archived && blockArchive)return route.fulfill({status:409,json:{message:"请先等待此对话中的活动任务结束"}});
  const chat=chats.find(value=>path.endsWith(`/${value.id}`));assert(chat);
  if(input.title!==undefined)chat.title=input.title;
  if(input.archived!==undefined)chat.archivedAt=input.archived?at:null;
  return route.fulfill({json:chat});
 }
 if(request.method()!=="GET" && !path.endsWith("/default-scope"))writes.push({path,method:request.method()});
 if(path==="/api/auth/me")body={id:"user",username:"fixture"};
 else if(path==="/api/projects")body=[project];
 else if(path.endsWith("/default-scope"))body={...scope,conversationId:chats[0].id};
 else if(path.endsWith("/workspaces"))body=[workspace];
 else if(path.endsWith("/conversations"))body=chats;
 else if(path.endsWith("/messages"))body=path.includes(chats[1].id)?messages:[];
 else if(path.endsWith("/context"))body={version:0,view:{mode:"AUTO",items:[],packIds:[]},messages:[],packs:[],excludedTurns:0,estimatedTokens:0};
 else if(path.endsWith("/context-packs"))body=[];
 else if(path.includes("/agent-platform/selection/"))body={profileId:null};
 else if(path.includes("/agent-platform/") || path.endsWith("/skills") || path.endsWith("/mcp-servers"))body=[];
 else if(path==="/api/model-providers")body=[{id:"provider",name:"Fixture",models:[{id:"fixture-model",name:"Fixture"}]}];
 else if(path==="/api/tasks")body=url.searchParams.get("conversationId")===chats[1].id?[task]:[];
 else if(/\/(events|artifacts|approvals)$/.test(path))body=[];
 else if(path.endsWith("/history"))body={taskId:task.id,currentAttempt:1,status:task.status,entries:[],artifacts:[],nextBeforeSequence:null};
 return route.fulfill({json:body});
});
const record=text=>{results.push(text);console.log(`PASS ${text}`);};
const row=()=>page.locator(".conversation-row").filter({has:page.locator(`button.tree-conversation[title="${chats[1].title}"]`)});
const action=async name=>{await row().locator("summary").click();await row().getByRole("button",{name,exact:true}).click();};
const composer=()=>page.getByRole("textbox",{name:"消息内容",exact:true});
try {
 await page.goto(process.env.PROOFCODE_UI_URL||"http://127.0.0.1:5188");
 await page.locator('[data-message-id="answer"]').waitFor();
 await composer().fill("保留我的聊天草稿");
 await action("重命名");
 let title=page.getByRole("textbox",{name:"对话名称",exact:true});
 assert(await title.evaluate(node=>document.activeElement===node));
 await title.fill("取消的名字");await title.press("Escape");
 assert.equal(await title.count(),0);assert.equal(chats[1].title,"上下文改造");
 failRename=true;await action("重命名");await title.fill("上下文与工具管理");await title.press("Enter");
 await row().getByRole("alert").waitFor();assert.equal(await title.inputValue(),"上下文与工具管理");
 failRename=false;await row().getByRole("button",{name:"保存名称",exact:true}).click();
 await title.waitFor({state:"hidden"});assert.equal(chats[1].title,"上下文与工具管理");
 assert.equal(await composer().inputValue(),"保留我的聊天草稿");record("inline rename supports keyboard cancel, preserves input on failure and keeps the chat draft");
 await page.reload();await page.locator('[data-message-id="answer"]').waitFor();
 assert.equal(await composer().inputValue(),"保留我的聊天草稿");
 await page.getByRole("button",{name:"上下文",exact:true}).click();
 await page.locator(".conversation-context").waitFor();
 blockArchive=true;await action("归档");await row().getByRole("button",{name:"确认归档",exact:true}).click();
 await row().getByRole("alert").waitFor();assert.equal(chats[1].archivedAt,null);
 blockArchive=false;await row().getByRole("button",{name:"确认归档",exact:true}).click();
 await page.locator(".archived-conversation-banner").waitFor();
 assert(await composer().isDisabled());assert(await page.getByRole("button",{name:"上下文",exact:true}).isDisabled());
 assert.equal(await page.locator(".conversation-context").count(),0);
 assert(await page.locator(".message-context-action").isDisabled());
 assert.equal(await page.locator(".chat-run-status").getByRole("button",{name:"重试",exact:true}).count(),0);
 assert(await row().isVisible());assert.equal(await composer().inputValue(),"保留我的聊天草稿");
 record("active-task rejection can retry; archive closes context editing and preserves readable history and drafts");
 await page.reload();await page.locator(".archived-conversation-banner").waitFor();await page.locator('[data-message-id="answer"]').waitFor();
 assert(await row().isVisible());assert(await composer().isDisabled());
 await page.getByRole("button",{name:"查看修改",exact:true}).click();
 await page.getByRole("heading",{name:"代码审查",exact:true}).waitFor();
 assert.equal(await page.locator(".review-task-picker").getByRole("button",{name:"重试",exact:true}).count(),0);
 await page.getByRole("button",{name:"恢复记录",exact:true}).click();await page.locator(".task-history").waitFor();
 record("archived scope survives reload and keeps review and recovery records available without retry");
 await row().getByRole("button",{name:chats[1].title,exact:true}).click();
 await page.locator('[data-message-id="answer"]').waitFor();
 for(const [width,height] of [[1440,900],[390,844],[760,480]]) {
  await page.setViewportSize({width,height});
  assert(await composer().isVisible());
  assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));
  await page.screenshot({path:resolve(output,`archived-${width}.png`)});
 }
 await page.setViewportSize({width:1440,height:900});
 await page.evaluate(()=>document.documentElement.dataset.theme="dark");
 await page.screenshot({path:resolve(output,"archived-dark.png")});
 record("archived view remains readable in dark, narrow and short layouts");
 await page.setViewportSize({width:390,height:844});
 await page.getByTitle("展开导航",{exact:true}).click();
 await row().waitFor();
 await action("重命名");await title.fill("窄屏里编辑的名称");
 await page.screenshot({path:resolve(output,"navigation-390.png")});
 assert(await title.isVisible());assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));
 await title.press("Escape");await page.keyboard.press("Escape");
 await page.getByTitle("展开导航",{exact:true}).waitFor();
 await page.setViewportSize({width:1440,height:900});if(await page.getByTitle("展开导航",{exact:true}).count())await page.getByTitle("展开导航",{exact:true}).click();
 record("mobile navigation opens a reachable conversation editor and Escape returns to the chat");
 await action("恢复对话");
 await row().getByRole("group",{name:"确认恢复对话",exact:true}).getByRole("button",{name:"恢复对话",exact:true}).click();
 await page.locator(".archived-conversation-banner").waitFor({state:"hidden"});assert(await composer().isEnabled());
 assert.equal(await composer().inputValue(),"保留我的聊天草稿");record("restore re-enables the conversation and retains the pre-archive draft");
 await action("重命名");await title.fill("延迟保存后的名称");
 const started=new Promise(resolve=>mutationStarted=resolve);delayMutation=true;
 await row().getByRole("button",{name:"保存名称",exact:true}).click();await started;
 await page.getByRole("button",{name:"默认对话",exact:true}).click();
 releaseMutation();await page.getByRole("button",{name:"延迟保存后的名称",exact:true}).waitFor();
 assert.equal(await page.locator(".context-chat-toolbar > span").textContent(),"默认对话");
 assert.equal(await composer().inputValue(),"");record("late mutation updates its row without switching the newly selected conversation");
 assert.deepEqual(errors,[]);assert(!writes.some(value=>value.path?.includes("/retry")||value.path?.endsWith("/context")||value.path==="/api/tasks"));
 await writeFile(resolve(output,"verification.json"),JSON.stringify({source:"local API fixtures, no model calls",results,errors},null,2));
} catch(error) {await page.screenshot({path:resolve(output,"failure.png")});console.error(await page.locator("body").innerText());throw error;}
finally {releaseMutation?.();await browser.close();}
