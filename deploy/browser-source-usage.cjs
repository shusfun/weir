require('./production-guard.cjs');
const {chromium}=require('playwright');const fs=require('fs'),path=require('path');let browser,stage='launch';
(async()=>{
 const mode=process.argv[2];if(!['exclude','include','panel','unknown'].includes(mode))throw Error('Mode required');
 const s=JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA,'MiGate/operations/deployment.json'),'utf8'));
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});const c=await browser.newContext({viewport:{width:1440,height:1000}});
 await c.addCookies([{name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);
 const p=await c.newPage();await p.goto('https://sub.caowo.baby/subscriptions?kind=sources');
 if(['exclude','include'].includes(mode)){
  stage='edit';const row=p.locator('article.inventory-item').filter({has:p.getByRole('heading',{name:'Owned usage source B',exact:true})});await row.getByRole('button',{name:'编辑',exact:true}).click();const d=p.getByRole('dialog'),toggle=d.getByRole('switch',{name:'合并时不计算此订阅源流量',exact:true});
  if((await toggle.getAttribute('aria-checked')==='true')!==(mode==='exclude'))await toggle.click();
  for(const width of [1440,1280,768,390]){await p.setViewportSize({width,height:1000});await toggle.scrollIntoViewIfNeeded();const b=await d.boundingBox();if(!b||b.x<0||b.x+b.width>width+1||await d.evaluate(el=>el.scrollWidth>el.clientWidth+1))throw Error('Usage editor clipped');await p.screenshot({path:`docs/evidence/source-usage-${mode}-${width}.png`});}
  stage='save';const [r]=await Promise.all([p.waitForResponse(r=>r.url().endsWith('/admin/resources/sources')&&r.request().method()==='POST'),d.getByRole('button',{name:'保存',exact:true}).click()]);if(r.status()!==200)throw Error('Usage setting rejected');await d.waitFor({state:'hidden'});
  await row.getByRole('button',{name:'编辑',exact:true}).click();if((await d.getByRole('switch',{name:'合并时不计算此订阅源流量',exact:true}).getAttribute('aria-checked')==='true')!==(mode==='exclude'))throw Error('Saved exclusion lost');await p.keyboard.press('Escape');
 }
 stage='panel';const panel=p.locator('[aria-label="上游用量合计"]');await panel.getByRole('button',{name:'刷新用量',exact:true}).click();await panel.getByText(/查询时间/).waitFor();
 if(mode==='panel'){
  await p.getByRole('tab',{name:'订阅组',exact:true}).click();
  const group=p.locator('article.inventory-item').filter({has:p.getByRole('heading',{name:'Owned usage exact group',exact:true})});await group.getByRole('button',{name:'预览',exact:true}).click();
  const output=p.getByLabel('库存预览',{exact:true});await output.waitFor();if(!(await output.inputValue()).includes('18446744073709551614'))throw Error('Profile usage missing from actual preview');await p.keyboard.press('Escape');await p.getByRole('tab',{name:'订阅源',exact:true}).click();
 }
 if(mode==='unknown'&&!await panel.getByText('未知',{exact:true}).count())throw Error('Missing counters represented as zero');
 for(const width of [1440,1280,768,390]){await p.setViewportSize({width,height:1000});await panel.scrollIntoViewIfNeeded();if(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Usage panel overflow');await p.screenshot({path:`docs/evidence/source-usage-${mode}-panel-${width}.png`});}
 await browser.close();console.log('Actual source usage UI '+mode+' passed');
})().catch(async()=>{console.error('Source usage UI failed at '+stage);if(browser)await browser.close();process.exitCode=1;});
