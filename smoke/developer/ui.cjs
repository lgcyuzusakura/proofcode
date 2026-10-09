const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require(process.env.PROOFCODE_PLAYWRIGHT_MODULE || 'playwright');
const native = JSON.parse(process.env.PROOFCODE_UI_NATIVE);
const apiBase = process.env.PROOFCODE_UI_CONTROL_URL || 'http://127.0.0.1:18097';
const web = process.env.PROOFCODE_UI_URL || 'http://127.0.0.1:15173';
const token = process.env.PROOFCODE_UI_TOKEN || 'experiment-smoke-user';
const output = process.env.PROOFCODE_UI_OUTPUT;
async function api(route, body) {
  const response = await fetch(apiBase + route, {method:body===undefined?'GET':'POST',headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)});
  assert(response.ok, `${route}: ${response.status} ${await response.clone().text()}`);
  return response.status===204 ? undefined : response.json();
}
async function waitTask(scope, prompt) {
  const deadline=Date.now()+60000;
  while(Date.now()<deadline){const tasks=await api(`/api/tasks?projectId=${scope.projectId}&conversationId=${scope.conversationId}`);const task=tasks.find(value=>value.prompt===prompt);if(task&&['SUCCEEDED','FAILED','CANCELLED'].includes(task.status)){assert.equal(task.status,'SUCCEEDED',task.error);return task;}await new Promise(resolve=>setTimeout(resolve,300));}
  throw Error('fixture task timed out');
}
(async()=>{
  const project=await api('/api/projects',{name:'Native UI fixture '+Date.now(),sourceKind:'LOCAL_FOLDER',localHandle:native.handle,bootstrapId:crypto.randomUUID(),defaultBranch:'main'});
  const scope=await api(`/api/projects/${project.id}/default-scope`,{});
  const browser=await chromium.launch({channel:'msedge',headless:true});
  try {
    const page=await browser.newPage({viewport:{width:1440,height:1000}});const errors=[];page.on('pageerror',error=>errors.push(String(error)));
    await page.addInitScript(({native,scope,token})=>{
      localStorage.setItem('proofcode.token',token);localStorage.setItem('proofcode.model','fixture-only');localStorage.setItem('proofcode.selected-scope',JSON.stringify(scope));
      const app={};for(const name of native.names)app[name]=async(...args)=>{const response=await fetch(native.bridge+'/rpc',{method:'POST',headers:{Authorization:'Bearer '+native.bridgeToken,'Content-Type':'application/json'},body:JSON.stringify({method:name,args})});const result=await response.json();if(result.error)throw Error(result.error);return result.value;};window.go={main:{App:app}};
    },{native,scope,token});
    await page.goto(web);await page.locator('.tree-conversation.selected').waitFor();
    const nav=async(name)=>page.locator('.nav-rail').getByRole('button',{name,exact:true}).click();
    assert.equal(await page.locator('.agent-context,.conversation-run-detail,.agent-task-picker').count(),0);
    await nav('代码 IDE');await page.locator('.file-list button').filter({hasText:'hello.ts'}).click();await page.locator('.monaco-editor textarea').waitFor();
    const editor=page.locator('.monaco-editor textarea');await editor.focus();await page.keyboard.press('Control+a');await page.keyboard.insertText('export const proof = "草稿恢复";\n');
    await page.getByText('hello.ts •',{exact:true}).waitFor();
    await nav('Git');await page.getByRole('heading',{name:/变更/}).waitFor();await nav('代码 IDE');
    await page.getByText(/已恢复此工程未保存的编辑草稿/).waitFor();await page.locator('.monaco-editor .view-lines').filter({hasText:'草稿恢复'}).waitFor();
    await page.getByRole('button',{name:'保存 · Ctrl+S'}).click();await page.getByText('hello.ts',{exact:true}).first().waitFor();
    await nav('Git');await page.locator('.git-file').filter({hasText:'hello.ts'}).getByRole('button',{name:'暂存',exact:true}).click();await page.getByLabel('提交说明',{exact:true}).fill('Verify real UI editor save');await page.getByRole('button',{name:'提交暂存修改'}).click();
    await page.getByRole('button',{name:'提交历史',exact:true}).click();await page.getByText('Verify real UI editor save',{exact:true}).waitFor();
    await nav('代码 IDE');await page.getByLabel('程序',{exact:true}).fill('node');
    // JSON.stringify keeps the command payload independent from shell quoting.
    await page.getByLabel('程序参数',{exact:true}).fill(JSON.stringify(['-e','console.log("ui-command-ok")']));await page.getByRole('button',{name:'运行',exact:true}).click();await page.locator('.command-console pre').filter({hasText:'ui-command-ok'}).waitFor();
    await nav('浏览器');await page.getByLabel('浏览器地址').fill(native.bridge+'/preview');await page.getByRole('button',{name:'打开',exact:true}).click();await page.locator('.browser-viewport img').waitFor();await page.getByText('ProofCode UI preview',{exact:false}).first().waitFor();
    if(output){fs.mkdirSync(output,{recursive:true});await page.screenshot({path:path.join(output,'developer-browser.png'),fullPage:true});}
    await nav('编码与聊天');await page.getByRole('button',{name:'代码任务',exact:true}).click();await page.locator('#task-composer').fill('PROOFCODE_SOURCE_SMOKE CREATE');await page.getByRole('button',{name:'运行任务',exact:true}).click();
    const first=await waitTask(scope,'PROOFCODE_SOURCE_SMOKE CREATE');await page.locator('.message-row.assistant').filter({hasText:'Created and tested'}).getByRole('button',{name:'查看修改',exact:true}).click();assert.equal(await page.getByLabel('审查回合').inputValue(),first.id);
    await page.locator('.review-patch').filter({hasText:'def add'}).waitFor();assert(!(await page.locator('.review-patch').innerText()).includes('__pycache__'));await page.getByRole('button',{name:'批准应用到工程',exact:true}).click();await page.getByRole('button',{name:'已应用到工程',exact:true}).waitFor();
    if(output)await page.screenshot({path:path.join(output,'developer-review.png'),fullPage:true});
    await nav('编码与聊天');await page.locator('#task-composer').fill('PROOFCODE_SOURCE_SMOKE READ');await page.getByRole('button',{name:'运行任务',exact:true}).click();const second=await waitTask(scope,'PROOFCODE_SOURCE_SMOKE READ');
    await page.locator('.message-row.assistant').filter({hasText:'Verified the unchanged'}).getByRole('button',{name:'查看修改',exact:true}).click();assert.equal(await page.getByLabel('审查回合').inputValue(),second.id);assert.equal(await page.locator('.review-patch .diff-added').count(),0);
    await nav('编码与聊天');await page.locator('.message-row.assistant').filter({hasText:'Created and tested'}).getByRole('button',{name:'查看修改',exact:true}).click();assert.equal(await page.getByLabel('审查回合').inputValue(),first.id);await page.locator('.review-patch').filter({hasText:'def add'}).waitFor();
    await nav('编码与聊天');if(output)await page.screenshot({path:path.join(output,'developer-chat.png'),fullPage:true});
    await page.setViewportSize({width:600,height:900});await page.locator('.rail-toggle').click();await page.locator('.nav-rail').getByRole('button',{name:'代码审查',exact:true}).click();await page.getByRole('heading',{name:'代码审查',exact:true}).waitFor();
    if(output)await page.screenshot({path:path.join(output,'developer-review-mobile.png'),fullPage:true});
    assert.deepEqual(errors,[]);console.log('PASS: real native editor/save/draft, Git commit, command, browser, agent patch application, exact review routing, mobile, zero page errors');
  } finally {await browser.close();}
})().catch(error=>{console.error(error);process.exit(1);});
