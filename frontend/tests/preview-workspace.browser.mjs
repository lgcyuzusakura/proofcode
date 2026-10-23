import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import JSZip from 'jszip';
const { chromium } = createRequire(import.meta.url)(process.env.PROOFCODE_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ channel: 'msedge', headless: true });
const context = await browser.newContext({ viewport: { width: 1600, height: 1000 } });
const page = await context.newPage(), errors = [], calls = [], outbound = [], results = [];
page.on('pageerror', error => errors.push(error.message));
page.on('request', request => { if (!/^(http:\/\/127\.0\.0\.1:|blob:|data:)/.test(request.url())) outbound.push(request.url()); });
const project = { id:'preview-project', name:'ProofCode', defaultBranch:'main', sourceKind:'LOCAL_FOLDER', localHandle:'fixture-handle' };
const workspace = {id:'preview-workspace',projectId:project.id,name:'开发',defaultWorkspace:true};
const conversation = {id:'preview-chat',projectId:project.id,workspaceId:workspace.id,title:'并排预览文档'};
const scope = {projectId:project.id,workspaceId:workspace.id,conversationId:conversation.id};
const messages = Array.from({length:24},(_,i)=>({id:`message-${i}`,sequence:i+1,role:i%2?'assistant':'user',content:i%2?'已完成模块分析。\n\n'+('这里是保留的项目说明。'.repeat(12)):`请检查模块 ${i+1}`,status:'SUCCEEDED',createdAt:'2026-10-22T08:00:00Z'}));
await context.addInitScript(({scope})=>{
 if(window.top!==window)return;
 localStorage.setItem('proofcode.session','fixture');localStorage.setItem('proofcode.selected-scope',JSON.stringify(scope));
 window.previewBrowserCalls=[];
 const image='data:image/svg+xml,'+encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="1280" height="800"><rect width="1280" height="800" fill="#f7f8fb"/><rect x="80" y="80" width="1120" height="640" rx="16" fill="white"/><text x="140" y="180" font-size="38" fill="#26314c">ProofCode preview</text><text x="140" y="240" font-size="22" fill="#68758e">Project browser alongside your conversation</text></svg>');
 let state={url:'http://127.0.0.1:3000',title:'本地开发预览',image,text:'Local preview',console:[],width:1280,height:800,owner:'human'};
 window.go={main:{App:{ListLocalProjects:async()=>[],ListProjectFiles:async()=>[{path:'README.md',size:80,hash:'fixture'}],ReadProjectFile:async()=>({path:'README.md',hash:'fixture',content:'# 项目文件预览\n\n这是本机工程的 Markdown。'}),GetProjectCommand:async()=>null,BindProjectBrowser:async()=>{},ProjectBrowser:async(handle,action,value,x,y)=>{window.previewBrowserCalls.push({handle,action,value,x,y});if(action==='open')state={...state,url:value};return state;}}}};
},{scope});
await page.route('**/api/**',async route=>{
 const request=route.request(),path=new URL(request.url()).pathname;calls.push({path,method:request.method(),body:request.postData()});let body=[];
 if(path==='/api/auth/me')body={id:'fixture',username:'fixture'};
 else if(path==='/api/projects')body=[project];
 else if(path.endsWith('/default-scope'))body=scope;
 else if(path.endsWith('/workspaces'))body=[workspace];
 else if(path.endsWith('/conversations'))body=[conversation];
 else if(path.endsWith('/messages'))body=messages;
 else if(path.endsWith('/context'))body={version:0,view:{mode:'AUTO',items:[],packIds:[]},messages:[],packs:[],excludedTurns:0,estimatedTokens:0};
 else if(path.includes('/selection/'))body={profileId:null};
 await route.fulfill({json:body});
});
const output=resolve('.proofcode/preview-workspace-qa');await mkdir(output,{recursive:true});
const record=message=>{results.push(message);console.log('PASS '+message);};
const pane=page.locator('aside.preview-pane'),picker=page.getByLabel('选择预览文档');
async function pdfReady(){await page.waitForFunction(()=>{const node=document.querySelector('.preview-document-content:not([hidden]) .preview-pdf-viewport');return node&&node.clientWidth===Number(node.dataset.renderedWidth)&&node.dataset.renderKey===node.dataset.renderedKey&&!node.querySelector('.preview-pdf-loading');});}
async function upload(name,buffer,mimeType='application/octet-stream'){await picker.setInputFiles({name,mimeType,buffer:Buffer.from(buffer)});}
function makePDF(){
 const objects=['<< /Type /Catalog /Pages 2 0 R >>','<< /Type /Pages /Kids [3 0 R 5 0 R] /Count 2 >>','<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 7 0 R >> >> /Contents 4 0 R >>','','<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 7 0 R >> >> /Contents 6 0 R >>','','<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>'];
 for(const [index,title]of [[3,'ProofCode Preview / Page one'],[5,'Page two / Local document']]){const stream=`BT /F1 22 Tf 50 770 Td (${title}) Tj /F1 12 Tf 0 -45 Td (Conversation stays visible while reading this document.) Tj ET`;objects[index]=`<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`;}
 let value='%PDF-1.4\n',offsets=[0];objects.forEach((object,i)=>{offsets.push(Buffer.byteLength(value));value+=`${i+1} 0 obj\n${object}\nendobj\n`;});const offset=Buffer.byteLength(value);value+=`xref\n0 ${objects.length+1}\n0000000000 65535 f \n`+offsets.slice(1).map(n=>String(n).padStart(10,'0')+' 00000 n \n').join('')+`trailer\n<< /Size ${objects.length+1} /Root 1 0 R >>\nstartxref\n${offset}\n%%EOF`;return Buffer.from(value);
}
try{
 await page.goto(process.env.PROOFCODE_UI_URL||'http://127.0.0.1:5177');await page.locator('.message-row').first().waitFor();
 const composer=page.getByLabel('消息内容');await composer.fill('PREVIEW_DRAFT_KEEP');
 await page.locator('.conversation-stream').evaluate(node=>node.scrollTop=300);
 await page.getByRole('button',{name:'文档与浏览器预览',exact:true}).click();await pane.waitFor();
 await upload('architecture.md','# 项目架构\n\n在右侧阅读，同时保留左侧对话。\n\n## 工作流\n\n| 阶段 | 状态 |\n| --- | --- |\n| 读取项目 | 完成 |\n| 审查修改 | 待处理 |\n\n```ts\nconst preview = true;\n```\n\n[本地页面](http://127.0.0.1:3000/docs)\n\n![tracking](https://never-fetch.example.test/pixel.png)\n<script>window.documentInjected=true</script>\n\n'+('补充项目说明\n\n'.repeat(100)),'text/markdown');
 await pane.getByRole('heading',{name:'项目架构',exact:true}).waitFor();
 assert.equal(await composer.inputValue(),'PREVIEW_DRAFT_KEEP');assert(Math.abs(await page.locator('.conversation-stream').evaluate(node=>node.scrollTop)-300)<100);
 assert(await pane.locator('table').isVisible());assert.equal(await page.evaluate(()=>window.documentInjected),undefined);
 const chatBounds=await composer.boundingBox(),paneBounds=await pane.boundingBox();assert(chatBounds.x+chatBounds.width<=paneBounds.x);
 const chatTop=await page.locator('.conversation-stream').evaluate(node=>node.scrollTop);await pane.locator('.preview-markdown').evaluate(node=>node.scrollTop=400);assert.equal(await page.locator('.conversation-stream').evaluate(node=>node.scrollTop),chatTop);
 record('Markdown renders safely beside chat; draft and independent scroll positions survive opening');
 const divider=page.getByRole('separator',{name:'调整对话与预览宽度'});await divider.focus();await page.keyboard.press('ArrowLeft');assert.equal(await divider.getAttribute('aria-valuenow'),'52');
 const before=await pane.boundingBox();const handle=await divider.boundingBox();await page.mouse.move(handle.x+3,handle.y+30);await page.mouse.down();await page.mouse.move(handle.x-45,handle.y+30);await page.mouse.up();assert((await pane.boundingBox()).width>before.width);record('divider resizes by keyboard and pointer');
 await upload('preview.pdf',makePDF(),'application/pdf');await pane.locator('.textLayer').getByText('ProofCode Preview / Page one',{exact:true}).waitFor();await pdfReady();
 assert(await pane.locator('canvas').evaluate(canvas=>canvas.width>100&&canvas.getContext('2d').getImageData(0,0,canvas.width,canvas.height).data.some((value,index)=>index%4!==3&&value<180)));
 await pane.getByTitle('下一页',{exact:true}).click();await pane.locator('.textLayer').getByText('Page two / Local document',{exact:true}).waitFor();
 await pane.getByTitle('放大文档',{exact:true}).click();await pdfReady();
 const pdfViewport=pane.getByLabel('PDF 阅读区域',{exact:true});
 await pdfViewport.evaluate(node=>node.scrollTop=240);await page.waitForTimeout(80);
 const readingOffset=await pdfViewport.evaluate(node=>node.scrollTop/node.querySelector('canvas').clientHeight);
 await pane.getByTitle('放大文档',{exact:true}).click();await pdfReady();
 assert(Math.abs(await pdfViewport.evaluate(node=>node.scrollTop/node.querySelector('canvas').clientHeight)-readingOffset)<.02);
 await pane.getByRole('tab',{name:'architecture.md',exact:true}).click();await pane.getByRole('tab',{name:'preview.pdf',exact:true}).click();assert.equal(await pane.getByLabel('PDF 页码').inputValue(),'2');
 await pane.getByTitle('全屏预览',{exact:true}).click();assert.equal((await pane.boundingBox()).width,1600);await pdfReady();
 assert(Math.abs(await pdfViewport.evaluate(node=>node.scrollTop/node.querySelector('canvas').clientHeight)-readingOffset)<.02);
 await page.keyboard.press('Escape');await page.waitForFunction(()=>!document.querySelector('.preview-expanded'));assert.equal(await composer.inputValue(),'PREVIEW_DRAFT_KEEP');
 await pdfReady();
 assert(Math.abs(await pdfViewport.evaluate(node=>node.scrollTop/node.querySelector('canvas').clientHeight)-readingOffset)<.02);
 await divider.focus();await page.keyboard.press('ArrowLeft');await pdfReady();
 assert(Math.abs(await pdfViewport.evaluate(node=>node.scrollTop/node.querySelector('canvas').clientHeight)-readingOffset)<.02);
 await pdfViewport.evaluate(node=>{node.scrollTop=0;node.scrollLeft=0;});await page.waitForTimeout(120);
 await pdfReady();
 assert(await pane.locator('canvas').evaluate(canvas=>canvas.getContext('2d').getImageData(0,0,canvas.width,canvas.height).data.some((value,index)=>index%4!==3&&value<180)));
 await page.screenshot({path:resolve(output,'pdf-light.png')});record('real local PDF canvas and selectable text, paging, zoom, tab state and fullscreen/Escape pass');
 const zip=new JSZip();zip.file('[Content_Types].xml','<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>');zip.file('_rels/.rels','<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>');zip.file('word/document.xml','<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:rPr><w:b/><w:sz w:val="36"/></w:rPr><w:t>ProofCode Word Preview</w:t></w:r></w:p><w:p><w:r><w:t>本地 Word 文档，与聊天同时查看。</w:t></w:r></w:p><w:p><w:r><w:br w:type="page"/><w:t>Second page</w:t></w:r></w:p><w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr></w:body></w:document>');
 await upload('preview.docx',await zip.generateAsync({type:'nodebuffer'}));const word=pane.frameLocator('iframe[title="Word 文档"]');await word.getByText('ProofCode Word Preview',{exact:true}).waitFor();assert.equal(await pane.locator('iframe').getAttribute('sandbox'),'');
 await pane.getByTitle('全屏预览',{exact:true}).click();await word.getByText('本地 Word 文档，与聊天同时查看。',{exact:true}).waitFor();await pane.getByTitle('退出全屏',{exact:true}).click();record('Word pages render inside local opaque sandbox in split and fullscreen modes');
 await page.waitForTimeout(200);await page.screenshot({path:resolve(output,'word-light.png')});
 await page.getByRole('button',{name:'切换深色外观',exact:true}).click();
 assert(await pane.locator('.preview-document-tab.is-active').evaluate(node=>{
  const reference=document.createElement('span');reference.style.backgroundColor='var(--soft)';document.body.append(reference);
  const matches=getComputedStyle(node).backgroundColor===getComputedStyle(reference).backgroundColor;reference.remove();return matches;
 }));
 await page.screenshot({path:resolve(output,'word-dark.png')});
 await pane.getByRole('tab',{name:'architecture.md',exact:true}).click();await pane.getByRole('link',{name:'本地页面',exact:true}).click();await page.waitForFunction(()=>window.previewBrowserCalls.some(item=>item.action==='open'&&item.value==='http://127.0.0.1:3000/docs'));
 await pane.getByRole('img',{name:'本地开发预览',exact:true}).click({position:{x:80,y:45}});assert(await page.evaluate(()=>window.previewBrowserCalls.some(item=>item.action==='click')));
 await pane.getByTitle('全屏预览',{exact:true}).click();await page.keyboard.press('Escape');await page.screenshot({path:resolve(output,'browser-dark.png')});record('Markdown links open existing native browser; browser controls and fullscreen preserve chat');
 await pane.getByTitle('收起预览',{exact:true}).click();await page.getByRole('button',{name:'文档与浏览器预览',exact:true}).click();await pane.getByRole('tab',{name:'preview.pdf',exact:true}).click();assert.equal(await pane.getByLabel('PDF 页码').inputValue(),'2');assert.equal(await composer.inputValue(),'PREVIEW_DRAFT_KEEP');record('collapsing and reopening keeps tabs, PDF page and composer draft');
 await upload('old.doc','fixture');await pane.getByRole('alert').filter({hasText:'旧版 .doc'}).waitFor();await pane.getByTitle('关闭文档提示').click();
 await upload('broken.pdf','not a PDF','application/pdf');await pane.getByRole('alert').filter({hasText:'PDF 无法读取'}).waitFor();await pane.getByTitle('关闭 broken.pdf',{exact:true}).click();
 await upload('broken.docx','not a ZIP');await pane.getByRole('alert').filter({hasText:'Word 文件损坏'}).waitFor();await pane.getByTitle('关闭 broken.docx',{exact:true}).click();record('legacy and damaged files show recoverable errors without losing open documents');
 for(const width of [760,390]){await page.setViewportSize({width,height:900});await page.waitForTimeout(300);const c=await composer.boundingBox(),p=await pane.boundingBox();assert(c.height>20&&p.height>100&&c.y+c.height<=p.y);assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await page.screenshot({path:resolve(output,`responsive-${width}.png`)});await pane.getByTitle('全屏预览',{exact:true}).click();assert.equal((await pane.boundingBox()).width,width);await pane.getByTitle('退出全屏',{exact:true}).click();}
 record('760px and 390px retain chat plus preview, no horizontal overflow, fullscreen available');
 await page.setViewportSize({width:1600,height:1000});
 await page.getByRole('button',{name:'文件',exact:true}).click();await page.getByRole('button',{name:'README.md',exact:true}).click();
 await page.locator('.editor-toolbar').getByRole('button',{name:'预览',exact:true}).click();
 await pane.getByRole('heading',{name:'项目文件预览',exact:true}).waitFor();assert.equal(await composer.inputValue(),'PREVIEW_DRAFT_KEEP');
 const fileTab=pane.getByRole('tab',{name:'README.md',exact:true});await fileTab.focus();await page.keyboard.press('Home');await pane.getByRole('tab',{name:'浏览器',exact:true,selected:true}).waitFor();
 record('project Markdown opens from editor into chat preview; keyboard tabs and existing draft survive navigation');
 await pane.getByTitle('关闭浏览器标签',{exact:true}).click();assert.equal(await pane.getByRole('tab',{name:'浏览器',exact:true}).count(),0);assert.equal(await composer.inputValue(),'PREVIEW_DRAFT_KEEP');
 const transfer=await page.evaluateHandle(()=>{const data=new DataTransfer();data.items.add(new File(['# 拖入的文档\n\n本机阅读，不进入聊天上下文。'],'dropped.md',{type:'text/markdown'}));return data;});
 await pane.dispatchEvent('dragenter',{dataTransfer:transfer});await pane.getByRole('status').filter({hasText:'松开文件'}).waitFor();
 await pane.dispatchEvent('drop',{dataTransfer:transfer});await transfer.dispose();await pane.getByRole('heading',{name:'拖入的文档',exact:true}).waitFor();
 assert.equal(await pane.getByRole('status').filter({hasText:'松开文件'}).count(),0);assert.equal(await composer.inputValue(),'PREVIEW_DRAFT_KEEP');
 record('local file drop opens a document without uploading; browser tab closes independently; PDF reading offset survives zoom and fullscreen');
 assert.deepEqual(outbound,[]);assert.deepEqual(calls.filter(call=>call.method!=='GET'&&!call.path.endsWith('/default-scope')),[]);assert.deepEqual(errors,[]);record('no document upload, model call, external resource request or runtime error');
 await writeFile(resolve(output,'verification.json'),JSON.stringify({checks:results,errors,outbound},null,2));
}finally{await browser.close();}
