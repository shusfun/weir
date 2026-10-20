require('./production-guard.cjs');
// Real domain authentication; secrets remain in the operator directory/in memory.
const { chromium } = require('playwright');
const fs = require('fs'), path = require('path'), crypto = require('crypto');
let browser;
function otp(secret) {
  let bits = ''; for (const c of secret.toUpperCase()) bits += 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'.indexOf(c).toString(2).padStart(5, '0');
  const bytes = []; for (let i = 0; i + 8 <= bits.length; i += 8) bytes.push(parseInt(bits.slice(i, i + 8), 2));
  const counter = Buffer.alloc(8); counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30000)));
  const h = crypto.createHmac('sha1', Buffer.from(bytes)).update(counter).digest(), o = h[19] & 15;
  return ((h.readUInt32BE(o) & 0x7fffffff) % 1000000).toString().padStart(6, '0');
}
(async () => {
  const s = JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA, 'MiGate/operations/deployment.json'), 'utf8'));
  browser = await chromium.launch({ channel: 'chrome', args: ['--no-proxy-server', '--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443'] });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } }), page = await context.newPage();
  const checks = [], errors = []; page.on('pageerror', e => errors.push(e.message));
  async function screenshots(name) {
    for (const width of [1440, 1280, 768, 390]) {
      await page.setViewportSize({ width, height: 1000 });
      if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw Error('Authentication overflow ' + name + ' ' + width);
      await page.screenshot({ path: `docs/evidence/auth-${name}-${width}.png`, fullPage: true, mask: [page.locator('input[type="email"]'), page.locator('input[type="password"]')] });
    }
  }
  await page.goto('https://sub.caowo.baby/');
  await page.getByRole('button', { name: '使用 Passkey', exact: true }).waitFor();
  await screenshots('login');
  await page.getByRole('button', { name: '忘记密码', exact: true }).click();
  await page.getByRole('heading', { name: '恢复账户', exact: true }).waitFor();
  if (!page.url().includes('auth=reset')) throw Error('Recovery deep link absent');
  await page.reload(); await page.getByRole('heading', { name: '恢复账户', exact: true }).waitFor();
  await screenshots('reset');
  await page.getByLabel('邮箱', { exact: true }).fill('mismatch@acceptance.invalid');
  await page.getByLabel('新密码', { exact: true }).fill('local-acceptance-only');
  await page.getByLabel('确认密码', { exact: true }).fill('different-local-acceptance-only');
  await page.getByLabel('邮箱验证码', { exact: true }).fill('not-submitted');
  let recoveryCalls = 0; const count = r => { if (r.url().endsWith('/auth/reset')) recoveryCalls++; }; page.on('request', count);
  await page.getByRole('button', { name: '重设密码', exact: true }).click();
  await page.getByRole('alert').filter({ hasText: '两次输入的密码不一致' }).waitFor();
  if (recoveryCalls) throw Error('Mismatched password reached recovery API'); page.off('request', count);
  await page.screenshot({ path: 'docs/evidence/auth-reset-mismatch-390.png', mask: [page.locator('input')] });
  checks.push('recovery deep link and reload; password confirmation rejects before API; no real email recovery claimed');
  await page.goto('https://sub.caowo.baby/?auth=register');
  await page.getByText('注册暂未开放，请联系管理员创建账户。', { exact: true }).waitFor();
  if (await page.getByLabel('密码', { exact: true }).count()) throw Error('Closed registration form rendered');
  await screenshots('registration-closed');
  checks.push('disabled registration deep link cannot render a registration form; server registration remains disabled');
  await page.goto('https://sub.caowo.baby/'); await page.getByLabel('邮箱', { exact: true }).fill(s.admin_email);
  await page.getByLabel('密码', { exact: true }).fill('incorrect-acceptance-only');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await page.getByRole('alert').filter({ hasText: '邮箱或密码不正确' }).waitFor();
  await page.screenshot({ path: 'docs/evidence/auth-wrong-password-390.png', mask: [page.locator('input')] });
  await page.getByLabel('密码', { exact: true }).fill(s.admin_password);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await page.getByRole('heading', { name: '验证你的身份', exact: true }).waitFor();
  if (await page.getByLabel('密码', { exact: true }).count()) throw Error('Password retained on MFA screen');
  await screenshots('mfa');
  await page.getByLabel('验证码 / 恢复码', { exact: true }).fill('invalid-acceptance-recovery');
  await page.getByRole('button', { name: '验证并登录', exact: true }).click();
  await page.getByRole('alert').filter({ hasText: '二次验证码无效或已使用' }).waitFor();
  // Wait for a fresh TOTP tick, avoiding replay of the previous settings acceptance code.
  await page.waitForTimeout(30100 - Date.now() % 30000);
  await page.getByLabel('验证码 / 恢复码', { exact: true }).fill(otp(s.totp_secret));
  await page.getByRole('button', { name: '验证并登录', exact: true }).click();
  await page.getByRole('heading', { name: '概览', exact: true }).waitFor();
  const version = await page.evaluate(async () => (await (await fetch('/api/v1/admin/overview')).json()).version);
  if (await page.evaluate(async () => (await fetch('/api/v1/me')).status) !== 200) throw Error('MFA login did not establish session');
  await page.evaluate(async () => { await fetch('/api/v1/auth/logout', { method: 'POST' }); });
  checks.push('actual admin wrong-password refusal, correct password challenge, invalid factor refusal, fresh TOTP login and isolated-session logout');
  await page.route('**/api/v1/public', route => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'settings_unavailable' } }) }));
  await page.goto('https://sub.caowo.baby/');
  await page.getByRole('alert').filter({ hasText: '登录方式暂时无法读取' }).waitFor();
  await page.screenshot({ path: 'docs/evidence/auth-policy-error-390.png' });
  await page.unroute('**/api/v1/public'); await page.getByRole('button', { name: '重新读取', exact: true }).click();
  await page.getByRole('button', { name: '使用 Passkey', exact: true }).waitFor();
  checks.push('injected policy 503 gives retry; actual policy restored without fabricated flags');
  if (errors.length) throw Error('Authentication browser runtime errors');
  fs.writeFileSync('docs/evidence/auth-ui.json', JSON.stringify({ version, date: new Date().toISOString(), checks, visual: 'four widths captured; human review pending', external: 'SMTP recovery delivery and physical passkey not claimed' }, null, 2));
  await browser.close(); console.log('Actual domain login, MFA, recovery validation, registration guard and policy retry passed');
})().catch(async e => { if (browser) await browser.close(); console.error(e.message); process.exitCode = 1; });
