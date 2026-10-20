require('./production-guard.cjs');
// Presentation states injected in browser only; not real Gate fault evidence.
const { chromium } = require('playwright');
const fs = require('fs'), path = require('path');
let browser;
(async () => {
  const s = JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA, 'MiGate/operations/deployment.json'), 'utf8'));
  browser = await chromium.launch({ channel: 'chrome', args: ['--no-proxy-server', '--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443'] });
  const c = await browser.newContext({ viewport: { width: 390, height: 1000 } });
  await c.addCookies([{ name: 'migate_session', value: s.admin_cookie.slice('migate_session='.length), domain: 'sub.caowo.baby', path: '/', secure: true, httpOnly: true }]);
  const p = await c.newPage(), checks = [];
  await p.goto('https://sub.caowo.baby/overview');
  const actual = await p.evaluate(async () => (await (await fetch('/api/v1/admin/devices')).json()));
  let state = 'unknown';
  await p.route('**/api/v1/admin/devices', async route => {
    const data = structuredClone(actual), d = data.items.find(row => row.id === s.device_id);
    d.status = 'approved'; d.metadata.core_status.validity = 'fresh';
    if (state === 'unsupported') d.metadata.capabilities = (d.metadata.capabilities ?? []).filter(cap => cap !== 'certificate.history.v1');
    else d.metadata.core_status.certificate_history = state === 'unknown' ? { validity: 'unknown' } : { validity: 'fresh', observed_at: new Date().toISOString(), total: 0, items: [], removable: 0, preview_digest: 'a'.repeat(64) };
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(data) });
  });
  for (const value of ['unknown', 'empty', 'unsupported']) {
    state = value;
    await p.goto(`https://sub.caowo.baby/devices/${s.device_id}?tab=certificates`);
    const panel = p.getByRole('heading', { name: '证书历史版本', exact: true }).locator('xpath=ancestor::*[@data-slot="card"][1]');
    const text = value === 'unknown' ? '历史观测未知或已过期' : value === 'empty' ? '尚无登记版本' : '此 Agent 尚不支持历史管理';
    await panel.getByRole('status').filter({ hasText: text }).waitFor();
    if (value === 'empty' && await panel.getByRole('button', { name: /^清理历史版本/ }).isEnabled()) throw Error('Empty state permits cleanup');
    if (value !== 'empty' && await panel.getByRole('button', { name: /^清理历史版本/ }).count()) throw Error('Unknown/unsupported state permits cleanup');
    await panel.scrollIntoViewIfNeeded();
    await p.screenshot({ path: `docs/evidence/certificate-history-${value}-injected-390.png`, fullPage: true });
    checks.push(`browser-only injected ${value} state has explicit status and no executable cleanup`);
  }
  fs.writeFileSync('docs/evidence/certificate-history-states-ui.json', JSON.stringify({ date: new Date().toISOString(), checks, limitation: 'presentation injection only; real helper failures and files recorded separately', visual: 'captured; human review pending' }, null, 2));
  await browser.close(); console.log('Injected unknown/empty/old-Agent certificate history presentation passed');
})().catch(async e => { if (browser) await browser.close(); console.error(e.message); process.exitCode = 1; });
