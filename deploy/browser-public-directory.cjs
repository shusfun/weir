require('./production-guard.cjs');
// Real domain publication and anonymous rendering. Only this run's resources are removed.
const { chromium } = require('playwright');
const fs = require('fs'), path = require('path');
let browser;
(async () => {
  const state = JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA, 'MiGate/operations/deployment.json'), 'utf8'));
  browser = await chromium.launch({ channel: 'chrome', args: ['--no-proxy-server', '--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443'] });
  const admin = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  await admin.addCookies([{ name: 'migate_session', value: state.admin_cookie.slice('migate_session='.length), domain: 'sub.caowo.baby', path: '/', secure: true, httpOnly: true }]);
  const page = await admin.newPage(), owned = [], checks = [], errors = [];
  page.on('pageerror', e => errors.push(e.message));
  async function request(method, endpoint, data) {
    return page.evaluate(async ({ method, endpoint, data }) => {
      const r = await fetch('/api/v1' + endpoint, { method, headers: { 'Content-Type': 'application/json' }, body: data === undefined ? undefined : JSON.stringify(data) });
      return { status: r.status, data: await r.json() };
    }, { method, endpoint, data });
  }
  async function update(record, data) {
    const r = await request('POST', '/admin/resources/public-page', { id: record.id, data: { ...record.data, ...data } });
    if (r.status !== 200) throw Error('Public page update failed ' + r.status);
    record.data = r.data.data;
  }
  try {
    await page.goto('https://sub.caowo.baby/subscriptions?kind=public-page');
    const version = (await request('GET', '/admin/overview')).data.version;
    const tag = 'directory-' + Date.now();
    const fixtures = [
      { name: '使用指南-' + tag, slug: tag + '-guide', title: '从这里开始，连接你的世界', description: '了解客户端设置与日常使用。公开内容只作说明，个人订阅请登录账户获取。', enabled: true },
      { name: '长名称-' + tag, slug: tag + '-help', title: '连接与账户常见问题 · 长标题在手机上也能完整阅读', description: '保留每一条信息的清晰层次。\n<img src=x onerror="window.__publicXSS=1">', enabled: true },
      { name: '草稿-' + tag, slug: tag + '-draft', title: 'PRIVATE DRAFT MUST NOT APPEAR', description: 'not published', enabled: false },
    ];
    for (const data of fixtures) {
      const r = await request('POST', '/admin/resources/public-page', { data });
      if (r.status !== 200) throw Error('Fixture creation failed');
      owned.push(r.data);
    }
    await page.getByRole('button', { name: '新增公开页', exact: true }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByLabel('名称', { exact: true }).fill('private operator directory ' + tag);
    await dialog.getByLabel('公开路径（2–64 位小写字母、数字、连字符）', { exact: true }).fill(tag);
    await dialog.getByLabel('公开页标题', { exact: true }).fill('保持连接，让探索更简单。');
    await dialog.getByLabel('公开介绍（纯文本）', { exact: true }).fill('欢迎来到连接指南。这里汇集已发布的使用说明与服务公告，帮助你快速找到所需信息。');
    await dialog.getByLabel('公告（纯文本）', { exact: true }).fill('服务说明\n个人订阅与可用节点请在登录后的用户中心查看。');
    await dialog.getByLabel('公告标题',{exact:true}).fill('服务维护提醒 · 请保管个人订阅');
    await dialog.getByRole('combobox',{name:'公告类型',exact:true}).click();await page.getByRole('option',{name:'提醒',exact:true}).click();
    await dialog.getByRole('switch',{name:'允许访客关闭当前公告',exact:true}).click();
    await dialog.locator('.announcement-preview').getByRole('heading',{name:'服务维护提醒 · 请保管个人订阅',exact:true}).waitFor();
    await dialog.getByRole('combobox', { name: '页面主题', exact: true }).click();
    await page.getByRole('option', { name: '海蓝', exact: true }).click();
    await dialog.getByRole('switch', { name: '允许复制页面地址与二维码', exact: true }).click();
    for (const item of fixtures) await dialog.getByRole('checkbox', { name: item.name, exact: true }).check();
    await dialog.getByRole('button', { name: '上移 ' + fixtures[1].name, exact: true }).click();
    await dialog.getByRole('switch', { name: '发布公开页', exact: true }).click();
    for (const width of [1440, 1280, 768, 390]) {
      await page.setViewportSize({ width, height: 1000 });
      const box = await dialog.boundingBox();
      if (!box || box.x < 0 || box.x + box.width > width + 1) throw Error('Public editor clipped at ' + width);
      await page.screenshot({ path: `docs/evidence/public-directory-editor-${width}.png`, fullPage: true });
    }
    const saved = page.waitForResponse(r => r.url().endsWith('/api/v1/admin/resources/public-page') && r.request().method() === 'POST');
    await dialog.getByRole('button', { name: '保存', exact: true }).click();
    const response = await saved;
    if (response.status() !== 200) throw Error('Directory UI save failed');
    const parent = await response.json(); owned.push(parent);
    await dialog.waitFor({ state: 'hidden' });
    checks.push('UIAble editor: business fields, theme selector, ordered published/draft page selection, explicit publication; editor four viewport bounds');
    const anonymous = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: ['clipboard-read', 'clipboard-write'] });
    const viewer = await anonymous.newPage(), apiPaths = [];
    viewer.on('pageerror', e => errors.push(e.message));
    viewer.on('request', r => { if (r.url().includes('/api/v1/')) apiPaths.push(new URL(r.url()).pathname); });
    await viewer.goto('https://sub.caowo.baby/p/' + tag);
    await viewer.getByRole('heading', { name: '保持连接，让探索更简单。', exact: true }).waitFor();
    const actual = await viewer.evaluate(async slug => { const r = await fetch('/api/v1/pages/' + slug, { credentials: 'omit' }); return { status: r.status, cache: r.headers.get('cache-control'), data: await r.json() }; }, tag);
    if (actual.status !== 200 || actual.cache !== 'no-store' || Object.keys(actual.data).sort().join(',') !== 'cards,description,notice,notice_dismissible,notice_revision,notice_title,notice_type,show_login,show_share,theme,title') throw Error('Public response allowlist mismatch');
    if (actual.data.cards.length !== 2 || actual.data.cards[0].slug !== fixtures[1].slug || actual.data.cards.some(c => Object.keys(c).sort().join(',') !== 'description,slug,title')) throw Error('Directory order/private fields mismatch');
    if (await viewer.getByText(fixtures[2].title, { exact: true }).count() || await viewer.locator('img[src=x]').count() || await viewer.evaluate(() => !!window.__publicXSS)) throw Error('Draft or executable markup leaked');
    if (await viewer.getByRole('button', { name: '登录', exact: true }).count()) throw Error('Disabled login entry visible');
    if ((await anonymous.cookies()).length || apiPaths.some(p => p.includes('/admin/') || p.includes('/subscription'))) throw Error('Anonymous view accessed private API/session');
    async function checkNoticeColors(){const colors=await viewer.locator('.published-announcement').evaluate(el=>({background:getComputedStyle(el).backgroundColor,title:getComputedStyle(el.querySelector('.published-section-title')).color}));if(colors.background!=='rgb(255, 250, 240)'||colors.title!=='rgb(152, 96, 11)')throw Error('Theme overrides warning semantic colors');}
    await checkNoticeColors();
    for (const width of [1440, 1280, 768, 390]) {
      await viewer.setViewportSize({ width, height: 1000 });
      if (await viewer.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw Error('Public directory overflow ' + width);
      await viewer.screenshot({ path: `docs/evidence/public-directory-ocean-${width}.png`, fullPage: true });
    }
    const notice=viewer.locator('.published-announcement');
    if(actual.data.notice_type!=='warning'||!actual.data.notice_dismissible||!actual.data.notice_revision)throw Error('Announcement settings not projected');
    await notice.getByRole('button',{name:'关闭公告',exact:true}).focus();await viewer.keyboard.press('Enter');await notice.waitFor({state:'hidden'});
    await viewer.reload();await viewer.getByRole('heading',{name:'探索目录',exact:true}).waitFor();if(await notice.count())throw Error('Dismissal not retained');
    await update(parent,{description:'已更新公开介绍；公告关闭偏好保持。'});await viewer.reload();await viewer.getByRole('heading',{name:'探索目录',exact:true}).waitFor();if(await notice.count())throw Error('Unrelated update reset dismissal');
    await update(parent,{notice:'更新后的服务公告：新内容应当重新提示。\n<script>window.__noticeXSS=1</script>'});await viewer.reload();await notice.waitFor();
    if(await viewer.evaluate(()=>!!window.__noticeXSS)||await notice.locator('script').count())throw Error('Executable announcement markup');
    await viewer.evaluate(()=>{Storage.prototype.setItem=function(){throw new Error('owned storage failure')};});await notice.getByRole('button',{name:'关闭公告',exact:true}).click();await notice.getByRole('alert').filter({hasText:'浏览器未保存偏好'}).waitFor();await viewer.screenshot({path:'docs/evidence/public-announcement-storage-failure-390.png',fullPage:true});
    await update(parent,{notice_enabled:false});await viewer.reload();await viewer.getByRole('heading',{name:'探索目录',exact:true}).waitFor();if(await notice.count())throw Error('Disabled announcement visible');
    const disabled=await viewer.evaluate(async slug=>(await fetch('/api/v1/pages/'+slug)).json(),tag);if(disabled.notice||disabled.notice_title)throw Error('Disabled draft content leaked');
    await update(parent,{notice_enabled:true});await viewer.reload();await notice.waitFor();
    checks.push('announcement UI title/type/preview; keyboard dismissal persists; unrelated update retains preference; content update redisplays; storage failure visible; disabled draft fields hidden; literal script never executes');
    await viewer.getByRole('button', { name: '复制页面地址', exact: true }).click();
    await viewer.getByRole('status').filter({ hasText: '页面地址已复制' }).waitFor();
    if (await viewer.evaluate(() => navigator.clipboard.readText()) !== 'https://sub.caowo.baby/p/' + tag) throw Error('Copied a private or wrong URL');
    await viewer.getByRole('button', { name: '页面二维码', exact: true }).click();
    const qr = viewer.getByRole('dialog');
    await qr.getByRole('heading', { name: '页面二维码', exact: true }).waitFor();
    const box = await qr.boundingBox(); if (box.x < 0 || box.x + box.width > 391) throw Error('QR dialog clipped');
    await viewer.screenshot({ path: 'docs/evidence/public-directory-qr-390.png', fullPage: true });
    await viewer.keyboard.press('Escape');
    checks.push('anonymous exact allowlists, no private API/session, draft hidden, order preserved, XSS text, long names; copy public URL and mobile QR');
    for (const theme of ['forest', 'violet']) {
      await update(parent, { theme }); await viewer.reload();
      await viewer.locator(`main[data-theme=${theme}]`).waitFor();await checkNoticeColors();
      for (const width of [1440, 390]) { await viewer.setViewportSize({ width, height: 1000 }); await viewer.screenshot({ path: `docs/evidence/public-directory-${theme}-${width}.png`, fullPage: true }); }
    }
    await viewer.route('**/api/v1/pages/' + tag, r => r.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'page_unavailable' } }) }));
    await viewer.reload(); await viewer.getByRole('alert').filter({ hasText: '页面服务暂时不可用' }).waitFor();
    if (await viewer.locator('.published-card-link').count()) throw Error('Stale directory shown on error');
    await viewer.screenshot({ path: 'docs/evidence/public-directory-error-390.png', fullPage: true });
    await viewer.unroute('**/api/v1/pages/' + tag); await viewer.getByRole('button', { name: '重新加载', exact: true }).click();
    await viewer.getByRole('heading', { name: '探索目录', exact: true }).waitFor();
    await update(owned[0], { enabled: false }); await viewer.reload();
    await viewer.getByRole('heading', { name: '探索目录', exact: true }).waitFor();
    if (await viewer.locator('.published-card-link').count() !== 1) throw Error('Withdrawn card remained public');
    await viewer.locator('.published-card-link').click();
    await viewer.getByRole('heading', { name: fixtures[1].title, exact: true }).waitFor();
    await update(parent, { enabled: false }); await viewer.goto('https://sub.caowo.baby/p/' + tag);
    await viewer.getByRole('alert').filter({ hasText: '页面不存在或尚未发布' }).waitFor();
    checks.push('three real configured themes, injected 503/retry, child withdrawal removes card, card navigation, parent withdrawal returns 404');
    if (errors.length) throw Error('Public directory runtime errors');
    fs.writeFileSync('docs/evidence/public-directory-ui.json', JSON.stringify({ date: new Date().toISOString(), version, checks, visual: 'screenshots captured; human review pending', limitations: 'Published text-page directory only; no anonymous subscription export, private inventory, guestbook or custom executable renderer' }, null, 2));
    console.log('Real public directory publish, theme, order, anonymous redaction, copy/QR, XSS, withdrawal and error recovery passed');
  } finally {
    for (const record of [...owned].reverse()) { const r = await request('DELETE', '/admin/resources/public-page/' + record.id); if (r.status !== 200) throw Error('Owned page cleanup failed'); }
    await browser.close();
  }
})().catch(async e => { if (browser) await browser.close(); console.error(e.message); process.exitCode = 1; });
