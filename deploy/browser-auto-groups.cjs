require('./production-guard.cjs');
const {chromium}=require('playwright');const fs=require('fs'),path=require('path');let browser;
(async()=>{
 const s=JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA,'MiGate/operations/deployment.json'),'utf8'));
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});const context=await browser.newContext({viewport:{width:1440,height:1000}});await context.addCookies([{name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);const page=await context.newPage(),owned=[],checks=[];
 async function req(method,url,data){return page.evaluate(async({method,url,data})=>{const r=await fetch('/api/v1'+url,{method,headers:{'Content-Type':'application/json'},body:data===undefined?undefined:JSON.stringify(data)});if(!r.ok)throw Error('Automatic group HTTP '+r.status);return r.json()},{method,url,data})}
 try{
  await page.goto('https://sub.caowo.baby/subscriptions?kind=templates');const nodeIds=[];
  for(const [name,url]of [['HK-auto-one','http://u:fixture@example.org:8080#HK-one'],['Japan-auto-two','socks5://u:fixture@example.org:1080#Japan-two'],['HK-auto-three','http://u:fixture@example.org:8081#HK-three']]){const node=await req('POST','/admin/resources/nodes',{data:{name,url}});owned.push(['nodes',node.id]);nodeIds.push(node.id)}
  for(const [mode,label]of [['region','地区'],['protocol','协议']]){
   await page.getByRole('button',{name:'新增输出模板',exact:true}).click();const dialog=page.getByRole('dialog').first();await dialog.getByLabel('名称',{exact:true}).fill('auto-'+mode+'-'+Date.now());
   await dialog.getByRole('button',{name:label+'分组预设',exact:true}).click();await page.getByRole('dialog').last().getByText('替换策略组与规则',{exact:true}).waitFor();await page.getByRole('dialog').last().getByRole('button',{name:'应用预设',exact:true}).click();
   await dialog.getByText('已启用'+label+'自动分组',{exact:false}).waitFor();if(!await dialog.getByRole('checkbox',{name:'自动生成的策略组',exact:true}).isChecked())throw Error('Preset did not select automatic groups');
   for(const width of [1440,1280,768,390]){await page.setViewportSize({width,height:1000});await dialog.getByRole('button',{name:label+'分组预设',exact:true}).scrollIntoViewIfNeeded();const box=await dialog.boundingBox();if(box.x<0||box.x+box.width>width)throw Error('Auto preset clipped '+width);await page.screenshot({path:`docs/evidence/template-auto-${mode}-${width}.png`})}
   const response=page.waitForResponse(r=>r.url().endsWith('/api/v1/admin/resources/templates')&&r.request().method()==='POST');await dialog.getByRole('button',{name:'保存',exact:true}).click();const r=await response;if(r.status()!==200)throw Error('Automatic preset save failed');const item=await r.json();owned.push(['templates',item.id]);await dialog.waitFor({state:'hidden'});
   const profile=await req('POST','/admin/resources/profiles',{data:{name:'auto-preview-'+mode,node_ids:nodeIds,template_ids:[item.id]}});owned.push(['profiles',profile.id]);
   const preview=await req('POST','/admin/profiles/preview',{id:profile.id,target:'clash'});const expected=mode==='region'?['香港 节点','日本 节点']:['HTTP 节点','SOCKS5 节点'];if(expected.some(v=>!preview.output.includes(v))||preview.output.includes('$auto')||!preview.output.includes('url-test'))throw Error('Actual preview did not expand correct automatic buckets');checks.push('actual '+mode+' preset confirmation, persisted template and profile output; current inventory only');
  }
  fs.writeFileSync('docs/evidence/template-auto-ui.json',JSON.stringify({version:(await req('GET','/admin/overview')).version,date:new Date().toISOString(),checks,limitations:'admin inventory fixture preview; real authorized-client egress tested separately; region is a keyword hint'},null,2));
 }finally{for(const [kind,id]of owned.reverse())await req('DELETE','/admin/resources/'+kind+'/'+id);await browser.close()}
 console.log('Actual region/protocol preset editing, preview and four-width interaction passed');
})().catch(async e=>{if(browser)await browser.close();console.error(e.message);process.exitCode=1});
