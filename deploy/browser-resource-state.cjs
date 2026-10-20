require('./production-guard.cjs');
// Only edits resources created by verify-live-resource-state.py.
const {chromium}=require('playwright');
const fs=require('fs'),path=require('path');
let browser,stage='launch';
(async()=>{
 const root=path.join(process.env.LOCALAPPDATA,'MiGate/operations');
 const s=JSON.parse(fs.readFileSync(path.join(root,'deployment.json'),'utf8'));
 const file=path.join(root,'resource-state-fixture.json'),f=JSON.parse(fs.readFileSync(file,'utf8'));
 if(!['inbounds','outbounds','routing'].includes(f.tab)||!Number.isSafeInteger(f.id)||typeof f.enabled!=='boolean')throw Error('Invalid owned fixture');
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});
 const c=await browser.newContext({viewport:{width:1440,height:1000}});
 await c.addCookies([{name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);
 const p=await c.newPage();await p.goto(`https://sub.caowo.baby/devices/${s.device_id}?tab=${f.tab}`);
 stage='edit';const row=p.locator('article.inventory-item').filter({has:p.locator('small').filter({hasText:new RegExp('资源 #'+f.id+'(?:\\s|$)')})});
 await row.getByRole('button',{name:'编辑',exact:true}).click();const d=p.getByRole('dialog');
 const toggle=d.getByRole('switch',{name:f.tab==='inbounds'?'启用入站':f.tab==='outbounds'?'启用出站':'启用路由',exact:true});
 stage='saved-state';if((await toggle.getAttribute('aria-checked')==='true')!==!f.enabled)throw Error('Saved enabled state not restored');
 stage='toggle';await toggle.click();
 stage='screenshots';for(const width of [1440,1280,768,390]){
  await p.setViewportSize({width,height:1000});await toggle.scrollIntoViewIfNeeded();
  const box=await d.boundingBox();if(!box||box.x<0||box.x+box.width>width+1||await d.evaluate(el=>el.scrollWidth>el.clientWidth+1))throw Error('State editor clipped');
  await p.screenshot({path:`docs/evidence/state-${f.tab}-${f.enabled?'on':'off'}-${width}.png`});
  await d.getByRole('button',{name:'核对配置',exact:true}).scrollIntoViewIfNeeded();
  await p.screenshot({path:`docs/evidence/state-${f.tab}-${f.enabled?'on':'off'}-actions-${width}.png`});
 }
 stage='submit';await d.getByRole('button',{name:'核对配置',exact:true}).click();
 const [r]=await Promise.all([
  p.waitForResponse(r=>/\/devices\/\d+\/tasks$/.test(r.url())&&r.request().method()==='POST'),
  d.getByRole('button',{name:'确认下发任务',exact:true}).click()
 ]);
 if(r.status()!==202)throw Error('Enabled change rejected');f.task_id=(await r.json()).id;fs.writeFileSync(file,JSON.stringify(f));
 await browser.close();console.log('Owned resource state submitted through UI; receipt pending');
})().catch(async(e)=>{console.error('Resource state UI failed at '+stage);fs.writeFileSync('docs/evidence/resource-state-ui-failure.json',JSON.stringify({stage,error_type:e?.name||'Error',time:new Date().toISOString()}));if(browser)await browser.close();process.exitCode=1;});
