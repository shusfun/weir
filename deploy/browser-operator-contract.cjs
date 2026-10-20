require('./production-guard.cjs');
// Real domain UI rejection/recovery. Synthetic inventory never grants access.
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
  let ownedId;
  async function request(method, endpoint, data) {
    return page.evaluate(async ({ method, endpoint, data }) => {
      const r = await fetch('/api/v1' + endpoint, { method, headers: { 'Content-Type': 'application/json' }, body: data === undefined ? undefined : JSON.stringify(data) });
      return { status: r.status, data: await r.json() };
    }, { method, endpoint, data });
  }
  try {
    await page.goto('https://sub.caowo.baby/subscriptions?kind=operators');
    for (const links of [[], ['ss://YWVzLTEyOC1nY206c3ludGhldGlj@example.com:8388#HK']]) {
      for (const op of [
        { type: 'dedup', params: { unknown: true } },
        { type: 'filter', params: { include: { enabled: true, rules: [{ pattern: 'HK', unknown: true }] } } },
        { type: 'script', params: { dsl: [{ action: 'filter', field: 'unsupported' }] } },
      ]) {
        const result = await request('POST', '/admin/subscriptions/preview', { links, operators: [op], target: 'base64' });
        if (result.status !== 422) throw Error('Unsupported settings silently accepted in live preview');
      }
    }
    checks.push('actual API rejects unknown params/nested rule/DSL field for empty and populated input');
    await page.getByRole('button', { name: '新增处理链', exact: true }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByRole('button', { name: '高级 JSON', exact: true }).click();
    const field = dialog.getByLabel('operators JSON', { exact: true });
    const data = { name: 'contract-acceptance-' + Date.now(), operators: [{ type: 'dedup', params: { unsupported: true } }] };
    await field.fill(JSON.stringify(data, null, 2));
    let response = page.waitForResponse(r => r.url().endsWith('/resources/operators') && r.request().method() === 'POST');
    await dialog.getByRole('button', { name: '保存', exact: true }).click();
    if ((await response).status() !== 400) throw Error('Invalid UI save was accepted');
    await dialog.getByRole('status').filter({ hasText: '未知字段不会被忽略' }).waitFor();
    for (const width of [1440, 1280, 768, 390]) {
      await page.setViewportSize({ width, height: 1000 });
      await page.waitForFunction(() => document.documentElement.scrollWidth <= innerWidth, null, { timeout: 2000 });
      const bounds = await dialog.boundingBox();
      if (!bounds || bounds.x < -1 || bounds.x + bounds.width > width + 1) throw Error('Contract dialog clipped');
      await page.screenshot({ path: `docs/evidence/operator-contract-${width}.png`, fullPage: true });
    }
    data.operators[0].params = { includeProtocol: true };
    await field.fill(JSON.stringify(data, null, 2));
    response = page.waitForResponse(r => r.url().endsWith('/resources/operators') && r.request().method() === 'POST');
    await dialog.getByRole('button', { name: '保存', exact: true }).click();
    const saved = await response;
    if (!saved.ok()) throw Error('Compatible corrected form failed to save');
    ownedId = (await saved.json()).id;
    await dialog.waitFor({ state: 'hidden' });
    const stored = await request('GET', '/admin/resources/operators?search=' + encodeURIComponent(data.name));
    if (!stored.data.items.some(r => r.id === ownedId && r.data.operators[0].params.includeProtocol === true)) throw Error('Corrected setting not persisted');
    checks.push('actual UI 400 stays open with actionable error; corrected camelCase setting saves and persists; four dialog widths');
    if (errors.length) throw Error('Contract UI runtime errors');
    fs.writeFileSync('docs/evidence/operator-contract-ui.json', JSON.stringify({ date: new Date().toISOString(), version: (await request('GET', '/admin/overview')).data.version, checks, visual: 'captured; human review pending' }, null, 2));
    console.log('Real unknown-param rejection, UI save error, correction and persistence passed');
  } finally {
    if (ownedId) {
      const result = await request('DELETE', '/admin/resources/operators/' + ownedId);
      if (result.status !== 200) throw Error('Owned contract fixture cleanup failed');
    }
    await browser.close();
  }
})().catch(async e => { if (browser) await browser.close(); console.error(e.message); process.exitCode = 1; });
