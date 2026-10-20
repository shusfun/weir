require('./production-guard.cjs');
// Only mutates the owned catalog fixture. Credentials are never printed or captured.
const {chromium}=require('playwright');
const fs=require('fs'),path=require('path');
let browser,stage='launch';
(async()=>{
 const mode=process.argv[2],root=path.join(process.env.LOCALAPPDATA,'MiGate/operations');
 const s=JSON.parse(fs.readFileSync(path.join(root,'deployment.json'),'utf8'));
 const f=JSON.parse(fs.readFileSync(path.join(root,'catalog-fixture.json'),'utf8'));
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});
 const c=await browser.newContext({viewport:{width:1440,height:1000}});
 await c.addCookies([{name:'migate_session',value:(mode==='user'?f.user_cookie:s.admin_cookie).slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);
 const p=await c.newPage();const errors=[];p.on('pageerror',()=>errors.push('pageerror'));
 async function shots(label,element){for(const width of [1440,1280,768,390]){await p.setViewportSize({width,height:1000});if(element)await element.scrollIntoViewIfNeeded();if(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Page overflow');await p.screenshot({path:`docs/evidence/client-catalog-${label}-${width}.png`,mask:[p.locator('.subscription-url'),p.locator('.qr-code')]});}}
 if(mode==='user'){
  stage='user catalog';await p.goto('https://sub.caowo.baby/subscription');
  const card=p.locator('article.client-choice').filter({has:p.getByRole('heading',{name:f.name,exact:true})});await card.waitFor();
  const icon=card.locator('img.catalog-uploaded-icon');await icon.waitFor();await icon.evaluate(img=>{if(!img.complete||img.naturalWidth!==32)throw Error('Uploaded icon not decoded');});
  await card.getByRole('button',{name:'使用此客户端',exact:true}).click();
  const input=p.getByRole('textbox',{name:'singbox订阅地址',exact:true});await input.waitFor();
  const value=await input.inputValue();if(!value.startsWith(f.subscription_url+'?target=singbox'))throw Error('Wrong own subscription');
  await p.evaluate(()=>{window.open=(url)=>{window.__catalogDownload=url;return null;}});
  await card.getByRole('button',{name:'下载客户端',exact:true}).click();
  if(await p.evaluate(()=>window.__catalogDownload)!==f.url)throw Error('Download metadata changed');
  if(await p.locator('.qr-code svg title').textContent()!=='我的订阅二维码')throw Error('QR missing');
  await shots('user',card);await shots('user-subscription',input);
  await p.getByRole('button',{name:/查看全部客户端/}).click();if(await p.locator('article.client-choice').count()<17)throw Error('Expansion lost clients');await p.getByRole('button',{name:'收起客户端目录',exact:true}).click();if(await p.locator('article.client-choice').count()!==6)throw Error('Collapse failed');
  await p.getByRole('combobox',{name:'客户端适用平台',exact:true}).click();await p.getByRole('option',{name:'Android',exact:true}).click();await card.waitFor({state:'hidden'});
 }else{
  stage='admin catalog';await p.goto('https://sub.caowo.baby/settings?tab=clients');
  const editor=p.locator('.client-catalog-editor');await editor.getByRole('heading',{name:'客户端目录',exact:true}).waitFor();
  const row=editor.locator('article.catalog-row').filter({has:p.getByText(f.name,{exact:true})});
  if(mode==='add'){
   await editor.getByRole('button',{name:'新增客户端',exact:true}).click();
   const d=p.getByRole('dialog');await d.getByLabel('客户端名称',{exact:true}).fill(f.name);await d.getByLabel('客户端说明',{exact:true}).fill('Owned acceptance client — long name, desktop platform, exact own subscription format.');await d.getByLabel('客户端下载地址',{exact:true}).fill(f.url);
   await d.getByRole('combobox',{name:'目录订阅格式',exact:true}).click();await p.getByRole('option',{name:'sing-box',exact:true}).click();
   await d.getByLabel('上传客户端图标（可选）',{exact:true}).setInputFiles({name:'rejected.svg',mimeType:'image/svg+xml',buffer:Buffer.from('<svg/>')});await d.getByRole('alert').filter({hasText:'请选择不超过'}).waitFor();
   await d.getByLabel('上传客户端图标（可选）',{exact:true}).setInputFiles({name:'owned-icon.jpg',mimeType:'image/jpeg',buffer:Buffer.from(f.icon_base64,'base64')});await d.locator('img.catalog-uploaded-icon').waitFor();
   await shots('edit',d.getByLabel('客户端名称',{exact:true}));await shots('edit-actions',d.getByRole('button',{name:'暂存客户端',exact:true}));
   await d.getByRole('button',{name:'暂存客户端',exact:true}).click();
   // Exercise the actual up/down controls, preserving the original rows' relative order.
   const move=row.getByRole('button',{name:'上移 '+f.name,exact:true});while(!await move.isDisabled())await move.click();
  }else if(mode==='disable'){
   await row.getByRole('button',{name:'编辑',exact:true}).click();const d=p.getByRole('dialog');const persisted=d.locator('img.catalog-uploaded-icon');await persisted.waitFor();await persisted.evaluate(img=>{if(!img.complete||img.naturalWidth!==32)throw Error('Persisted icon missing');});await d.getByRole('button',{name:'移除上传图标',exact:true}).click();await persisted.waitFor({state:'hidden'});const sw=d.getByRole('switch',{name:'向用户显示此客户端',exact:true});if(await sw.getAttribute('aria-checked')!=='true')throw Error('Enabled state not restored');await sw.click();await d.getByRole('button',{name:'暂存客户端',exact:true}).click();
  }else if(mode==='delete'){
   await row.getByRole('button',{name:'删除',exact:true}).click();await p.getByRole('dialog').getByRole('button',{name:'确认移除目录项',exact:true}).click();
  }else throw Error('Unknown mode');
  stage='review';await editor.getByRole('button',{name:'核对并保存目录',exact:true}).click();const d=p.getByRole('dialog');await shots('review-'+mode,d);
  stage='reauth publish';await d.getByLabel('管理员密码',{exact:true}).fill(s.admin_password);const crypto=require('crypto');while(Date.now()/1000%30>25)await new Promise(r=>setTimeout(r,1000));
  const alphabet='ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';let bits='';for(const x of s.totp_secret.replace(/=+$/,''))bits+=alphabet.indexOf(x).toString(2).padStart(5,'0');const key=Buffer.from((bits.match(/.{8}/g)||[]).map(b=>parseInt(b,2)));const counter=Buffer.alloc(8);counter.writeBigUInt64BE(BigInt(Math.floor(Date.now()/30000)));const hash=crypto.createHmac('sha1',key).update(counter).digest();const offset=hash[19]&15;const code=String((hash.readUInt32BE(offset)&0x7fffffff)%1000000).padStart(6,'0');await d.getByLabel('二次验证码（已绑定时必填）',{exact:true}).fill(code);
  const [r]=await Promise.all([p.waitForResponse(r=>r.url().endsWith('/api/v1/admin/clients')&&r.request().method()==='PUT'),d.getByRole('button',{name:'确认发布目录',exact:true}).click()]);if(r.status()!==200)throw Error('Publish rejected: '+r.status());await d.waitFor({state:'hidden'});await editor.getByRole('status').waitFor();
  if(mode==='add'){await shots('admin',row);const search=editor.getByRole('textbox',{name:'搜索客户端目录',exact:true});if(await search.count()){await search.fill('nonexistent-owned-client');await editor.getByText('没有匹配的客户端。',{exact:true}).waitFor();await search.fill(f.name);if(await editor.locator('article.catalog-row').count()!==1)throw Error('Catalog search failed');await search.fill('');}}
 }
 if(errors.length)throw Error('Browser runtime error');await browser.close();console.log('Real catalog UI '+mode+' passed');
})().catch(async e=>{fs.writeFileSync('docs/evidence/client-catalog-ui-failure.json',JSON.stringify({stage,error_type:e?.name||'Error',date:new Date().toISOString()}));console.error('Catalog UI failed at '+stage);if(browser)await browser.close();process.exitCode=1;});
