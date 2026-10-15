const {chromium} = require(process.env.PROOFCODE_PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const config = JSON.parse(process.env.PROOFCODE_BROWSER_UI_CONFIG);
const output = process.env.PROOFCODE_UI_OUTPUT;
const scope = {projectId:config.projectId,workspaceId:'22222222-2222-4222-8222-222222222222',conversationId:'33333333-3333-4333-8333-333333333333'};
const project = {id:config.projectId,name:'ProofCode 开发工程',sourceKind:'LOCAL_FOLDER',localHandle:config.handle,defaultBranch:'main'};
const task = {id:'visual-task',...scope,prompt:'检查开发页面',model:'configured-model',status:'SUCCEEDED',createdAt:new Date().toISOString()};
const events = [{sequence:1,type:'task.started',timestamp:new Date().toISOString(),payload:{tools:[{name:'browser',description:'AI 和人工共用的项目浏览器'}]}},{sequence:2,type:'usage.updated',timestamp:new Date().toISOString(),payload:{requestInputTokens:4842,requestOutputTokens:42,requestCachedInputTokens:3870,requestCacheReported:true,inputTokens:9321,outputTokens:118,totalTokens:9439,contextCeiling:128000}},{sequence:3,type:'agent.delegated',timestamp:new Date().toISOString(),payload:{agentId:'agent-visual',kind:'completed',payload:{name:'页面调查',status:'completed',result:'按钮点击后页面显示刷新结果。',usage:{inputTokens:1600,outputTokens:220}}}}];
(async()=>{
 const browser=await chromium.launch({channel:'msedge',headless:true});
 try{
  const page=await browser.newPage({viewport:{width:1440,height:1050}});const errors=[];page.on('pageerror',e=>errors.push(String(e)));
  await page.addInitScript(({config,scope})=>{
   localStorage.setItem('proofcode.selected-scope',JSON.stringify(scope));
   window.go={main:{App:{}}};
   for(const name of ['ProjectBrowser','BindProjectBrowser'])window.go.main.App[name]=async(...args)=>{const response=await fetch(config.bridge+'/rpc',{method:'POST',headers:{'Authorization':'Bearer '+config.token,'Content-Type':'application/json'},body:JSON.stringify({method:name,args})});const body=await response.json();if(body.error)throw Error(body.error);return body.value};
  },{config,scope});
  await page.route('**/api/**',async route=>{
   const p=new URL(route.request().url()).pathname;let body=[];
   if(p==='/api/projects')body=[project];
   else if(p.endsWith('/workspaces'))body=[{id:scope.workspaceId,projectId:project.id,name:'主工作区',defaultWorkspace:true}];
   else if(p.endsWith('/conversations'))body=[{id:scope.conversationId,projectId:project.id,workspaceId:scope.workspaceId,title:'网页验证'}];
   else if(p==='/api/tasks')body=[task];else if(p.endsWith('/events'))body=events;
   await route.fulfill({contentType:'application/json',body:JSON.stringify(body)});
  });
  await page.goto(process.env.PROOFCODE_UI_URL);
  await page.locator('.nav-item').filter({hasText:'浏览器'}).click();
  await page.locator('.browser-viewport img').waitFor();await page.getByText('ProofCode web preview',{exact:true}).waitFor();
  await page.getByRole('tab',{name:/页面文本/}).click();await page.locator('.browser-page-text').filter({hasText:'验证通过：订单列表已刷新'}).waitFor();
  await page.getByRole('button',{name:'接管',exact:true}).click();await page.getByText('人工控制',{exact:true}).waitFor();
  await page.getByRole('button',{name:'交还 AI',exact:true}).click();await page.getByText('AI 可操作',{exact:true}).waitFor();
  await page.getByRole('tab',{name:/页面控件/}).click();await page.locator('.browser-elements').getByText('刷新订单',{exact:true}).waitFor();
  await page.locator('.shared-browser-page').evaluate(e=>e.scrollIntoView({block:'start'}));
  if(output){fs.mkdirSync(output,{recursive:true});await page.screenshot({path:path.join(output,'shared-browser-desktop.png'),fullPage:true});}
  await page.setViewportSize({width:390,height:844});await page.waitForTimeout(300);await page.getByTitle('收起导航').click();
  await page.waitForFunction(()=>document.querySelector('.nav-rail').getBoundingClientRect().width<=60);
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,'mobile overflow');
  if(output)await page.screenshot({path:path.join(output,'shared-browser-mobile.png'),fullPage:true});
  await page.setViewportSize({width:1440,height:1050});await page.getByTitle('展开导航').click();await page.locator('.nav-item').filter({hasText:'执行链'}).click();await page.getByRole('heading',{name:'上下文与缓存'}).waitFor();
  if(output)await page.screenshot({path:path.join(output,'context-and-team.png'),fullPage:true});
  assert.deepEqual(errors,[]);console.log(JSON.stringify({sharedBrowser:true,actualChromium:true,aiClickVisible:true,handover:true,mobileOverflow:false,uiFixtureTelemetry:true,pageErrors:errors}));
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
