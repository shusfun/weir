require('./production-guard.cjs');
const {chromium}=require('playwright');
const fs=require('fs'),path=require('path');
let browser,stage='launch';
(async()=>{
 const s=JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA,'MiGate/operations/deployment.json'),'utf8'));
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});
 const c=await browser.newContext({viewport:{width:1440,height:1100}});
 await c.addCookies([{name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);
 const p=await c.newPage();const errors=[];p.on('pageerror',()=>errors.push('runtime error'));
 await p.goto(`https://sub.caowo.baby/devices/${s.device_id}?tab=cores`);
 const ids=[];
 for(const core of ['xray','sing-box']){
  stage='submit '+core;await p.getByRole('button',{name:'只读诊断 '+core,exact:true}).click();const d=p.getByRole('dialog');
  if(!(await d.innerText()).includes('只读检查'))throw Error('Missing read-only explanation');
  const response=p.waitForResponse(r=>/\/devices\/\d+\/tasks$/.test(r.url())&&r.request().method()==='POST');
  await d.getByRole('button',{name:'确认执行',exact:true}).click();const r=await response;if(r.status()!==202)throw Error('Inspection rejected');const task=await r.json();ids.push(task.id);
  stage='receipt '+core;await p.getByText(`${core} · 诊断 #${task.id}`,{exact:true}).waitFor({timeout:90000});
 }
 stage='preview';const section=p.getByRole('region',{name:'只读内核诊断'});
 if((await section.innerText()).includes('状态未知'))throw Error('Unexpected unknown real config');
 await section.getByText('配置 SHA-256',{exact:true}).first().click();
 for(const width of [1440,1280,768,390]){
  stage='visual '+width;await p.setViewportSize({width,height:1100});await section.scrollIntoViewIfNeeded();
  if(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('Diagnostics overflow');
  await p.screenshot({path:`docs/evidence/core-diagnostics-${width}.png`,fullPage:true});
  await section.screenshot({path:`docs/evidence/core-diagnostics-detail-${width}.png`});
 }
 if(errors.length)throw Error('Runtime errors');await browser.close();console.log(JSON.stringify({task_ids:ids}));
})().catch(async()=>{if(browser)await browser.close();console.error('Core diagnostics browser failed at '+stage+'; private context omitted');process.exitCode=1;});
