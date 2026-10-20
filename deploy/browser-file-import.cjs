require('./production-guard.cjs');
// Browser File API is real; accepted nodes are synthetic, never user grants.
const { chromium }=require('playwright'),fs=require('fs'),path=require('path');
let browser;
(async()=>{
 const s=JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA,'MiGate/operations/deployment.json'),'utf8'));
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});
 const c=await browser.newContext({viewport:{width:1440,height:1000}});
 await c.addCookies([{name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);
 const p=await c.newPage(),ids=[],checks=[],tag='File-'+Date.now();
 async function api(method,endpoint){return p.evaluate(async({method,endpoint})=>{const r=await fetch('/api/v1'+endpoint,{method});if(!r.ok)throw Error('API rejected');return r.json();},{method,endpoint});}
 try{
  await p.goto('https://sub.caowo.baby/subscriptions?kind=nodes');await p.getByRole('button',{name:'导入节点',exact:true}).click();
  const d=p.getByRole('dialog'),input=d.getByLabel('选择订阅文件',{exact:true}),content=d.getByLabel('节点清单内容',{exact:true});
  const before=(await api('GET','/admin/resources/nodes')).total;
  await input.setInputFiles({name:'too-large.txt',mimeType:'text/plain',buffer:Buffer.alloc(2*1024*1024+1,65)});
  await d.getByRole('alert').getByText('请选择一个不超过 2 MiB 的文本订阅文件。',{exact:true}).waitFor();
  if(await content.inputValue()!=='')throw Error('Oversize file retained stale content');
  await input.setInputFiles({name:'binary.txt',mimeType:'text/plain',buffer:Buffer.from([0xff,0xfe,0,0])});
  await d.getByRole('alert').getByText(/无法读取文件/).waitFor();
  checks.push('oversize and non-UTF8 files rejected locally; previous content/preview cleared');
  const text=`﻿# local inventory\n[Proxy]\n${tag}-INI = ss, example.com, 8388, encrypt-method=aes-128-gcm, password="synthetic"`;
  await input.setInputFiles({name:'香港本地节点.conf',mimeType:'text/plain',buffer:Buffer.from(text,'utf8')});
  await d.getByText('文件已读取，请解析预览；尚未写入库存。',{exact:true}).waitFor();
  await d.getByRole('button',{name:'解析预览',exact:true}).click();await d.getByText('准备导入 1 个 · 检测到 0 个重复项',{exact:true}).waitFor();
  // Drop a new JSON file after preview: old result must disappear.
  await p.evaluate(({name,tag})=>{const transfer=new DataTransfer();transfer.items.add(new File([JSON.stringify({outbounds:[{type:'http',tag,server:'example.org',server_port:8080,username:'u',password:'synthetic'}]})],name,{type:'application/json'}));document.querySelector('.import-file-panel').dispatchEvent(new DragEvent('drop',{bubbles:true,cancelable:true,dataTransfer:transfer}));},{name:'拖放节点.json',tag:tag+'-JSON'});
  await d.getByText('拖放节点.json',{exact:true}).waitFor();
  if(await d.getByRole('button',{name:'确认导入 1 个节点',exact:true}).count())throw Error('New file retained old preview');
  await d.getByRole('button',{name:'解析预览',exact:true}).click();await d.getByText('准备导入 1 个 · 检测到 0 个重复项',{exact:true}).waitFor();
  if((await api('GET','/admin/resources/nodes')).total!==before)throw Error('File preview wrote inventory');
  for(const width of [1440,1280,768,390]){
   await p.setViewportSize({width,height:1000});await d.getByRole('button',{name:'选择订阅文件',exact:true}).scrollIntoViewIfNeeded();
   const b=await d.boundingBox();if(!b||b.x<0||b.x+b.width>width+1||await d.evaluate(el=>el.scrollWidth>el.clientWidth+1))throw Error('File panel overflow');
   await p.screenshot({path:`docs/evidence/file-import-${width}.png`,fullPage:false,mask:[p.locator('.sensitive-output')]});
   await d.getByRole('button',{name:'确认导入 1 个节点',exact:true}).scrollIntoViewIfNeeded();
   if(width===390){const summary=d.locator('.import-node-cards');if(!await summary.isVisible()||!(await summary.innerText()).includes('example.org'))throw Error('Mobile node summary missing');}
   await p.screenshot({path:`docs/evidence/file-import-actions-${width}.png`,fullPage:false,mask:[p.locator('.sensitive-output')]});
  }
  const response=p.waitForResponse(r=>r.url().endsWith('/api/v1/admin/subscriptions/import')&&r.request().method()==='POST');
  await d.getByRole('button',{name:'确认导入 1 个节点',exact:true}).click();const r=await response,body=await r.json();if(r.status()!==200||body.ids?.length!==1)throw Error('File commit failed');ids.push(...body.ids);
  await d.getByText('已导入 1 个节点',{exact:true}).waitFor();
  const saved=(await api('GET','/admin/resources/nodes')).items.find(n=>n.id===ids[0]);if(saved?.data.name!==tag+'-JSON')throw Error('Wrong file committed');
  checks.push('native file picker reads UTF8/BOM INI; real drag/drop replaces preview with JSON; file reading/preview do not write; explicit commit creates only selected file node');
  fs.writeFileSync('docs/evidence/file-import-ui.json',JSON.stringify({version:(await api('GET','/admin/overview')).version,date:new Date().toISOString(),checks,visual:'four widths captured, review pending'},null,2));
 }finally{for(const id of ids)await api('DELETE','/admin/resources/nodes/'+id);await browser.close();}
 console.log('Real file picker, drag/drop, size/encoding rejection, preview reset and commit passed');
})().catch(async()=>{if(browser)await browser.close();console.error('File import workflow failed; private request inputs omitted');process.exitCode=1;});
