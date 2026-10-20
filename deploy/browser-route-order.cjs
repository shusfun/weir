require('./production-guard.cjs');
// Invoked by verify-live-route-order.py with its owned direct rule ID.
const { chromium } = require('playwright');
const fs = require('fs'), path = require('path');
let browser;
(async () => {
  const directRule = Number(process.argv[2]); if (!Number.isSafeInteger(directRule) || directRule <= 0) throw Error('Owned rule ID required');
  const state = JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA, 'MiGate/operations/deployment.json'), 'utf8'));
  browser = await chromium.launch({ channel: 'chrome', args: ['--no-proxy-server', '--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443'] });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  await context.addCookies([{ name: 'migate_session', value: state.admin_cookie.slice('migate_session='.length), domain: 'sub.caowo.baby', path: '/', secure: true, httpOnly: true }]);
  const page = await context.newPage(), errors = [], checks = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.goto(`https://sub.caowo.baby/devices/${state.device_id}?tab=routing`);
  const adjust = page.getByRole('button', { name: '调整顺序', exact: true });
  await adjust.waitFor(); await adjust.click();
  const dialog = page.getByRole('dialog', { name: '调整路由匹配顺序', exact: true });
  const up = dialog.getByRole('button', { name: new RegExp('^上移 .*规则 #' + directRule + '$') });
  // Preserve pre-existing relative order; move this owned direct rule above the
  // matching owned deny rule (the runner placed the pair at the highest priority).
  await up.click();
  for (const width of [1440, 1280, 768, 390]) {
    await page.setViewportSize({ width, height: 1000 });
    const box = await dialog.boundingBox();
    if (!box || box.x < 0 || box.x + box.width > width + 1) throw Error('Route order dialog clipped ' + width);
    await page.screenshot({ path: `docs/evidence/route-order-dialog-${width}.png`, fullPage: true });
  }
  // Reject a stale editor explicitly; no optimistic applied state is permitted.
  await page.route(`**/api/v1/admin/devices/${state.device_id}/tasks`, route => route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ error: { code: 'routing_order_changed' } }) }));
  await dialog.getByRole('button', { name: '确认提交排序任务', exact: true }).click();
  await dialog.getByRole('alert').filter({ hasText: '路由列表或顺序已变化' }).waitFor();
  await page.screenshot({ path: 'docs/evidence/route-order-conflict-390.png', fullPage: true });
  await page.unroute(`**/api/v1/admin/devices/${state.device_id}/tasks`);
  await dialog.getByRole('button', { name: '取消', exact: true }).click(); await adjust.click(); await dialog.getByRole('button', { name: new RegExp('^上移 .*规则 #' + directRule + '$') }).click();
  const saved = page.waitForResponse(r => r.url().endsWith(`/api/v1/admin/devices/${state.device_id}/tasks`) && r.request().method() === 'POST');
  await dialog.getByRole('button', { name: '确认提交排序任务', exact: true }).click();
  const response = await saved; if (response.status() !== 202) throw Error('Route order task enqueue failed');
  const task = await response.json(); await dialog.waitFor({ state: 'hidden' });
  for (let i = 0; i < 90; i++) {
    const result = await page.evaluate(async id => (await (await fetch('/api/v1/admin/tasks?q=' + id)).json()).items[0], task.id);
    if (result?.status === 'failed') throw Error('Real route ordering task failed');
    if (result?.status === 'applied') break;
    if (i === 89) throw Error('Route ordering task timed out');
    await page.waitForTimeout(1000);
  }
  await page.reload();
  await page.getByRole('heading', { name: '匹配优先级', exact: true }).waitFor();
  const rows = page.locator('article.inventory-item');
  const first = rows.first();
  if (!(await first.innerText()).includes('资源 #' + directRule)) throw Error('Applied list did not follow actual order');
  for (const width of [1440, 1280, 768, 390]) {
    await page.setViewportSize({ width, height: 1000 });
    if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw Error('Route list overflow');
    await page.screenshot({ path: `docs/evidence/route-order-applied-${width}.png`, fullPage: true });
  }
  const version = await page.evaluate(async () => (await (await fetch('/api/v1/admin/overview')).json()).version);
  checks.push('actual Gate route tab, named target selection, priority move; four widths', 'injected stale-editor 409 remains visible and does not report applied', 'actual atomic task 202, wait for applied receipt and refreshed route order');
  if (errors.length) throw Error('Routing UI runtime errors');
  fs.writeFileSync('docs/evidence/route-order-ui.json', JSON.stringify({ version, date: new Date().toISOString(), task_id: task.id, checks, visual: 'captured; human review pending' }, null, 2));
  await browser.close(); console.log('Real route order dialog, conflict and applied list passed');
})().catch(async e => { if (browser) await browser.close(); console.error(e.message); process.exitCode = 1; });
