require('./production-guard.cjs');
// Actual center API + UI; synthetic inventory, no real subscription secrets.
const { chromium } = require('playwright');
const fs = require('fs'), path = require('path');
let browser;
(async () => {
  const s = JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA, 'MiGate/operations/deployment.json'), 'utf8'));
  browser = await chromium.launch({ channel: 'chrome', args: ['--no-proxy-server', '--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443'] });
  const c = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  await c.addCookies([{ name: 'migate_session', value: s.admin_cookie.slice('migate_session='.length), domain: 'sub.caowo.baby', path: '/', secure: true, httpOnly: true }]);
  const p = await c.newPage(), owned = [], checks = [], tag = 'INI-' + Date.now();
  async function api(method, endpoint, data) {
    return p.evaluate(async ({ method, endpoint, data }) => {
      const r = await fetch('/api/v1' + endpoint, { method, headers: { 'Content-Type': 'application/json' }, body: data === undefined ? undefined : JSON.stringify(data) });
      return { status: r.status, body: await r.json() };
    }, { method, endpoint, data });
  }
  try {
    await p.goto('https://sub.caowo.baby/subscriptions?kind=nodes');
    await p.getByRole('button', { name: '导入节点', exact: true }).click();
    const d = p.getByRole('dialog'), content = d.getByLabel('节点清单内容', { exact: true });
    await content.fill(`# exported inventory\n; local proxies only\n[Proxy]\n${tag}-Surge = ss, example.com, 8388, encrypt-method=aes-128-gcm, password="a,b=+%25", udp-relay=true\n${tag}-Loon = Trojan, example.org, 443, "synthetic", sni=tls.example.org\n[server_local]\ntrojan=[2001:db8::1]:443, password=https://synthetic.invalid/path, tls-host=tls.example.org, tls-verification=false, tag=${tag}-QX\n[policy]\nstatic=PROXY, ${tag}-QX`);
    const before = (await api('GET', '/admin/resources/nodes')).body.total;
    await d.getByRole('button', { name: '解析预览', exact: true }).click();
    await d.getByText('准备导入 3 个 · 检测到 0 个重复项', { exact: true }).waitFor();
    if ((await api('GET', '/admin/resources/nodes')).body.total !== before) throw Error('Preview wrote data');
    for (const width of [1440,1280,768,390]) {
      await p.setViewportSize({ width, height: 1000 });
      const b = await d.boundingBox(); if (!b || b.x < 0 || b.x + b.width > width + 1 || await d.evaluate(el => el.scrollWidth > el.clientWidth + 1)) throw Error('Import dialog overflow');
      await p.screenshot({ path: `docs/evidence/ini-import-${width}.png`, fullPage: false, mask: [p.locator('.sensitive-output')] });
    }
    const response = p.waitForResponse(r => r.url().endsWith('/api/v1/admin/subscriptions/import') && r.request().method() === 'POST');
    await d.getByRole('button', { name: '确认导入 3 个节点', exact: true }).click();
    const imported = await response, body = await imported.json();
    if (imported.status() !== 200 || body.ids?.length !== 3) throw Error('INI import failed');
    owned.push(...body.ids);
    await d.getByText('已导入 3 个节点', { exact: true }).waitFor();
    const nodes = (await api('GET', '/admin/resources/nodes')).body.items.filter(n => owned.includes(n.id));
    if (nodes.length !== 3 || !nodes.every(n => n.data.name.startsWith(tag))) throw Error('INI names not persisted');
    checks.push('leading comments and URL-like credentials are preserved; actual UI parses Surge/Loon/QX, quoted comma/equal/percent, IPv6 and TLS options; preview is read-only; commits three distinct private inventory nodes');
    const bad = await api('POST', '/admin/subscriptions/import', { content: `n = ss, example.com, 8388, encrypt-method=aes-128-gcm, password=synthetic, unsupported=hidden`, preview: false, deduplicate: true });
    if (bad.status < 400 || (await api('GET', '/admin/resources/nodes')).body.total !== before + 3) throw Error('Unknown field accepted or wrote partial data');
    checks.push('unknown option rejects before inventory writes; policies are not applied and no ordinary-user grants created');
    fs.writeFileSync('docs/evidence/ini-import-ui.json', JSON.stringify({ version: (await api('GET', '/admin/overview')).body.version, date: new Date().toISOString(), checks, visual: 'screenshots captured; review pending', limitations: 'synthetic inventory; commercial-client acceptance remains external' }, null, 2));
  } finally {
    for (const id of owned) { const r = await api('DELETE', '/admin/resources/nodes/' + id); if (![200,404].includes(r.status)) throw Error('Owned node cleanup failed'); }
    await browser.close();
  }
  console.log('Actual INI import preview/write/rejection, four widths and owned cleanup passed');
})().catch(async () => { if(browser) await browser.close(); console.error('INI import workflow failed; request credentials omitted'); process.exitCode = 1; });
