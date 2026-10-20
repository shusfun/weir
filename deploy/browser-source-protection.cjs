require('./production-guard.cjs');
const {chromium}=require('playwright');
const fs=require('fs'),path=require('path');let browser,stage='launch';
(async()=>{
 const id=Number(process.argv[2]),enabled=process.argv[3]==='on';if(!Number.isSafeInteger(id)||id<=0)throw Error('Owned fixture required');
 const s=JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA,'MiGate/operations/deployment.json'),'utf8'));
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});
 const c=await browser.newContext({viewport:{width:1440,height:1000}});
 await c.addCookies([{name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);
 const p=await c.newPage();await p.goto('https://sub.caowo.baby/subscriptions?kind=sources');
 const row=p.locator('article.inventory-item').filter({has:p.getByRole('heading',{name:'Owned protective cache fixture',exact:true})});
 await row.getByRole('button',{name:'编辑',exact:true}).click();const d=p.getByRole('dialog');
 const toggle=d.getByRole('switch',{name:'保护性节点缓存',exact:true});stage='toggle';
 if((await toggle.getAttribute('aria-checked')==='true')!==enabled)await toggle.click();
 if(enabled)await d.getByLabel('缓存最长保留（60–604800 秒）',{exact:true}).fill('60');
 stage='screenshots';for(const width of [1440,1280,768,390]){
  await p.setViewportSize({width,height:1000});await toggle.scrollIntoViewIfNeeded();
  const box=await d.boundingBox();if(!box||box.x<0||box.x+box.width>width+1||await d.evaluate(el=>el.scrollWidth>el.clientWidth+1))throw Error('Protection editor clipped');
  await p.screenshot({path:`docs/evidence/source-protection-${enabled?'on':'off'}-${width}.png`});
  await d.getByRole('button',{name:'保存',exact:true}).scrollIntoViewIfNeeded();
  await p.screenshot({path:`docs/evidence/source-protection-${enabled?'on':'off'}-actions-${width}.png`});
 }
 stage='save';const [r]=await Promise.all([p.waitForResponse(r=>r.url().endsWith('/admin/resources/sources')&&r.request().method()==='POST'),d.getByRole('button',{name:'保存',exact:true}).click()]);
 if(r.status()!==200)throw Error('Protection settings rejected');await d.waitFor({state:'hidden'});
 await row.getByRole('button',{name:'编辑',exact:true}).click();if((await d.getByRole('switch',{name:'保护性节点缓存',exact:true}).getAttribute('aria-checked')==='true')!==enabled)throw Error('Saved switch not restored');
 await browser.close();console.log('Owned protective cache UI save and reopen passed');
})().catch(async()=>{console.error('Source protection UI failed at '+stage);if(browser)await browser.close();process.exitCode=1;});
