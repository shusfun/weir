require('./production-guard.cjs');
const { chromium } = require('playwright');
const fs = require('fs'), path = require('path');
let browser;
(async () => {
  const state = JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA, 'MiGate/operations/deployment.json'), 'utf8'));
  browser = await chromium.launch({ channel: 'chrome', args: ['--no-proxy-server', '--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443'] });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  await context.addCookies([{ name: 'migate_session', value: state.admin_cookie.slice('migate_session='.length), domain: 'sub.caowo.baby', path: '/', secure: true, httpOnly: true }]);
  const page = await context.newPage(), errors = [], checks = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.goto(`https://sub.caowo.baby/devices/${state.device_id}`);
  const panel = page.getByRole('heading', { name: '流量历史', exact: true }).locator('xpath=ancestor::*[@data-slot="card"][1]');
  await panel.getByRole('button', { name: '查看最近采样明细', exact: true }).click();
  await panel.getByLabel('实际采样明细').waitFor();
  const actual = await page.evaluate(async id => (await (await fetch(`/api/v1/admin/devices/${id}/traffic-history`)).json()), state.device_id);
  if (!actual.items.length || !actual.items.some(r => r.quality === 'fresh' && Number(r.delta_bytes) > 0) || !actual.items.some(r => r.quality === 'unknown' && r.delta_bytes === null)) throw Error('Actual positive and unknown intervals required before browser acceptance');
  for (const row of actual.items) if ('identity' in row || 'baseline_fresh' in row) throw Error('Private sampling field leaked');
  for (const width of [1440, 1280, 768, 390]) {
    await page.setViewportSize({ width, height: 1000 });
    await page.waitForFunction(() => document.documentElement.scrollWidth <= innerWidth, null, { timeout: 2000 });
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw Error('History overflow ' + width);
    await page.screenshot({ path: `docs/evidence/traffic-history-${width}.png`, fullPage: true });
  }
  await panel.getByRole('combobox', { name: '流量时间范围', exact: true }).click(); await page.getByRole('option', { name: '最近 24 小时', exact: true }).click();
  await panel.getByRole('combobox', { name: '流量时间范围', exact: true }).filter({ hasText: '最近 24 小时' }).waitFor();
  checks.push('real accepted-settlement API positive and unknown intervals; private fields omitted; chart and detail four widths; 24-hour selector');
  const endpoint = `**/api/v1/admin/devices/${state.device_id}/traffic-history*`;
  await page.route(endpoint, r => r.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'observation_unavailable' } }) }));
  await panel.getByRole('button', { name: '刷新流量历史', exact: true }).click();
  await panel.getByRole('alert').filter({ hasText: '暂时无法加载' }).waitFor();
  if (await panel.locator('[data-slot=chart]').count()) throw Error('Error retained a healthy curve');
  await page.screenshot({ path: 'docs/evidence/traffic-history-error-390.png', fullPage: true });
  await page.unroute(endpoint); await panel.getByRole('button', { name: '重新加载', exact: true }).click();
  await panel.locator('[data-slot=chart]').waitFor();
  // Presentation-only empty/loading states are marked as injected, never as
  // real historical evidence or a successful live flow.
  await page.route(endpoint, r => r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [], observed_at: new Date().toISOString(), source: 'accepted_grant_settlements', retention_days: 7 }) }));
  await panel.getByRole('button', { name: '刷新流量历史', exact: true }).click(); await panel.getByText('尚无历史采样', { exact: true }).waitFor();
  await page.screenshot({ path: 'docs/evidence/traffic-history-empty-injected-390.png', fullPage: true });
  await page.unroute(endpoint);
  let resume;
  const pending = new Promise(resolve => { resume = resolve; });
  await page.route(endpoint, async route => { await pending; await route.continue(); });
  await page.reload({ waitUntil: 'domcontentloaded' });
  await panel.getByRole('status').filter({ hasText: '正在加载数据' }).waitFor();
  await page.screenshot({ path: 'docs/evidence/traffic-history-loading-injected-390.png', fullPage: true });
  resume(); await panel.locator('[data-slot=chart]').waitFor(); await page.unroute(endpoint);
  await page.goto('https://sub.caowo.baby/overview'); await page.getByRole('heading', { name: '流量历史', exact: true }).waitFor();
  if (!(await page.getByRole('combobox', { name: '选择流量观测 Gate', exact: true }).innerText()).includes('gate-154')) throw Error('Overview did not name selected Gate');
  for (const width of [1440, 1280, 768, 390]) { await page.setViewportSize({ width, height: 1000 }); await page.waitForFunction(() => document.documentElement.scrollWidth <= innerWidth, null, { timeout: 2000 }); await page.screenshot({ path: `docs/evidence/traffic-overview-${width}.png`, fullPage: true }); }
  checks.push('injected 503 removes healthy display, actual retry restores; injected empty presentation explicitly labeled; actual overview device selector and chart four widths');
  const version = await page.evaluate(async () => (await (await fetch('/api/v1/admin/overview')).json()).version);
  if (errors.length) throw Error('History UI runtime error');
  fs.writeFileSync('docs/evidence/traffic-history-ui.json', JSON.stringify({ version, date: new Date().toISOString(), checks, visual: 'captured; human review pending', real_data: 'live-traffic-history.json', limitations: 'accepted authorized traffic aggregate, not interface throughput; injected empty/error screenshots are presentation tests' }, null, 2));
  await browser.close(); console.log('Actual history/overview charts, time range, details and injected error/empty presentation passed');
})().catch(async e => { if (browser) await browser.close(); console.error(e.message); process.exitCode = 1; });
