require('./production-guard.cjs');
// Actual source/node/profile form acceptance. Fresh synthetic inventory only.
const {chromium}=require('playwright');
const fs=require('node:fs');const path=require('node:path');
let browser;
(async()=>{
 const s=JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA,'MiGate/operations/deployment.json'),'utf8'));
 browser=await chromium.launch({channel:'chrome',headless:true,args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});
 const context=await browser.newContext({viewport:{width:1280,height:900}});
 await context.addCookies([{name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',httpOnly:true,secure:true,sameSite:'Strict'}]);
 const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
 const created=[];const name='form-acceptance-'+Date.now();
 try{
  await page.goto('https://sub.caowo.baby');await page.getByRole('button',{name:'订阅管理',exact:true}).click();
  await page.getByLabel('订阅源地址',{exact:true}).waitFor();
  if(await page.getByLabel('sources JSON').count())throw Error('source form requires JSON');
  await page.getByRole('button',{name:'节点',exact:true}).click();
  await page.getByLabel('名称',{exact:true}).fill(name);
  await page.getByLabel('节点链接',{exact:true}).fill('ss://YWVzLTEyOC1nY206cGFzcw@example.com:8388#HK');
  const response=page.waitForResponse(r=>r.url().endsWith('/admin/resources/nodes')&&r.request().method()==='POST');
  await page.getByRole('button',{name:'保存',exact:true}).click();const saved=await (await response).json();if(!saved.id)throw Error('node not saved');created.push(['nodes',saved.id]);
  await page.getByRole('button',{name:'订阅组',exact:true}).click();
  await page.getByLabel('名称',{exact:true}).fill(name+'-group');
  await page.getByLabel('独立节点（按顺序填写 ID，逗号分隔）').fill(String(saved.id));
  await page.getByRole('button',{name:'高级 JSON',exact:true}).click();
  const data=JSON.parse(await page.getByLabel('profiles JSON').inputValue());if(!Array.isArray(data.node_ids)||data.node_ids[0]!==saved.id)throw Error('form advanced switch loses reference type');
  await page.getByRole('button',{name:'返回表单',exact:true}).click();
  const profileResponse=page.waitForResponse(r=>r.url().endsWith('/admin/resources/profiles')&&r.request().method()==='POST');
  await page.getByRole('button',{name:'保存',exact:true}).click();const profile=await (await profileResponse).json();if(!profile.id)throw Error('profile not saved');created.push(['profiles',profile.id]);
  const preview=await page.evaluate(async id=>{const r=await fetch('/api/v1/admin/profiles/preview',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id,target:'links'})});return {status:r.status,body:await r.json()}},profile.id);
  if(preview.status!==200 || !preview.body.output.includes('example.com:8388'))throw Error('form profile not effective');
  await page.getByRole('button',{name:'模板',exact:true}).click();
  await page.getByLabel('名称',{exact:true}).fill(name+'-template');
  await page.getByLabel('输出格式',{exact:true}).click();await page.getByRole('option',{name:'singbox',exact:true}).click();
  await page.getByLabel('DNS 服务器（逗号分隔；IP、https:// 或 tls://）',{exact:true}).fill('1.1.1.1');
  const templateResponse=page.waitForResponse(r=>r.url().endsWith('/api/v1/admin/resources/templates') && r.request().method()==='POST');
  await page.getByRole('button',{name:'保存',exact:true}).click();const savedTemplate=await templateResponse;
  if(savedTemplate.status()!==200)throw Error('template form save failed');
  const template=await savedTemplate.json();created.push(['templates',template.id]);
  const applied=await page.evaluate(async({profile,template})=>{
    const update=await fetch('/api/v1/admin/resources/profiles',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id:profile.id,data:{...profile.data,template_ids:[template.id]}})});
    if(update.status!==200)return {status:update.status};
    const r=await fetch('/api/v1/admin/profiles/preview',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({id:profile.id,target:'singbox'})});return {status:r.status,body:await r.json()};
  },{profile,template});
  if(applied.status!==200 || JSON.parse(applied.body.output).dns.servers[0].server!=='1.1.1.1')throw Error('template form not applied to actual preview');
  await page.getByRole('button',{name:'节点',exact:true}).click();
  await page.getByLabel('节点清单内容',{exact:true}).fill('proxies:\n - {name: import-acceptance, type: http, server: example.org, port: 8080, username: user, password: synthetic}');
  const importResponse=page.waitForResponse(r=>r.url().endsWith('/api/v1/admin/subscriptions/import') && r.request().method()==='POST');
  await page.getByRole('button',{name:'导入节点',exact:true}).click();const imported=await importResponse;
  if(imported.status()!==200)throw Error('structured inventory form import failed');
  const importedData=await imported.json();for(const id of importedData.ids)created.push(['nodes',id]);
  if(importedData.node_count!==1)throw Error('structured inventory count mismatch');
  await page.setViewportSize({width:390,height:900});if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('resource form mobile overflow');
  await page.screenshot({path:'docs/evidence/live-resource-form-mobile.png',fullPage:true});
  if(errors.length)throw Error('browser page error');
  fs.writeFileSync('docs/evidence/live-resource-forms.json',JSON.stringify({date:new Date().toISOString(),checks:['source ordinary form','create synthetic node through form','profile references remain arrays across advanced mode','create profile and preview actual output','UIAble template target selection and DNS form save','assigned template affects actual profile preview','structured inventory import through ordinary form','mobile form without horizontal overflow'],errors},null,2));
  console.log('Actual node/profile form and mobile acceptance passed');
 }finally{
  for(const [kind,id] of created.reverse()){
   const status=await page.evaluate(async({kind,id})=>(await fetch(`/api/v1/admin/resources/${kind}/${id}`,{method:'DELETE'})).status,{kind,id});if(status!==200)throw Error('test resource cleanup failed');
  }
  await browser.close();
 }
})().catch(async e=>{if(browser)await browser.close();console.error(e.message);process.exitCode=1});
