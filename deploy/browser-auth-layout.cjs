// Local rendering regression only. Stubbed authentication is not a security acceptance.
const { chromium } = require('playwright');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const origin = process.env.MIGATE_LAYOUT_ORIGIN || 'http://127.0.0.1:5179';
assert(['localhost', '127.0.0.1'].includes(new URL(origin).hostname), 'Local rendering only');
const output = path.resolve('docs/evidence/auth-layout-local');
fs.mkdirSync(output, { recursive: true });
(async () => {
  const browser = await chromium.launch({ channel: 'chrome' });
  const measurements = [];
  try {
    const page = await browser.newPage();
    let policyFailure = false;
    await page.route('**/api/v1/**', async route => {
      const endpoint = new URL(route.request().url()).pathname;
      const data = {
        '/api/v1/setup/status': { installed: true },
        '/api/v1/me': { error: { code: 'unauthorized' } },
        '/api/v1/public': { registration_enabled: false, email_verification: false, passkey_enabled: true },
        '/api/v1/auth/login': { mfa_required: true, challenge: 'local-rendering-only' },
      };
      assert(endpoint in data, 'Unexpected request: ' + endpoint);
      await route.fulfill({ status: endpoint.endsWith('/me') ? 401 : policyFailure && endpoint.endsWith('/public') ? 503 : 200,
        contentType: 'application/json', body: JSON.stringify(data[endpoint]) });
    });
    const states = ['login', 'reset', 'mfa', 'policy-error'];
    for (const state of states) {
      policyFailure = state === 'policy-error';
      await page.goto(origin + (state === 'reset' ? '/?auth=reset' : '/'));
      await page.locator('.auth-card').waitFor();
      if (state === 'mfa') {
        await page.getByLabel('邮箱', { exact: true }).fill('layout@example.invalid');
        await page.getByLabel('密码', { exact: true }).fill('local-rendering-only');
        await page.getByRole('button', { name: '登录', exact: true }).click();
        await page.getByRole('heading', { name: '验证你的身份' }).waitFor();
      }
      if (state === 'policy-error') await page.getByRole('button', { name: '重新读取' }).waitFor();
      for (const [width, height] of [[2560,1440],[1920,1080],[1440,1000],[1280,900],[768,1024],[390,844],[390,480]]) {
        await page.setViewportSize({ width, height });
        await page.evaluate(() => document.fonts.ready);
        const box = await page.locator('.auth-container').boundingBox();
        const delta = Math.abs(box.x + box.width / 2 - width / 2);
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth);
        await page.screenshot({ path: path.join(output, `${state}-${width}-${height}.png`), fullPage: true });
        measurements.push({ state, width, height, box, center_offset: delta, overflow });
        assert(delta <= 1, `${state} ${width}: center offset ${delta}px`);
        assert(!overflow, `${state} ${width}: horizontal overflow`);
        assert(box.y >= 0, `${state}: top of form clipped`);
        await page.locator('.auth-footer').scrollIntoViewIfNeeded();
        assert(await page.locator('.auth-footer').isVisible(), 'Footer unreachable');
        await page.evaluate(() => scrollTo(0, 0));
      }
    }
    fs.writeFileSync(path.join(output, 'result.json'), JSON.stringify({ tested_at: new Date().toISOString(), result: 'passed', scope: 'Local real React rendering with stubbed API; no authentication or production acceptance', measurements }, null, 2));
    console.log(`Passed ${measurements.length} authentication layout checks`);
  } finally { await browser.close(); }
})().catch(error => { console.error(error.message); process.exitCode = 1; });
