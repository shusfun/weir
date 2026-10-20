require('./production-guard.cjs');
const { chromium } = require('playwright');
const fs = require('fs'), path = require('path');
let browser;
(async () => {
  const id = Number(process.argv[2]); if (!Number.isInteger(id) || id <= 0) throw Error('Fixture ID required');
  const s = JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA, 'MiGate/operations/deployment.json'), 'utf8'));
  browser = await chromium.launch({ channel: 'chrome', args: ['--no-proxy-server', '--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443'] });
  const c = await browser.newContext({ viewport: { width:1440,height:1000 } });
  await c.addCookies([{ name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true }]);
  const p = await c.newPage();
  async function api(endpoint) {return p.evaluate(async endpoint => { const r = await fetch('/api/v1' + endpoint); if (!r.ok) throw Error('API failed');return r.json(); }, endpoint);}
  try {
    await p.goto('https://sub.caowo.baby/subscriptions?kind=sources');
    const original = (await api('/admin/resources/sources')).items.find(r=>r.id===id);
    if (!original || original.data.name !== 'Acceptance source fixture') throw Error('Owned source fixture missing');
    const row = p.locator('article.inventory-item').filter({has:p.getByRole('heading',{name:original.data.name,exact:true})});
    await row.getByRole('button',{name:'编辑',exact:true}).click();
    const d=p.getByRole('dialog');
    await d.getByLabel('请求 User-Agent（留空使用默认值）',{exact:true}).fill('MiGate-Acceptance/69');
    await d.getByLabel('服务商网站（可选）',{exact:true}).fill('https://example.com/support');
    await d.getByLabel('订阅源备注',{exact:true}).fill('真实请求头验收 · 含长名称与换行\n不自动授予用户');
    for(const width of [1440,1280,768,390]) {
      await p.setViewportSize({width,height:1000});
      await d.getByLabel('请求 User-Agent（留空使用默认值）',{exact:true}).scrollIntoViewIfNeeded();
      const b=await d.boundingBox();if(!b||b.x<0||b.x+b.width>width+1||await d.evaluate(el=>el.scrollWidth>el.clientWidth+1))throw Error('Source options dialog overflow');
      await p.screenshot({path:`docs/evidence/source-options-${width}.png`,fullPage:false,mask:[p.locator('.sensitive-output')]});
      await d.getByRole('button',{name:'保存',exact:true}).scrollIntoViewIfNeeded();
      await p.screenshot({path:`docs/evidence/source-options-actions-${width}.png`,fullPage:false,mask:[p.locator('.sensitive-output')]});
    }
    const response=p.waitForResponse(r=>r.url().endsWith('/api/v1/admin/resources/sources')&&r.request().method()==='POST');
    await d.getByRole('button',{name:'保存',exact:true}).click();
    if((await response).status()!==200)throw Error('Source form rejected');await d.waitFor({state:'hidden'});
    const saved=(await api('/admin/resources/sources')).items.find(r=>r.id===id).data;
    if(saved.user_agent!=='MiGate-Acceptance/69'||saved.website!=='https://example.com/support'||!saved.notes.includes('\n')||saved.links||saved.last_update)throw Error('Options/cache contract failed');
    await row.getByRole('button',{name:'编辑',exact:true}).click();
    if(await d.getByLabel('请求 User-Agent（留空使用默认值）',{exact:true}).inputValue()!=='MiGate-Acceptance/69')throw Error('Saved options not restored');
    await p.keyboard.press('Escape');
    fs.writeFileSync('docs/evidence/source-options-ui.json',JSON.stringify({version:(await api('/admin/overview')).version,date:new Date().toISOString(),checks:['actual source form edits UA/website/multiline notes','UA change invalidates cache before new refresh','reopen restores saved request options','four width dialog screenshots captured'],visual:'review pending'},null,2));
  } finally {await browser.close();}
  console.log('Actual source request options form/cache invalidation passed');
})().catch(async()=>{if(browser)await browser.close();console.error('Source options workflow failed; request credentials omitted');process.exitCode=1;});
