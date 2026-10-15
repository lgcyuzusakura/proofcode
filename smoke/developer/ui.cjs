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
    const page=await browser.newPage({viewport:{width:1440,height:1000}});const errors=[];page.on('pageerror',error=>{errors.push(String(error));console.error('Page error:',String(error));});
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
    if(native.python){
      const rpc=(method,args)=>page.evaluate(async({method,args})=>window.go.main.App[method](...args),{method,args});
      // Construct real branch fixtures via the same native bridge, then drive
      // the React merge approval workflow. The original fixture stays isolated.
      const source='function notify(status) { return status; }\n';
      await rpc('SaveProjectFile',[native.handle,'billing.js','',source]);
      let git=await rpc('GetProjectGit',[native.handle]);git=await rpc('ProjectGitAction',[native.handle,'stage',git.revision,'',['billing.js']]);git=await rpc('ProjectGitAction',[native.handle,'commit',git.revision,'Merge base',[]]);git=await rpc('ProjectGitAction',[native.handle,'create_branch',git.revision,'colleague',[]]);
      let file=await rpc('ReadProjectFile',[native.handle,'billing.js']);await rpc('SaveProjectFile',[native.handle,file.path,file.hash,'function notify(status, page) { return status; }\n']);git=await rpc('GetProjectGit',[native.handle]);git=await rpc('ProjectGitAction',[native.handle,'stage',git.revision,'',['billing.js']]);git=await rpc('ProjectGitAction',[native.handle,'commit',git.revision,'Order pagination',[]]);git=await rpc('ProjectGitAction',[native.handle,'switch_branch',git.revision,'main',[]]);
      file=await rpc('ReadProjectFile',[native.handle,'billing.js']);await rpc('SaveProjectFile',[native.handle,file.path,file.hash,'function notify(status) { return status + 1; }\n']);git=await rpc('GetProjectGit',[native.handle]);git=await rpc('ProjectGitAction',[native.handle,'stage',git.revision,'',['billing.js']]);await rpc('ProjectGitAction',[native.handle,'commit',git.revision,'Payment change',[]]);
      await page.getByRole('button',{name:'刷新',exact:true}).click();await page.getByText('Payment change',{exact:true}).waitFor();await page.getByLabel('合并目标').fill('colleague');await page.getByRole('button',{name:'隔离预览',exact:true}).click();await page.waitForFunction(()=>!!document.querySelector('.merge-summary')||!!document.querySelector('.merge-workbench [role=alert]'));assert.equal(await page.locator('.merge-workbench [role=alert]').count(),0,await page.locator('.merge-workbench').innerText());await page.getByText('Git ort + Mergiraf',{exact:true}).waitFor();await page.getByLabel('合并结果').waitFor();assert.match(await page.getByLabel('合并结果').inputValue(),/status, page/);assert.match(await page.getByLabel('合并结果').inputValue(),/status \+ 1/);
      if(output)await page.screenshot({path:path.join(output,'structured-merge.png'),fullPage:true});
      page.once('dialog',dialog=>dialog.accept());await page.getByRole('button',{name:'批准应用合并',exact:true}).click();await page.waitForFunction(()=>!document.querySelector('.merge-summary')||!!document.querySelector('.merge-workbench [role=alert]'));assert.equal(await page.locator('.merge-workbench [role=alert]').count(),0,await page.locator('.merge-workbench').innerText());await page.getByRole('button',{name:'提交历史',exact:true}).click();await page.getByText('Merge colleague (reviewed in ProofCode)',{exact:true}).waitFor();
      await rpc('SaveProjectFile',[native.handle,'main.py','','value = 41\nresult = value + 1\nprint(result)\n']);const files=await rpc('ListProjectFiles',[native.handle]);assert(files.some(value=>value.path==='main.py'),'native file listing missed main.py');
      await nav('代码 IDE');await page.getByRole('button',{name:'刷新文件',exact:true}).click();await page.locator('.file-list button').filter({hasText:'main.py'}).click();await page.locator('.monaco-editor .view-lines').filter({hasText:'value = 41'}).waitFor();
      const lsp=page.locator('.protocol-panel').filter({has:page.getByRole('heading',{name:'语言服务 · LSP',exact:true})});await lsp.getByLabel('协议服务程序').fill(native.python);await lsp.getByLabel('协议服务参数').fill('["-m","pylsp"]');await lsp.getByRole('button',{name:'启动',exact:true}).click();await lsp.getByText('服务已连接 · 能力协商结果',{exact:true}).waitFor();
      const pythonSource='value = 41\nresult = value + 1\nprint(result)\n';
      await page.evaluate(()=>{window.protocolTrace=[];const app=window.go.main.App,original=app.ProjectProtocolRequest;app.ProjectProtocolRequest=async(...args)=>{try{const value=await original(...args);if(args[2].includes('completion')||args[2].includes('didChange'))window.protocolTrace.push({method:args[2],params:args[3],value});return value;}catch(cause){window.protocolTrace.push({method:args[2],error:String(cause)});throw cause;}};});
      const replaceEditor=async text=>{await page.locator('.monaco-editor').scrollIntoViewIfNeeded();await page.locator('.monaco-editor textarea').focus();await page.keyboard.press('Control+a');await page.keyboard.insertText(text);};
      await replaceEditor(pythonSource+'missing_symbol\n');await page.locator('.monaco-editor .squiggly-error').first().waitFor();
      await replaceEditor(pythonSource+'val');await page.getByRole('button',{name:'代码补全',exact:true}).click();try{await page.locator('.suggest-widget.visible').getByText(/^value/).first().waitFor();}catch(cause){throw Error('Completion UI: '+JSON.stringify(await page.evaluate(()=>window.protocolTrace))+' widgets='+JSON.stringify(await page.locator('.suggest-widget').allTextContents())+' '+cause);}
      await replaceEditor(pythonSource);await page.keyboard.press('Escape');
      const margin=await page.locator('.monaco-editor .glyph-margin').boundingBox();const secondLine=await page.locator('.monaco-editor .view-lines .view-line').nth(1).boundingBox();assert(margin&&secondLine);await page.mouse.click(margin.x+margin.width/2,secondLine.y+secondLine.height/2);await page.locator('.debug-breakpoint').waitFor();
      const dbg=page.locator('.protocol-panel').filter({has:page.getByRole('heading',{name:'断点调试 · DAP',exact:true})});await dbg.getByLabel('协议服务程序').fill(native.python);await dbg.getByLabel('协议服务参数').fill('["-m","debugpy.adapter"]');await dbg.getByLabel('调试启动配置').fill(JSON.stringify({type:'python',request:'launch',name:'UI debug',program:'main.py',python:native.python,console:'internalConsole',stopOnEntry:false}));await dbg.getByRole('button',{name:'启动',exact:true}).click();try{await dbg.getByRole('button',{name:'启动调试程序',exact:true}).click();}catch(cause){throw Error('Debug initialize UI: '+await dbg.innerText()+' '+cause);}await dbg.getByText(/已暂停/).waitFor();await dbg.locator('.debug-inspection').getByRole('button').filter({hasText:/· 2$/}).first().click();await dbg.locator('.debug-inspection').getByRole('button',{name:'Locals',exact:true}).click();await dbg.locator('pre').filter({hasText:'value: 41'}).waitFor();await dbg.getByRole('button',{name:'步过',exact:true}).click();await dbg.locator('.debug-inspection').getByRole('button').filter({hasText:/· 3$/}).first().click();
      await dbg.getByLabel('调试表达式').fill('40 + 2');await dbg.getByRole('button',{name:'求值',exact:true}).click();await dbg.getByText('调试输出与事件',{exact:true}).click();await dbg.locator('pre').filter({hasText:'"result":"42"'}).waitFor();
      if(output)await page.screenshot({path:path.join(output,'lsp-debug.png'),fullPage:true});
      await dbg.getByRole('button',{name:'继续',exact:true}).click();await dbg.getByRole('button',{name:'停止',exact:true}).click();await lsp.getByRole('button',{name:'停止',exact:true}).click();
      await nav('Git');await page.locator('.git-file').filter({hasText:'main.py'}).getByRole('button',{name:'暂存',exact:true}).click();await page.getByLabel('提交说明',{exact:true}).fill('Add debugger fixture');await page.getByRole('button',{name:'提交暂存修改'}).click();
    }
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
    assert.deepEqual(errors,[]);console.log('PASS: real native editor/save/draft, Git commit, structured merge approval, LSP diagnostics/completion, gutter breakpoint, DAP step/stack/scopes/evaluate, command, browser, agent patch application, exact review routing, mobile, zero page errors');
  } finally {await browser.close();}
})().catch(error=>{console.error(error);process.exit(1);});
