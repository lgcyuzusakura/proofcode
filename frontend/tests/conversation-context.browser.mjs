import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir } from "node:fs/promises";
import { resolve } from "node:path";

// UI fixtures only. Backend projection and snapshot semantics are verified in Java/Go.
const { chromium } = createRequire(import.meta.url)("playwright");
const browser = await chromium.launch({ channel: "msedge", headless: true, ignoreDefaultArgs: ["--hide-scrollbars"] });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await context.newPage();
const errors = []; page.on("pageerror", error => errors.push(error.message));
const project = { id: "project-context", name: "ProofCode 上下文工作台", defaultBranch: "main", sourceKind: "REMOTE_REPOSITORY" };
const workspace = { id: "workspace-context", projectId: project.id, name: "开发工作区", defaultWorkspace: true, kind: "REMOTE_REPOSITORY" };
const chat = { id: "chat-context", projectId: project.id, workspaceId: workspace.id, title: "登录模块与独立支线", defaultConversation: true };
const fork = { ...chat, id: "fork-context", title: "登录模块约束", defaultConversation: false };
const scope = { projectId: project.id, workspaceId: workspace.id, conversationId: chat.id };
const timestamp = "2026-10-14T01:00:00Z";
const messages = Array.from({ length: 40 }, (_, index) => ({ id: `message-${index}`, sequence: index + 1, taskId: `task-${Math.floor(index/2)}`, attempt: 1, role: index % 2 ? "assistant" : "user", content: index < 2 ? (index ? "周末天气是独立话题，和登录模块无关。" : "独立支线：周末天气 SECRET_TANGENT") : index % 2 ? `登录进度记录 ${Math.floor(index/2)+1}：保留 Java 17 和登录失败证据。\n\n下一步检查 session 校验和账户隔离。` : `登录配置检查 ${Math.floor(index/2)+1}：请检查登录模块，使用 Java 17。`, status: index % 2 ? "SUCCEEDED" : "COMPLETED", createdAt: timestamp }));
const tasks = messages.filter(item => item.role === "user").map(item => ({ id: item.taskId, ...scope, prompt: item.content, model: "fixture", executionMode: "CHAT", status: "SUCCEEDED", createdAt: timestamp }));
const provider = { id: "fixture-provider", name: "本地验收", models: [{ id: "fixture", name: "本地模型" }], modelSettings: {} };
let version = 0, view = { mode: "AUTO", items: [], packIds: [] }, packs = [], forked = false, submissions = 0;
let transfer;
const build = () => {
  const input = [];
  let excludedTurns = 0;
  for (let index=0;index<messages.length;index+=2) {
    const user = messages[index]; const item = view.mode === "FOCUS" ? view.items.find(value => value.messageId === user.id) : null;
    const state = item?.state || (view.mode === "FOCUS" ? "EXCLUDE" : "INCLUDE");
    if (state === "EXCLUDE") { excludedTurns++; continue; }
    for (const message of state === "REPLACE" ? [{...user,content:`[User-edited summary of an earlier turn]\n${item.replacement}`}] : messages.slice(index,index+2)) input.push({id:message.id,sequence:message.sequence,role:message.role,content:message.content,sourceConversationId:chat.id,sourceHash:"source-hash-fixture"});
  }
  return { version, view, messages: input, packs: packs.filter(pack => view.packIds.includes(pack.id)), excludedTurns, estimatedTokens: input.reduce((sum,item)=>sum+item.content.length*3+4,0), selectionHash: `selection-${version}`, estimateMethod: "UTF-8 byte bound; fixture" };
};
await context.addInitScript(scope => { localStorage.setItem("proofcode.session","fixture");localStorage.setItem("proofcode.selected-scope",JSON.stringify(scope)); },scope);
await page.route("**/api/**", async route => {
  const request = route.request(), url = new URL(request.url()), path = url.pathname; let body;
  if (path === "/api/auth/me") body = {id:"fixture",username:"local-context-qa"};
  else if (path === "/api/projects") body = [project];
  else if (path.endsWith("/default-scope")) body = scope;
  else if (path.endsWith("/workspaces")) body = [workspace];
  else if (path.endsWith("/conversations")) body = forked ? [chat,fork] : [chat];
  else if (path.endsWith("/messages")) body = path.includes(fork.id) ? [] : messages;
  else if (path.endsWith("/context-packs")) body = packs;
  else if (path.endsWith("/context/fork")) { transfer=request.postDataJSON();forked=true;body=fork; }
  else if (path.endsWith("/context/packs")) { transfer=request.postDataJSON();const pack={id:"pack-fixture",name:transfer.title,sourceConversationId:chat.id,messageCount:transfer.messageIds.length,stale:false};packs.push(pack);body=pack; }
  else if (path.endsWith("/context")) {
    if (request.method()==="PATCH") { const change=request.postDataJSON(); assert.equal(change.version,version); view=change.view;version++; }
    body=path.includes(fork.id) ? {...build(),messages:build().messages.filter(input=>transfer.messageIds.includes(input.id)),packs:[{id:"fork-pack",name:fork.title,sourceConversationId:chat.id,messageCount:transfer.messageIds.length,stale:false}]} : build();
  }
  else if (path === "/api/model-providers") body=[provider];
  else if (path === "/api/tasks") { if(request.method()==="POST")submissions++; body=path.includes("never")?[]:url.searchParams.get("conversationId")===fork.id?[]:tasks; }
  else if (path.endsWith("/events")||path.endsWith("/approvals")||path.endsWith("/artifacts")) body=[];
  else throw new Error(`Unexpected API ${request.method()} ${path}`);
  await route.fulfill({contentType:"application/json",body:JSON.stringify(body)});
});
const output=resolve(".tools/context-ui-2026-10-14");await mkdir(output,{recursive:true});
const checks=[];const record=value=>{checks.push(value);console.log(`PASS ${value}`);};
try {
  await page.goto(process.env.PROOFCODE_UI_URL || "http://127.0.0.1:5177");
  await page.locator(".message-row").first().waitFor();
  await page.locator("#open-conversation-context").click();
  const panel=page.getByLabel("对话上下文",{exact:true});await panel.waitFor();await panel.locator(".context-turn").first().waitFor();
  assert.equal(await panel.locator(".context-turn").count(),20);
  assert.equal(await panel.locator(".context-turn-top").filter({hasText:/第.*轮/}).count(),0);
  record("context drawer opens without turn numbers, default automatic mode");
  await panel.getByRole("button",{name:"手动",exact:true}).click();
  await panel.locator(".context-turn").first().getByText("已排除",{exact:true}).waitFor();
  await panel.getByLabel("包含搜索结果").check();
  await panel.locator(".context-turn").first().getByText("已包含",{exact:true}).waitFor();
  const first=panel.locator(".context-turn").first();
  await first.getByRole("button",{name:"排除问答",exact:true}).click();
  await first.getByText("已排除",{exact:true}).waitFor();assert(!JSON.stringify(build().messages).includes("SECRET_TANGENT"));
  assert(await page.locator(".message-row").first().textContent().then(text=>text.includes("SECRET_TANGENT")));
  record("excluding a turn changes input while keeping the transcript visible");
  await panel.getByRole("button",{name:"预览历史输入",exact:true}).click();
  assert(!(await panel.getByLabel("历史输入预览").textContent()).includes("SECRET_TANGENT"));
  record("input preview omits excluded question and answer");
  await panel.getByRole("button",{name:"自动",exact:true}).click();
  await first.getByText("自动纳入",{exact:true}).waitFor();
  assert(JSON.stringify(build().messages).includes("SECRET_TANGENT"));
  await panel.getByRole("button",{name:"手动",exact:true}).click();
  await first.getByText("已排除",{exact:true}).waitFor();
  assert(!JSON.stringify(build().messages).includes("SECRET_TANGENT"));
  record("automatic restores ordinary history and manual restores saved selection");
  await panel.getByRole("button",{name:"收起输入预览",exact:true}).click();
  await first.getByRole("button",{name:"恢复问答",exact:true}).click();await first.getByText("已包含",{exact:true}).waitFor();
  record("turn inclusion is reversible");
  await first.getByRole("button",{name:"写摘要",exact:true}).click();await panel.getByLabel("上下文摘要").fill("只保留登录模块要求：Java 17");
  await panel.getByRole("button",{name:"保存摘要",exact:true}).click();await first.getByText("使用摘要",{exact:true}).waitFor();
  assert(!JSON.stringify(build().messages).includes("SECRET_TANGENT"));assert(build().messages[0].content.includes("Java 17"));
  record("edited summary replaces the entire old turn");
  await panel.getByRole("button",{name:"撤销上次修改",exact:true}).click();await first.getByText("已包含",{exact:true}).waitFor();
  assert(JSON.stringify(build().messages).includes("SECRET_TANGENT"));record("undo restores the previous view without rewriting history");
  await panel.getByLabel("搜索上下文问答").fill("独立支线");assert.equal(await panel.locator(".context-turn").count(),1);
  await panel.getByLabel("包含搜索结果").uncheck();await first.getByText("已排除",{exact:true}).waitFor();
  await panel.getByLabel("搜索上下文问答").fill("");record("search results support bulk exclusion");
  await panel.getByRole("button",{name:"清空选择",exact:true}).click();await panel.locator(".context-turn").nth(1).getByText("已排除",{exact:true}).waitFor();
  await panel.locator(".context-turn").nth(1).getByRole("button",{name:"恢复问答",exact:true}).click();await panel.locator(".context-turn").nth(1).getByText("已包含",{exact:true}).waitFor();
  assert.equal(build().messages.length,2);record("focus mode sends only explicitly included complete turns");
  await panel.locator(".context-turn").nth(1).locator("input[type=checkbox]").check();
  await panel.getByLabel("上下文包名称").fill("登录模块约束");await panel.getByRole("button",{name:"保存为包",exact:true}).click();await panel.getByText("上下文包已保存，可以在其他对话中带入。").waitFor();
  assert.deepEqual(transfer.messageIds,["message-2","message-3"]);record("saved named pack contains exactly the chosen question and answer");
  const pack=panel.locator(".context-pack").first();await pack.getByRole("button",{name:"带入",exact:true}).click();await pack.getByRole("button",{name:"卸载",exact:true}).waitFor();
  await pack.getByRole("button",{name:"卸载",exact:true}).click();await pack.getByRole("button",{name:"带入",exact:true}).waitFor();record("saved packs can be mounted and detached");
  assert(await page.locator(".conversation-stream").evaluate(node=>node.scrollHeight>node.clientHeight && getComputedStyle(node).overflowY==="scroll"));
  assert(await panel.locator(".context-panel-scroll").evaluate(node=>node.scrollHeight>node.clientHeight));record("chat and context list retain independent vertical scrolling");
  await panel.locator(".context-panel-scroll").evaluate(node=>node.scrollTop=0);
  await page.screenshot({path:resolve(output,"desktop-context.png")});
  await page.getByTitle("切换深色外观").click();await page.screenshot({path:resolve(output,"dark-context.png")});await page.getByTitle("切换浅色外观").click();
  await page.setViewportSize({width:390,height:844});await page.screenshot({path:resolve(output,"mobile-context.png")});
  assert((await panel.boundingBox()).width<=390);assert(await page.locator(".context-mobile-scrim").isVisible());
  assert(await panel.locator(".context-panel-head strong").evaluate(node=>{ const box=node.getBoundingClientRect();return box.x>=0 && node.contains(document.elementFromPoint(box.x+2,box.y+2)); }));
  await page.keyboard.press("Escape");await panel.waitFor({state:"hidden"});assert.equal(await page.locator("#open-conversation-context").evaluate(node=>node===document.activeElement),true);
  record("mobile drawer fits viewport and Escape restores keyboard focus");
  await page.setViewportSize({width:1440,height:900});await page.locator("#open-conversation-context").click();
  // Selection is local to the panel; choose the retained turn again.
  await panel.locator(".context-turn").nth(1).locator("input[type=checkbox]").check();await panel.getByLabel("上下文包名称").fill("登录模块约束");
  await panel.getByRole("button",{name:"带入新对话",exact:true}).click();await page.locator(".tree-conversation").filter({hasText:fork.title}).waitFor();
  await page.getByText("从一个想法，开始构建。",{exact:true}).waitFor();await page.locator("#open-conversation-context").click();
  await page.getByRole("button",{name:"预览历史输入",exact:true}).click();
  assert(!(await page.getByLabel("历史输入预览").textContent()).includes("SECRET_TANGENT"));
  assert((await page.getByLabel("历史输入预览").textContent()).includes("Java 17"));
  record("new conversation carries selected context with source metadata");
  assert.equal(submissions,0);assert.deepEqual(errors,[]);record("context operations make no model calls and raise no runtime errors");
  console.log(JSON.stringify({checks:checks.length,evidence:output}));
} finally { await browser.close(); }
