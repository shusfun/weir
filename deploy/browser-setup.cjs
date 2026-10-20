// Disposable local installation against deploy/test.compose.yml. No remote credentials.
const { chromium } = require('playwright');
const fs = require('fs'), os = require('os'), path = require('path'), cp = require('child_process');
let browser, server, scratch, stackOwned = false;
function command(binary, args, options = {}) { return cp.execFileSync(binary, args, { encoding: 'utf8', windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'], ...options }); }
const compose = (...args) => command('docker', ['compose', '-f', 'deploy/test.compose.yml', ...args]);
(async () => {
  if (command('docker', ['ps', '-aq', '--filter', 'label=com.docker.compose.project=migate-local-test']).trim()) throw Error('Dedicated test stack is in use; refusing to reset it');
  compose('up', '-d', '--wait'); stackOwned = true;
  const executable = path.resolve('dist/migate-setup-acceptance.exe');
  command('go', ['build', '-o', executable, './apps/control']);
  scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'migate-setup-browser-'));
  const args = ['--config', path.join(scratch, 'config.json'), '--listen', '127.0.0.1:19812', '--public-url', 'http://localhost:19812'];
  const output = command(executable, [...args, '--init']);
  const token = output.trim().split(/\s+/).at(-1); if (!token || token.length < 20) throw Error('Initialization token not generated');
  server = cp.spawn(executable, args, { windowsHide: true, stdio: 'ignore' });
  let ready = false; for (let i = 0; i < 100; i++) { try { const r = await fetch('http://localhost:19812/healthz'); if (r.ok) { ready = true; break; } } catch {} await new Promise(r => setTimeout(r, 100)); }
  if (!ready) throw Error('Isolated setup server not ready');
  browser = await chromium.launch({ channel: 'chrome', args: ['--no-proxy-server'] });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } }), page = await context.newPage(), errors = [], checks = [];
  page.on('pageerror', e => errors.push(e.message));
  async function shots(name) { for (const width of [1440, 1280, 768, 390]) { await page.setViewportSize({ width, height: 1000 }); if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw Error('Setup overflow ' + name); await page.screenshot({ path: `docs/evidence/setup-new-${name}-${width}.png`, fullPage: true, mask: [page.locator('input[type="password"]'), page.locator('input[type="email"]')] }); } }
  await page.goto('http://localhost:19812/'); await page.getByRole('heading', { name: '初始化你的控制中心', exact: true }).waitFor(); await shots('token');
  await page.getByLabel('短时初始化凭证', { exact: true }).fill('invalid-isolated-token'); await page.getByRole('button', { name: '下一步', exact: true }).click();
  await page.getByRole('alert').filter({ hasText: '初始化凭证无效或已过期' }).waitFor();
  if (await page.getByLabel('主机', { exact: true }).count()) throw Error('Invalid token advanced installer');
  await page.getByLabel('短时初始化凭证', { exact: true }).fill(token); await page.getByRole('button', { name: '下一步', exact: true }).click();
  await page.getByRole('heading', { name: '连接 PostgreSQL', exact: true }).waitFor(); if(!(await page.getByLabel('SSL 模式',{exact:true}).innerText()).includes('关闭（同机本地连接）'))throw Error('SSL selector rendered raw value'); await shots('postgres');
  await page.getByLabel('端口', { exact: true }).fill('25432'); await page.getByLabel('密码', { exact: true }).fill('incorrect-local-test'); await page.getByRole('button', { name: '下一步', exact: true }).click();
  await page.getByRole('alert').filter({ hasText: 'PostgreSQL 连接失败' }).waitFor(); await page.screenshot({ path: 'docs/evidence/setup-new-postgres-failure-390.png', mask: [page.locator('input[type="password"]')] });
  await page.getByLabel('密码', { exact: true }).fill('local-test-only'); await page.getByRole('button', { name: '下一步', exact: true }).click();
  await page.getByRole('heading', { name: '连接 Redis', exact: true }).waitFor(); await shots('redis');
  await page.getByLabel('连接地址', { exact: true }).fill('127.0.0.1:26379'); await page.getByLabel('密码', { exact: true }).fill('wrong-local-test'); await page.getByRole('button', { name: '下一步', exact: true }).click();
  await page.getByRole('alert').waitFor(); await page.getByLabel('密码', { exact: true }).fill('local-test-only'); await page.getByRole('button', { name: '下一步', exact: true }).click();
  await page.getByRole('heading', { name: '创建首位管理员', exact: true }).waitFor(); await shots('admin');
  await page.getByLabel('管理员邮箱', { exact: true }).fill('admin@isolated.test'); await page.getByLabel('密码（12–72 字节）', { exact: true }).fill('isolated-browser-password'); await page.getByLabel('确认管理员密码', { exact: true }).fill('mismatched-isolated-password');
  await page.getByRole('button', { name: '安装并锁定', exact: true }).click(); await page.getByRole('alert').filter({ hasText: '两次输入的密码不一致' }).waitFor();
  await page.getByLabel('确认管理员密码', { exact: true }).fill('isolated-browser-password'); await page.getByRole('button', { name: '安装并锁定', exact: true }).click();
  await page.getByRole('heading', { name: '欢迎回来', exact: true }).waitFor();
  await page.getByLabel('邮箱', { exact: true }).fill('admin@isolated.test'); await page.getByLabel('密码', { exact: true }).fill('isolated-browser-password'); await page.getByRole('button', { name: '登录', exact: true }).click(); await page.getByRole('heading', { name: '概览', exact: true }).waitFor();
  const result = await page.evaluate(async token => ({ me: await (await fetch('/api/v1/me')).json(), lock: (await fetch('/api/v1/setup/verify', { method: 'POST', headers: { Authorization: 'Bearer ' + token } })).status, policy: await (await fetch('/api/v1/public')).json() }), token);
  if (result.me.role !== 'admin' || result.lock !== 409 || result.policy.registration_enabled !== false) throw Error('Installed role, registration default or installation lock failed');
  checks.push('actual credential verification refuses invalid token before database step', 'actual PostgreSQL bad-password rejection then corrected connection', 'actual Redis bad-password rejection then corrected ACL connection', 'administrator password mismatch refusal; actual UI install and new-admin login; registration closed and initialization locked');
  if (errors.length) throw Error('Setup browser runtime errors');
  fs.writeFileSync('docs/evidence/setup-new-ui.json', JSON.stringify({ date: new Date().toISOString(), target: 'disposable local control binary + dedicated PG18/Redis8 containers', checks, visual: '16 screenshots captured; human review pending', production: 'Production initialization state not modified' }, null, 2));
  console.log('Isolated actual setup UI, dependency retries, administrator login and installation lock passed');
})().catch(e => { console.error(e.message); process.exitCode = 1; }).finally(async () => {
  if (browser) await browser.close();
  if (server) { server.kill(); await new Promise(r => setTimeout(r, 500)); }
  if (stackOwned) compose('down');
  if (scratch && path.dirname(scratch) === os.tmpdir() && path.basename(scratch).startsWith('migate-setup-browser-')) fs.rmSync(scratch, { recursive: true });
});
