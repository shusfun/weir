require('./production-guard.cjs');
const {chromium}=require('playwright');const fs=require('fs'),path=require('path');
let browser,stage='launch';
(async()=>{
 const root=path.join(process.env.LOCALAPPDATA,'MiGate/operations'),privateFile=path.join(root,'certificate-binding-fixture.json');
 const s=JSON.parse(fs.readFileSync(path.join(root,'deployment.json'),'utf8')),f=JSON.parse(fs.readFileSync(privateFile,'utf8'));
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});
 const c=await browser.newContext({viewport:{width:1440,height:1100}});await c.addCookies([{name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);
 const p=await c.newPage();const errors=[];p.on('pageerror',()=>errors.push('runtime error'));
 await p.goto(`https://sub.caowo.baby/devices/${s.device_id}?tab=certificates`);
 stage='open';await p.getByRole('button',{name:'批量绑定证书',exact:true}).click();const d=p.getByRole('dialog');
 stage='choose certificate';await d.getByRole('combobox',{name:'目标证书',exact:true}).click();await p.getByRole('option').filter({hasText:f.cert_name}).click();
 await d.getByLabel('搜索 TLS 入站',{exact:true}).fill(f.prefix);
 for(const name of f.inbound_names)await d.getByRole('checkbox',{name:'选择入站 '+name,exact:true}).check();
 stage='review';await d.getByRole('button',{name:'核对绑定',exact:true}).click();if(!(await d.innerText()).includes('已选 2 个'))throw Error('Wrong binding selection');
 for(const width of [1440,1280,768,390]){await p.setViewportSize({width,height:1100});const box=await d.boundingBox();if(box.x<0||box.x+box.width>width+1)throw Error('Binding dialog clipped');await p.screenshot({path:`docs/evidence/certificate-binding-${width}.png`});}
 stage='submit';const response=p.waitForResponse(r=>/\/devices\/\d+\/tasks$/.test(r.url())&&r.request().method()==='POST');await d.getByRole('button',{name:'确认下发绑定任务',exact:true}).click();const r=await response;if(r.status()!==202)throw Error('Batch rejected');f.task_id=(await r.json()).id;fs.writeFileSync(privateFile,JSON.stringify(f));
 if(errors.length)throw Error('Runtime errors');await browser.close();console.log('Actual two-inbound binding form submitted; receipt verified separately');
})().catch(async()=>{if(browser)await browser.close();console.error('Certificate binding browser failed at '+stage+'; private fixture omitted');process.exitCode=1;});
