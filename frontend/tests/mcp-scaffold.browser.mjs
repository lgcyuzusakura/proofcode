import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
const { chromium } = createRequire(import.meta.url)(process.env.PROOFCODE_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ channel: 'msedge', headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await context.newPage(), errors = [], writes = [], results = [];
page.on('pageerror', error => errors.push(error.message));
const project = { id: 'scaffold-project', name: 'ProofCode', sourceKind: 'SCRATCH' };
const workspace = { id: 'workspace', projectId: project.id, name: '开发', defaultWorkspace: true };
const conversation = { id: 'chat', projectId: project.id, workspaceId: workspace.id, title: '工具服务' };
const scope = { projectId: project.id, workspaceId: workspace.id, conversationId: conversation.id };
await context.addInitScript(({ scope }) => {
 localStorage.setItem('proofcode.session', 'fixture'); localStorage.setItem('proofcode.selected-scope', JSON.stringify(scope));
 window.scaffoldCalls = []; window.scaffoldMode = 'cancel'; window.templateMode = 'ready';
 window.go = { main: { App: {
  GetMcpServiceTemplate: async () => { if (window.templateMode === 'error') throw Error('PRIVATE_NATIVE_ERROR'); return { available: window.templateMode !== 'missing', files: 14, release: '0.1.0' }; },
  CreateMcpService: async (name, transport, port, docker) => {
   window.scaffoldCalls.push({ name, transport, port, docker });
   if (window.scaffoldMode === 'pending') await new Promise(resolve => { window.releaseScaffold = resolve; });
   if (window.scaffoldMode === 'error') throw Error('PRIVATE_NATIVE_ERROR');
   if (window.scaffoldMode === 'cancel') return { created: false };
   return { created: true, name, transport, docker, directory: 'D:\\tools\\ProofCode-mcp-' + name, bearer: transport === 'http' ? 'FIXTURE_PRIVATE_SCAFFOLD_TOKEN' : '', url: transport === 'http' ? `http://${docker ? 'host.docker.internal' : '127.0.0.1'}:${port}/mcp` : '', commands: 'npm ci\nnpm run check\nnpm start' };
  },
 } } };
}, { scope });
await page.route('**/api/**', async route => {
 const request = route.request(), path = new URL(request.url()).pathname; let body = [];
 if (request.method() !== 'GET' && !path.endsWith('/default-scope')) writes.push({ path, body: request.postDataJSON() });
 if (path === '/api/auth/me') body = { id: 'fixture', username: 'fixture' };
 else if (path === '/api/projects') body = [project];
 else if (path.endsWith('/workspaces')) body = [workspace];
 else if (path.endsWith('/conversations')) body = [conversation];
 else if (path.endsWith('/default-scope')) body = scope;
 else if (path.includes('/selection/')) body = { profileId: null };
 else if (path.endsWith('/context')) body = { version: 0, view: { mode: 'AUTO', items: [], packIds: [] }, messages: [], packs: [] };
 else if (path.endsWith('/mcp-servers') && request.method() === 'POST') { const input = request.postDataJSON(); body = { id: 'new-service', name: input.name, url: input.url, enabled: true, version: 0, hasBearer: true }; }
 await route.fulfill({ json: body });
});
const record = message => { results.push(message); console.log('PASS ' + message); };
const output = resolve('.proofcode/mcp-scaffold-qa'); await mkdir(output, { recursive: true });
const dialog = page.getByRole('dialog', { name: '智能体、偏好与项目记忆' });
const wizard = page.getByRole('region', { name: '桌面创建工具服务' });
const create = () => wizard.getByRole('button', { name: '选择位置并创建工具服务', exact: true });
const fill = () => wizard.getByRole('button', { name: '填入新服务表单', exact: true });
const open = async () => {
 await page.getByRole('button', { name: '智能体与记忆', exact: true }).click();
 await dialog.getByRole('button', { name: 'MCP 服务', exact: true }).click();
 await dialog.locator('.mcp-service-guide > summary').click();
 try { await create().waitFor({ timeout: 5000 }); } catch (error) { console.log(await dialog.innerText()); throw error; }
};
try {
 await page.goto(process.env.PROOFCODE_UI_URL || 'http://127.0.0.1:5177'); await open();
 assert.deepEqual(writes, []); assert.equal(await page.evaluate(() => window.scaffoldCalls.length), 0);
 await dialog.getByLabel('服务别名', { exact: true }).fill('existing_draft');
 await dialog.getByLabel('服务地址', { exact: true }).fill('https://existing.example.test/mcp');
 await wizard.getByLabel('新工具服务别名').fill('../escape'); assert(await create().isDisabled());
 await wizard.getByLabel('新工具服务别名').fill('my_tools');
 await wizard.getByLabel('新工具监听端口').fill('80'); assert(await create().isDisabled());
 await wizard.getByLabel('新工具监听端口').fill('3190'); await create().click();
 await wizard.getByText('未创建工具服务，原配置保持不变。').waitFor();
 assert.equal(await dialog.getByLabel('服务别名', { exact: true }).inputValue(), 'existing_draft'); record('passive metadata, validation and cancelled picker never mutate existing service draft');
 await page.evaluate(() => window.scaffoldMode = 'pending'); await create().dblclick();
 await page.waitForFunction(() => typeof window.releaseScaffold === 'function');
 assert(await create().isDisabled()); assert(await dialog.getByRole('button', { name: '智能体', exact: true }).isDisabled());
 assert.equal(await page.evaluate(() => window.scaffoldCalls.length), 2);
 await page.evaluate(() => { window.scaffoldMode = 'success'; window.releaseScaffold(); });
 await fill().waitFor();
 assert.equal(await dialog.getByLabel('服务别名', { exact: true }).inputValue(), 'existing_draft'); assert.deepEqual(writes, []);
 assert(!(await dialog.innerText()).includes('FIXTURE_PRIVATE_SCAFFOLD_TOKEN'));
 await fill().click(); assert.equal(await dialog.getByLabel('服务别名', { exact: true }).inputValue(), 'my_tools');
 const password = dialog.getByLabel('访问令牌（可选）', { exact: true });
 assert.equal(await password.getAttribute('type'), 'password'); assert.equal(await password.inputValue(), 'FIXTURE_PRIVATE_SCAFFOLD_TOKEN');
 assert(!(await page.evaluate(() => JSON.stringify(localStorage) + JSON.stringify(sessionStorage))).includes('FIXTURE_PRIVATE_SCAFFOLD_TOKEN')); assert.deepEqual(writes, []);
 await dialog.getByRole('button', { name: '保存服务', exact: true }).click();
 await page.waitForFunction(() => document.querySelector('.mcp-servers-body aside strong')?.textContent === 'my_tools');
 assert.equal(writes.length, 1); assert.equal(writes[0].body.bearer, 'FIXTURE_PRIVATE_SCAFFOLD_TOKEN'); record('single-flight creation preserves draft until explicit fill; token remains password-only and saving is separate');
 await wizard.getByLabel('供 Docker 中的后端访问').check(); await create().click(); await fill().waitFor();
 assert.match(await wizard.innerText(), /host.docker.internal:3190/);
 await wizard.getByLabel('新工具传输方式').selectOption('stdio'); assert.equal(await wizard.getByLabel('新工具监听端口').count(), 0);
 await create().click(); await wizard.getByText('管理员请参考生成目录 README 的 stdio 接入说明；该服务没有自动添加到项目。').waitFor();
 assert.equal(await fill().count(), 0); const last = await page.evaluate(() => window.scaffoldCalls.at(-1)); assert.equal(last.transport, 'stdio'); assert.equal(last.docker, false); assert.equal(last.port, 0); record('Docker is explicit and stdio produces administrator instructions without HTTP form import');
 await page.evaluate(() => window.scaffoldMode = 'error'); await create().click();
 await wizard.getByRole('alert').waitFor(); assert(!(await dialog.innerText()).includes('PRIVATE_NATIVE_ERROR'));
 assert.equal(await dialog.getByLabel('服务别名', { exact: true }).inputValue(), 'my_tools'); record('native failures keep saved form and show no raw native error');
 for (const theme of ['light', 'dark']) {
  await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme);
  for (const width of [1440, 760, 390]) {
   await page.setViewportSize({ width, height: width === 760 ? 480 : 900 }); await create().scrollIntoViewIfNeeded();
   assert(await create().isVisible()); assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
   await page.screenshot({ path: resolve(output, `${theme}-${width}.png`) });
  }
 }
 await page.setViewportSize({ width: 1440, height: 900 }); await dialog.locator('.mcp-service-guide > summary').focus(); await page.keyboard.press('Enter'); assert.equal(await wizard.isVisible(), false); record('light/dark 390px and short 760px windows scroll to wizard actions; keyboard guide toggle works');
 for (const mode of ['missing', 'error']) {
  await page.evaluate(mode => window.templateMode = mode, mode);
  await page.keyboard.press('Escape'); await page.getByRole('button', { name: '智能体与记忆', exact: true }).click();
  await dialog.getByRole('button', { name: 'MCP 服务', exact: true }).click(); await dialog.locator('.mcp-service-guide > summary').click();
  await wizard.getByText('此开发版本没有可用的内置模板，请使用下方命令创建独立工具目录。').waitFor();
  assert.equal(await create().count(), 0); assert(await dialog.getByLabel('创建 MCP 服务命令').isVisible());
 }
 assert.equal(writes.length, 1); assert.deepEqual(errors, []); record('missing/corrupt template falls back to CLI, with no unexpected API mutations or runtime errors');
 await writeFile(resolve(output, 'verification.json'), JSON.stringify({ checks: results, errors }, null, 2));
} finally { await browser.close(); }
