require('./production-guard.cjs');
const { chromium } = require('playwright');
const fs = require('fs'), path = require('path');
let browser;
(async () => {
  const s = JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA, 'MiGate/operations/deployment.json'), 'utf8'));
  browser = await chromium.launch({ channel: 'chrome', args: ['--no-proxy-server', '--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443'] });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  await context.addCookies([{ name: 'migate_session', value: s.admin_cookie.slice('migate_session='.length), domain: 'sub.caowo.baby', path: '/', secure: true, httpOnly: true }]);
  const page = await context.newPage(), errors = [], checks = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.goto(`https://sub.caowo.baby/devices/${s.device_id}?tab=certificates`);
  const panel = page.getByRole('heading', { name: 'DNS 验证记录清理', exact: true }).locator('xpath=ancestor::*[@data-slot="card"][1]');
  await panel.getByRole('status').filter({ hasText: '尚无 TXT 验证记录' }).waitFor();
  const actual = await page.evaluate(async id => { const r = await fetch(`/api/v1/admin/devices/${id}/dns-challenges`); return { status: r.status, data: await r.json() }; }, s.device_id);
  if (actual.status !== 200 || actual.data.total !== 0 || actual.data.items.length !== 0) throw Error('Expected genuinely empty DNS ledger while external DNS remains unconfigured');
  for (const width of [1440, 1280, 768, 390]) {
    await page.setViewportSize({ width, height: 1000 }); await panel.scrollIntoViewIfNeeded();
    if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw Error('DNS cleanup overflow ' + width);
    await page.screenshot({ path: `docs/evidence/dns-cleanup-${width}.png`, fullPage: true });
  }
  checks.push('actual device certificate route, real empty API response; no fabricated TXT history and no DNS calls');
  const endpoint = `**/api/v1/admin/devices/${s.device_id}/dns-challenges*`;
  await page.route(endpoint, route => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'query_failed' } }) }));
  await panel.getByRole('button', { name: '刷新记录', exact: true }).click();
  await panel.getByRole('alert').filter({ hasText: '暂时无法加载' }).waitFor();
  if (await panel.getByText('尚无 TXT 验证记录', { exact: true }).count()) throw Error('DNS load failure displayed as empty ledger');
  await panel.scrollIntoViewIfNeeded(); await page.screenshot({ path: 'docs/evidence/dns-cleanup-error-390.png', fullPage: true });
  await page.unroute(endpoint); await panel.getByRole('button', { name: '重新加载', exact: true }).click();
  await panel.getByText('尚无 TXT 验证记录', { exact: true }).waitFor();
  const nonexisting = await page.evaluate(async id => (await fetch(`/api/v1/admin/devices/${id}/dns-challenges/2147483647/retry`, { method: 'POST' })).status, s.device_id);
  if (nonexisting !== 409) throw Error('Missing cleanup target accepted');
  checks.push('injected 503 shown as error, real retry restored ledger; actual missing retry target refused with 409');
  const version = await page.evaluate(async () => (await (await fetch('/api/v1/admin/overview')).json()).version);
  if (errors.length) throw Error('DNS cleanup runtime error');
  fs.writeFileSync('docs/evidence/dns-cleanup-ui.json', JSON.stringify({ version, date: new Date().toISOString(), checks, visual: 'four widths plus error captured; human review pending', limitations: 'Populated lifecycle exercised in isolated PostgreSQL with controlled DNS provider; no real Cloudflare/CA success claimed' }, null, 2));
  await browser.close(); console.log('Actual domain DNS cleanup empty/error/retry and missing-target rejection passed');
})().catch(async e => { if (browser) await browser.close(); console.error(e.message); process.exitCode = 1; });
