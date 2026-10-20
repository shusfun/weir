require('./production-guard.cjs');
const { chromium } = require('playwright');
const fs = require('fs'), path = require('path');let browser;
(async () => {
 const s = JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA, 'MiGate/operations/deployment.json'), 'utf8'));
 browser = await chromium.launch({ channel: 'chrome', args: ['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443'] });
 const c = await browser.newContext({viewport:{width:1440,height:1000}});
 await c.addCookies([{name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);
 const page = await c.newPage(), checks=[];let id;
 const endpoint='/api/v1/admin/resources/templates';
 try {
  await page.goto('https://sub.caowo.baby/subscriptions?kind=templates');
  await page.getByRole('button',{name:'新增输出模板',exact:true}).click();
  const dialog=page.getByRole('dialog'),name='dns-ui-'+Date.now();
  await dialog.getByLabel('名称',{exact:true}).fill(name);
  await dialog.getByLabel('DNS 服务器（逗号分隔；IP、https:// 或 tls://）',{exact:true}).fill('https://cloudflare-dns.com/dns-query,tls://1.1.1.1');
  let response=page.waitForResponse(r=>r.url().endsWith(endpoint)&&r.request().method()==='POST');
  await dialog.getByRole('button',{name:'保存',exact:true}).click();
  if((await response).status()!==400)throw Error('Unbootstrapped encrypted hostname accepted');
  await dialog.getByText(/模板无效/).waitFor();
  await dialog.getByLabel('引导 DNS IP（加密 DNS 使用域名时必填，逗号分隔）',{exact:true}).fill('1.1.1.1');
  for(const width of [1440,1280,768,390]){
   await page.setViewportSize({width,height:1000});await dialog.getByLabel('DNS 服务器（逗号分隔；IP、https:// 或 tls://）',{exact:true}).scrollIntoViewIfNeeded();
   await page.screenshot({path:`docs/evidence/template-dns-live-${width}.png`});
  }
  response=page.waitForResponse(r=>r.url().endsWith(endpoint)&&r.request().method()==='POST');
  await dialog.getByRole('button',{name:'保存',exact:true}).click();const saved=await response;
  if(saved.status()!==200)throw Error('Encrypted template save failed');id=(await saved.json()).id;
  await dialog.waitFor({state:'hidden'});
  const resource=await page.evaluate(async({endpoint,id})=>(await (await fetch(endpoint)).json()).items.find(r=>r.id===id),{endpoint,id});
  if(resource.data.dns[0]!=='https://cloudflare-dns.com/dns-query'||resource.data.dns_bootstrap[0]!=='1.1.1.1')throw Error('Encrypted DNS fields not persisted');
  checks.push('actual UI rejects hostname without bootstrap; adding bootstrap saves encrypted DNS fields');
  checks.push('four viewport form screenshots; client TLS verification remains enabled');
  const version=await page.evaluate(async()=>(await(await fetch('/api/v1/admin/overview')).json()).version);
  fs.writeFileSync('docs/evidence/template-dns-live.json',JSON.stringify({version,date:new Date().toISOString(),checks},null,2));
 } finally {
  if(id)await page.evaluate(async({endpoint,id})=>{const r=await fetch(endpoint+'/'+id,{method:'DELETE'});if(!r.ok)throw Error('Template cleanup failed')},{endpoint,id});
  await browser.close();
 }
 console.log('Encrypted DNS template actual form validation, correction, persistence and four-width screenshots passed');
})().catch(async e=>{if(browser)await browser.close();console.error(e.message);process.exitCode=1});
