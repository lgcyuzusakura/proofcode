const { chromium } = require(process.env.PROOFCODE_PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const apiBase = process.env.PROOFCODE_UI_CONTROL_URL || 'http://127.0.0.1:18097';
const web = process.env.PROOFCODE_UI_URL || 'http://127.0.0.1:15175/';
const token = process.env.PROOFCODE_UI_TOKEN || 'experiment-smoke-user';
const output = process.env.PROOFCODE_UI_OUTPUT || 'smoke/visual/results';
fs.mkdirSync(output, { recursive: true });
async function api(path, body) {
  const response = await fetch(apiBase + path, { method: body === undefined ? 'GET' : 'POST', headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
  assert(response.ok, `${path}: ${response.status} ${await response.clone().text()}`);
  return response.status === 204 ? undefined : response.json();
}
(async () => {
  const project = await api('/api/projects', { name: `可视化验收 ${Date.now()}`, sourceKind: 'SCRATCH', defaultBranch: 'main', bootstrapId: crypto.randomUUID() });
  const scope = await api(`/api/projects/${project.id}/default-scope`, {});
  const postgres = await api(`/api/projects/${project.id}/data/resources`, { name: '验收数据库', provider: 'postgres', environment: 'dev', secretRef: 'SMOKE_DB', allowedSchema: 'public' });
  const redis = await api(`/api/projects/${project.id}/data/resources`, { name: '验收缓存', provider: 'redis', environment: 'dev', secretRef: 'SMOKE_REDIS', allowedSchema: [] });
  const browser = await chromium.launch({ channel: 'msedge', headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = []; page.on('pageerror', error => errors.push(String(error)));
  let executionRequests = 0;
  await page.addInitScript(value => { localStorage.setItem('proofcode.token', value.token); localStorage.setItem('proofcode.selected-scope', JSON.stringify(value.scope)); }, { token, scope });
  await page.route('**/api/**', async route => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.pathname.endsWith('/execute')) executionRequests++;
    if (url.pathname === '/api/tasks') return route.fulfill({ contentType: 'application/json', body: JSON.stringify([{ id: 'visual-task-0001', projectId: project.id, workspaceId: scope.workspaceId, conversationId: scope.conversationId, prompt: '验证执行链时间线展示', model: 'fixture-model', executionMode: 'CODE', status: 'SUCCEEDED', createdAt: new Date().toISOString() }]) });
    if (url.pathname === '/api/tasks/visual-task-0001/events') return route.fulfill({ contentType: 'application/json', body: JSON.stringify([{ sequence: 1, type: 'tool.requested', timestamp: new Date().toISOString(), payload: { tool: 'search_code', callId: 'fixture-call-1' } }, { sequence: 2, type: 'tool.completed', timestamp: new Date().toISOString(), payload: { tool: 'search_code', result: { matches: 4 } } }, { sequence: 3, type: 'verification.completed', timestamp: new Date().toISOString(), payload: { report: '4 项检查全部通过' } }, { sequence: 4, type: 'task.completed', timestamp: new Date().toISOString(), payload: { result: '已完成' } }]) });
    if (url.pathname === '/api/tasks/visual-task-0001/artifacts') return route.fulfill({ contentType: 'application/json', body: '[]' });
    if (url.pathname === '/api/tasks/visual-task-0001/approvals') return route.fulfill({ contentType: 'application/json', body: JSON.stringify([{ id: 'fixture-approval', taskId: 'visual-task-0001', callId: 'fixture-call-2', tool: 'run_command', risk: 'HIGH', arguments: { command: 'npm test', reason: '确认本次代码修改通过测试' }, status: 'PENDING', createdAt: new Date().toISOString() }]) });
    const response = await fetch(apiBase + url.pathname + url.search, { method: request.method(), headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: ['GET', 'HEAD'].includes(request.method()) ? undefined : request.postData() });
    let body = await response.text();
    if (url.pathname.endsWith('/schema') && response.ok && JSON.parse(request.postData()).resourceId === postgres.id) {
      const snapshot = JSON.parse(body);
      const parent = snapshot.metadata.tables[0];
      snapshot.metadata.tables.push({ schema: 'public', name: 'visual_orders', columns: [{ ...parent.columns[0], name: 'id', type: 'int4' }, { ...parent.columns[0], name: 'demo_id', type: 'int4' }, { ...parent.columns[1], name: 'total', type: 'numeric' }], primaryKey: ['id'], foreignKeys: [{ name: 'fk_visual_orders_demo', columns: ['demo_id'], referencesSchema: parent.schema, referencesTable: parent.name, referencesColumns: [parent.primaryKey[0]] }], indexes: [] });
      body = JSON.stringify(snapshot);
    }
    await route.fulfill({ status: response.status, contentType: 'application/json', body });
  });
  await page.goto(web); await page.getByRole('button', { name: '数据库与缓存', exact: true }).click();
  await page.getByRole('button', { name: /验收数据库/ }).click(); await page.getByRole('button', { name: '读取连接与结构' }).click();
  await page.getByRole('button', { name: '表关系图', exact: true }).click(); await page.locator('.schema-diagram').waitFor();
  assert(await page.locator('.diagram-table').count() > 0, 'schema diagram has no table cards');
  await page.screenshot({ path: `${output}/visual-database-schema.png`, fullPage: true });
  await page.getByRole('button', { name: /验收缓存/ }).click(); await page.getByRole('button', { name: '读取连接与结构' }).click();
  await page.locator('.namespace-card').waitFor();
  await page.getByRole('button', { name: '生成缓存计划', exact: true }).click();
  await page.getByRole('button', { name: '批准此计划', exact: true }).waitFor();
  assert.equal(executionRequests, 0, 'an operation executed before approval');
  assert.equal(await page.getByRole('button', { name: '执行已批准计划', exact: true }).count(), 0);
  await page.screenshot({ path: `${output}/visual-redis-workbench.png`, fullPage: true });
  await page.getByRole('button', { name: '批准此计划', exact: true }).click();
  await page.getByRole('button', { name: '执行已批准计划', exact: true }).click();
  await page.locator('.plan-inspector .data-status.succeeded').waitFor();
  assert.equal(executionRequests, 1);
  await page.getByRole('button', { name: '计划与恢复', exact: true }).click(); await page.screenshot({ path: `${output}/visual-plan-history.png`, fullPage: true });
  await page.getByRole('button', { name: '代码审查', exact: true }).click(); await page.locator('.approval-heading').waitFor();
  assert.equal(await page.locator('.approval-heading h3').textContent(), '运行命令等待你的确认');
  await page.screenshot({ path: `${output}/visual-review.png`, fullPage: true });
  await page.getByRole('button', { name: '执行链', exact: true }).click(); await page.locator('.chain-event').first().waitFor(); await page.screenshot({ path: `${output}/visual-execution-chain.png`, fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 }); await page.getByRole('button', { name: '收起导航', exact: true }).click();
  await page.waitForFunction(() => document.querySelector('.nav-rail').getBoundingClientRect().width <= 59);
  await page.screenshot({ path: `${output}/visual-mobile.png`, fullPage: true });
  await page.getByRole('button', { name: '数据库与缓存', exact: true }).click(); await page.getByRole('button', { name: /验收缓存/ }).click(); await page.getByRole('button', { name: '读取连接与结构' }).click(); await page.locator('.namespace-card').waitFor();
  await page.locator('.route-overlay').evaluate(element => element.scrollTop = 0);
  await page.screenshot({ path: `${output}/visual-cache-mobile.png`, fullPage: true });
  const dimensions = await page.evaluate(() => ({ document: document.documentElement.scrollWidth, viewport: innerWidth }));
  assert(dimensions.document <= dimensions.viewport + 1, JSON.stringify(dimensions)); assert.deepEqual(errors, []);
  fs.writeFileSync(`${output}/visual-ui-results.json`, JSON.stringify({ ok: true, projectId: project.id, postgresResource: postgres.id, redisResource: redis.id, beforeApprovalExecutions: 0, approvedScanExecutions: executionRequests, syntheticFixtures: ['visual_orders foreign key', 'execution timeline', 'command approval'], dimensions, errors }, null, 2));
  await browser.close(); console.log('VISUAL_UI_SMOKE_PASSED');
})().catch(error => { console.error(error); process.exit(1); });

