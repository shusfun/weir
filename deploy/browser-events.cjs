require('./production-guard.cjs');
const {chromium}=require('playwright');const fs=require('fs'),path=require('path');let browser;
(async()=>{
 const s=JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA,'MiGate/operations/deployment.json'),'utf8'));
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});
 const context=await browser.newContext({viewport:{width:1440,height:1000}});await context.addCookies([{name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);
 const page=await context.newPage(),checks=[],errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto('https://sub.caowo.baby/events');await page.getByRole('heading',{name:'安全观测与复核',exact:true}).waitFor();await page.getByRole('button',{name:/^查看事件 #/}).first().waitFor();
 const actual=await page.evaluate(async()=>(await(await fetch('/api/v1/admin/events?page=1&page_size=200')).json()));
 if(!actual.items.length)throw Error('No real observations for event acceptance');
 const sample=actual.items.find(e=>e.kind==='security-event');if(!sample)throw Error('No real rule observation');
 await page.getByLabel('搜索安全事件').fill(sample.data.category);await page.getByRole('button',{name:/^查看事件 #/}).first().waitFor();
 await page.getByLabel('筛选事件来源').click();await page.getByRole('option',{name:'基础规则',exact:true}).click();await page.getByRole('button',{name:/^查看事件 #/}).first().waitFor();
 await page.getByRole('button',{name:/^查看事件 #/}).first().click();await page.getByRole('dialog').getByText('中心规则（基于实际心跳 / 计量）',{exact:true}).waitFor();await page.keyboard.press('Escape');
 checks.push('actual center observations searched by category, filtered by source and inspected with timestamp/source/validity');
 await page.getByLabel('搜索安全事件').fill('');
 await page.getByRole('button',{name:/^查看事件 #/}).first().waitFor();
 for(const width of [1440,1280,768,390]){await page.setViewportSize({width,height:1000});if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Events horizontal overflow '+width);await page.screenshot({path:`docs/evidence/events-live-${width}.png`,fullPage:true})}
 const stale=actual.items.find(e=>e.validity==='stale');
 if(stale){await page.getByLabel('搜索安全事件').fill(stale.data.category);await page.getByRole('button',{name:'查看事件 #'+stale.id,exact:true}).click();await page.getByRole('dialog').getByText('已过期，不能据此判断正常',{exact:true}).waitFor();await page.getByRole('dialog').getByText('未知',{exact:true}).first().waitFor();await page.screenshot({path:'docs/evidence/events-stale-390.png'});await page.keyboard.press('Escape');checks.push('actual stale record detail marks current observation unknown and retains original recorded state separately')}
 await page.getByLabel('搜索安全事件').fill('no-event-'+Date.now());await page.getByText('没有符合条件的安全事件',{exact:true}).waitFor();
 await page.route('**/api/v1/admin/events?**',route=>route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:{code:'observation_unavailable'}})}));await page.getByRole('button',{name:'刷新事件',exact:true}).click();await page.getByText('暂时无法加载',{exact:true}).waitFor();await page.unroute('**/api/v1/admin/events?**');await page.getByLabel('搜索安全事件').fill('');await page.getByRole('button',{name:/^查看事件 #/}).first().waitFor();
 checks.push('real empty search; browser-injected API error and actual backend recovery, not a simulated real-service acceptance');
 if(errors.length)throw Error('Browser runtime error');
 const version=await page.evaluate(async()=>(await(await fetch('/api/v1/admin/overview')).json()).version);fs.writeFileSync('docs/evidence/events-live.json',JSON.stringify({version,date:new Date().toISOString(),checks,external:'Laya actual service remains unconfigured; no classifier acceptance claimed'},null,2));await browser.close();console.log('Actual event source, search, details, unknown stale state, empty/error recovery and four-width UI passed');
})().catch(async e=>{if(browser)await browser.close();console.error(e.message);process.exitCode=1});
