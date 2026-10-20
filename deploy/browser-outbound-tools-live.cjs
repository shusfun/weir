require('./production-guard.cjs');
const {chromium}=require('playwright'),fs=require('node:fs'),path=require('node:path');
const f=require('./browser-fixture.cjs').fixture();
const record=JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA,'MiGate/operations/production-pending.json'),'utf8'));
if(record.fixture_prefix!==process.env.MIGATE_FIXTURE_PREFIX || record.cleanup.handler!=='outbound-pool') throw Error('Owned pool scope required');
const prefix=record.fixture_prefix,device=record.cleanup.device_id;
let browser,stage='start';
(async()=>{
 browser=await chromium.launch({channel:'chrome',args:f.args});
 const context=await browser.newContext({viewport:{width:1440,height:1000}});
 context.setDefaultTimeout(20000);
 await context.addCookies([{name:'migate_session',value:f.s.admin_cookie.slice('migate_session='.length),...f.cookie}]);
 const page=await context.newPage(),checks=[];
 async function req(url){return page.evaluate(async url=>{const r=await fetch('/api/v1'+url);if(!r.ok)throw Error('Read HTTP '+r.status);return r.json()},url)}
 async function shots(name,locator){
  for(const width of [1440,1280,768,390]){
   await page.setViewportSize({width,height:1000});await locator.scrollIntoViewIfNeeded();
   const box=await locator.boundingBox();
   if(!box||box.x<0||box.x+box.width>width+1||await locator.evaluate(e=>e.scrollWidth>e.clientWidth+1))throw Error('Tool layout clipped');
   await locator.screenshot({path:`docs/evidence/live-pool-tools-${name}-${width}.png`});
   const close=locator.getByRole('button',{name:/^(关闭|取消)$/,exact:true});
   if(await close.count()){
    await close.last().scrollIntoViewIfNeeded();const b=await close.last().boundingBox(),d=await locator.boundingBox();
    if(!b||b.y<d.y||b.y+b.height>d.y+d.height+1)throw Error('Tool footer clipped');
    await locator.screenshot({path:`docs/evidence/live-pool-tools-${name}-actions-${width}.png`});
   }
  }
 }
 async function waitTask(operation,previous){
  const deadline=Date.now()+95000;
  while(Date.now()<deadline){
   const items=(await req(`/admin/tasks?device_id=${device}&operation=${operation}&page_size=200`)).items;
   const task=items.find(t=>!previous.has(t.id));
   if(task?.status==='applied')return task;
   if(task&&['failed','expired'].includes(task.status))throw Error('Real tool task failed');
   await new Promise(r=>setTimeout(r,750));
  }
  throw Error('Tool task receipt timeout');
 }
 async function oldTasks(operation){return new Set((await req(`/admin/tasks?device_id=${device}&operation=${operation}&page_size=200`)).items.map(t=>t.id))}
 await page.goto(f.origin+`/devices/${device}?tab=outbounds`);
 stage='manual order';
 await page.getByRole('button',{name:'手动出站排序',exact:true}).click();let dialog=page.getByRole('dialog');
 await dialog.getByRole('button',{name:'下移 '+prefix+'TCP fixture 0',exact:true}).click();
 await shots('manual-order',dialog);await dialog.getByRole('button',{name:'取消',exact:true}).click();
 let previous=await oldTasks('outbound.reorder');
 await page.getByRole('button',{name:'手动出站排序',exact:true}).click();dialog=page.getByRole('dialog');
 await dialog.getByRole('button',{name:'下移 '+prefix+'TCP fixture 0',exact:true}).click();
 await dialog.getByRole('button',{name:'确认提交排序任务',exact:true}).click();await dialog.waitFor({state:'hidden'});
 await waitTask('outbound.reorder',previous);await page.reload();
 const inventory=(await req(`/admin/resources/device-outbound?owner_id=${device}&page_size=500`)).items;
 if(inventory.find(r=>r.data.remark===prefix+'TCP fixture 0')?.data.sort!==20)throw Error('Live UI manual order not persisted');
 stage='TCP selection';previous=await oldTasks('outbound.probe');
 await page.getByRole('button',{name:'TCP 连通性探测',exact:true}).click();dialog=page.getByRole('dialog');
 await shots('probe-selection',dialog);await dialog.getByRole('button',{name:'开始 TCP 探测',exact:true}).click();await dialog.waitFor({state:'hidden'});
 const task=await waitTask('outbound.probe',previous);
 if(task.result?.source!=='gate_tcp_connect'||task.result.items.length!==2)throw Error('Real TCP sample missing');
 await page.getByText('TCP 探测 #'+task.id,{exact:true}).waitFor();
 await page.getByText(/采样 .*超过 5 分钟/).waitFor();
 await shots('probe-result',page.locator('main'));
 checks.push('Domain manual order move/cancel/submit/real Agent acknowledgement/reload; selected two public targets and actual TCP observation, no proxy usability assertion');
 stage='registered public catalog';
 const candidate=record.cleanup.catalog_candidates[0];
 if(!candidate)throw Error('Fixed external catalog unavailable; real acceptance not passed');
 const catalog=await req(`/admin/proxy-catalog/${candidate.protocol}?page=1`),item=catalog.items.find(p=>p.key===candidate.key);
 if(!item)throw Error('Registered catalog snapshot expired; stop without adopting another candidate');
 await page.getByRole('button',{name:'公共代理目录',exact:true}).click();dialog=page.getByRole('dialog');
 const label=candidate.protocol==='socks'?'SOCKS5':candidate.protocol.toUpperCase();
 await dialog.getByRole('combobox',{name:'公共代理类型',exact:true}).click();await page.getByRole('option',{name:label,exact:true}).click();
 await dialog.getByRole('textbox',{name:'搜索公共代理',exact:true}).fill(item.address);
 const row=dialog.locator('.inventory-item').filter({hasText:`${item.address}:${item.port}`}).first();await row.waitFor();
 await shots('catalog',dialog);
 previous=await oldTasks('outbound.catalog.probe');await row.getByRole('button',{name:'TCP 探测',exact:true}).click();
 await waitTask('outbound.catalog.probe',previous);
 await row.getByText(/TCP 可达|TCP 未连接|未知或安全限制/).waitFor();
 await row.getByRole('button',{name:'导入出站',exact:true}).click();await shots('catalog-confirm',dialog);
 await dialog.getByRole('button',{name:'返回目录',exact:true}).click();
 previous=await oldTasks('outbound.upsert');await row.getByRole('button',{name:'导入出站',exact:true}).click();
 const submitted=page.waitForResponse(r=>r.url().endsWith(`/devices/${device}/proxy-catalog/import`)&&r.request().method()==='POST');
 await dialog.getByRole('button',{name:'确认导入此代理',exact:true}).click();
 const response=await submitted;
 if(response.status()!==202){const body=await response.json();throw Error('Catalog import HTTP '+response.status()+' '+String(body.error?.code||'unknown'));}
 await waitTask('outbound.upsert',previous);
 await dialog.getByText(/导入任务/).waitFor();await dialog.getByRole('button',{name:'关闭',exact:true}).click();
 await page.reload();
 if(!(await req(`/admin/resources/device-outbound?owner_id=${device}&page_size=500`)).items.some(r=>r.data.tag===candidate.tag))throw Error('Catalog UI import missing');
 checks.push('Actual fixed catalog fetch/type/search; pre-registered candidate real TCP receipt; import confirmation cancel/submit and actual materialization; no user authorization or route created');
 const version=(await req('/admin/overview')).version;
 fs.writeFileSync('docs/evidence/live-outbound-tools-browser.json',JSON.stringify({date:new Date().toISOString(),version,result:'passed',checks,visual:'manual screenshot review pending'},null,2));
 await browser.close();browser=null;
})().catch(async e=>{
 fs.writeFileSync('docs/evidence/live-pool-tools-browser-failure.json',JSON.stringify({date:new Date().toISOString(),stage,detail:String(e.message).split('\n')[0]},null,2));
 console.error(stage+': '+String(e.message).split('\n')[0]);if(browser)await browser.close();process.exitCode=1;
});
