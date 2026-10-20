require('./production-guard.cjs');
const { chromium } = require('playwright');
const fs = require('fs'), path = require('path');
let browser, stage = 'launch';
(async () => {
  const dir = path.join(process.env.LOCALAPPDATA, 'MiGate/operations');
  const s = JSON.parse(fs.readFileSync(path.join(dir, 'deployment.json'), 'utf8'));
  const fixture = JSON.parse(fs.readFileSync(path.join(dir, 'certificate-name-fixture.json'), 'utf8'));
  browser = await chromium.launch({ channel: 'chrome', args: ['--no-proxy-server', '--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443'] });
  const c = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  await c.addCookies([{ name: 'migate_session', value: s.admin_cookie.slice('migate_session='.length), domain: 'sub.caowo.baby', path: '/', secure: true, httpOnly: true }]);
  const p = await c.newPage(), checks = [];
  let ownedID;
  async function request(method, endpoint, data) {
    return p.evaluate(async ({ method, endpoint, data }) => {
      const r = await fetch('/api/v1' + endpoint, { method, headers: { 'Content-Type': 'application/json' }, body: data === undefined ? undefined : JSON.stringify(data) });
      if (!r.ok) throw Error('Certificate workflow API failed'); return r.json();
    }, { method, endpoint, data });
  }
  async function waitTask(id) {
    for (let i = 0; i < 75; i++) {
      const data = await request('GET', '/admin/tasks?device_id=' + s.device_id), task = data.items.find(t => t.id === id);
      if (task?.status === 'failed') throw Error('Certificate task failed');
      if (task?.status === 'applied') return;
      await p.waitForTimeout(1000);
    }
    throw Error('Certificate task timeout');
  }
  const dialog = p.getByRole('dialog');
  async function submit() {
    await dialog.getByRole('button', { name: '核对配置', exact: true }).click();
    const response = p.waitForResponse(r => /\/devices\/\d+\/tasks$/.test(r.url()) && r.request().method() === 'POST');
    await dialog.getByRole('button', { name: '确认下发任务', exact: true }).click();
    const result = await response; if (result.status() !== 202) throw Error('Certificate form rejected');
    const data = await result.json(); ownedID = data.resource_id;
    await dialog.waitFor({ state: 'hidden' }); await waitTask(data.id);
    await request('GET', '/admin/resources/device-certificate');
    return data;
  }
  try {
    stage = 'manual import';
    await p.goto(`https://sub.caowo.baby/devices/${s.device_id}?tab=certificates`);
    await p.getByRole('button', { name: '导入证书', exact: true }).click();
    const name = '华东入口 · 历史名称验收 ' + Date.now();
    await dialog.getByLabel('证书名称', { exact: true }).fill(name);
    await dialog.getByLabel('证书链 PEM', { exact: true }).fill(fixture.certificate);
    await dialog.getByLabel('私钥 PEM（仅提交，不回显）', { exact: true }).fill(fixture.key);
    for (const width of [1440, 1280, 768, 390]) {
      await p.setViewportSize({ width, height: 1000 });
      await dialog.getByLabel('证书名称', { exact: true }).scrollIntoViewIfNeeded();
      const b = await dialog.boundingBox(); if (!b || b.x < -1 || b.x + b.width > width + 1) throw Error('Certificate form clipped');
      await p.screenshot({ path: `docs/evidence/certificate-name-import-${width}.png`, fullPage: false, mask: [p.locator('.sensitive-output')] });
    }
    await submit();
    await p.getByRole('heading', { name, exact: true }).waitFor({ timeout: 15000 });
    checks.push('actual named import through UI, device receipt, four responsive form screenshots; key/certificate content masked');
    stage = 'certificate update';
    const row = p.locator('article.inventory-item').filter({ has: p.getByRole('heading', { name, exact: true }) });
    await row.getByRole('button', { name: '更新证书', exact: true }).click();
    if (await dialog.getByLabel('证书名称', { exact: true }).inputValue() !== name) throw Error('Stored name not restored');
    const updated = name + ' · 已续换';
    await dialog.getByLabel('证书名称', { exact: true }).fill(updated);
    await dialog.getByLabel('私钥 PEM（仅提交，不回显）', { exact: true }).fill(fixture.key);
    await submit();
    await p.getByRole('heading', { name: updated, exact: true }).waitFor({ timeout: 15000 });
    const stored = (await request('GET', '/admin/resources/device-certificate')).items.find(r => Number(r.data.id) === ownedID);
    if (!stored || stored.data.name !== updated || ['key_pem', 'sealed_key'].some(k => k in stored.data)) throw Error('Name persistence/private field allowlist failed');
    const history = p.getByRole('heading', { name: '证书历史版本', exact: true }).locator('xpath=ancestor::*[@data-slot="card"][1]');
    await history.getByText(updated + ' ·', { exact: false }).first().waitFor({ timeout: 30000 });
    checks.push('actual update preloads/stores name; list and registered history use updated name; resource response has no private key');
    stage = 'TLS certificate selector';
    await p.goto(`https://sub.caowo.baby/devices/${s.device_id}?tab=inbounds`);
    await p.getByRole('button', { name: '新增入站', exact: true }).click();
    await dialog.getByRole('combobox', { name: '传输安全', exact: true }).click();
    await p.getByRole('option', { name: 'TLS', exact: true }).click();
    await dialog.getByRole('combobox', { name: '绑定证书', exact: true }).click();
    await p.getByRole('option', { name: updated, exact: false }).click();
    if (!(await dialog.getByRole('combobox', { name: '绑定证书', exact: true }).innerText()).includes(updated)) throw Error('Certificate name missing from selector');
    for (const width of [1440, 1280, 768, 390]) {
      await p.setViewportSize({ width, height: 1000 });
      await dialog.getByRole('combobox', { name: '绑定证书', exact: true }).scrollIntoViewIfNeeded();
      const bounds = await dialog.boundingBox(), field = await dialog.getByRole('combobox', { name: '绑定证书', exact: true }).boundingBox();
      if (!bounds || !field || field.x < bounds.x + 8 || field.x + field.width > bounds.x + bounds.width - 8) throw Error('Long certificate field clips dialog padding');
      await p.screenshot({ path: `docs/evidence/certificate-name-binding-${width}.png`, fullPage: false, mask: [p.locator('.sensitive-output')] });
      if (await dialog.evaluate(el => el.scrollWidth > el.clientWidth + 1)) {
        fs.writeFileSync('docs/evidence/certificate-dialog-overflow.json', JSON.stringify(await dialog.evaluate(el => ({ width: innerWidth, client: el.clientWidth, scroll: el.scrollWidth, elements: Array.from(el.querySelectorAll('*')).filter(n => n.getBoundingClientRect().right > el.getBoundingClientRect().right - 10).map(n => ({ tag: n.tagName, slot: n.getAttribute('data-slot'), class: n.className, width: n.clientWidth, scroll: n.scrollWidth })).slice(0, 30) })), null, 2));
        throw Error('Dialog internally overflows');
      }
      await dialog.getByRole('button', { name: '核对配置', exact: true }).scrollIntoViewIfNeeded();
      await p.screenshot({ path: `docs/evidence/certificate-name-footer-${width}.png`, fullPage: false, mask: [p.locator('.sensitive-output')] });
    }
    await dialog.getByRole('button', { name: '取消', exact: true }).click();
    checks.push('actual TLS resource selector displays/selects human certificate name; canceled draft creates no inbound/listener');
    fs.writeFileSync('docs/evidence/certificate-name-ui.json', JSON.stringify({ version: (await request('GET', '/admin/overview')).version, date: new Date().toISOString(), checks, visual: 'captured; human review pending' }, null, 2));
  } catch (error) {
    const allowed = ['Long certificate field clips dialog padding', 'Dialog internally overflows', 'Certificate form clipped', 'Certificate name missing from selector', 'Certificate task failed', 'Certificate task timeout'];
    console.error('Certificate name workflow failed at stage: ' + stage + (allowed.includes(error.message) ? ' / ' + error.message : ' / browser or API operation'));
    throw error;
  } finally {
    stage = 'owned fixture cleanup';
    if (ownedID) { const task = await request('POST', `/admin/devices/${s.device_id}/tasks`, { operation: 'certificate.delete', payload: { id: ownedID }, confirm: true }); await waitTask(task.id); }
    await browser.close();
  }
  console.log('Actual named certificate import/update/history and TLS selector passed');
})().catch(async () => { if (browser) await browser.close(); console.error('Certificate name workflow failed at stage: ' + stage); process.exitCode = 1; });
