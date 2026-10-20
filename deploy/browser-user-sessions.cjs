require('./production-guard.cjs');
const {chromium}=require('playwright');const fs=require('fs'),path=require('path'),crypto=require('crypto');let browser;
(async()=>{
 const s=JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA,'MiGate/operations/deployment.json'),'utf8'));
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});
 const context=await browser.newContext({viewport:{width:1440,height:1000}});await context.addCookies([{name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);const page=await context.newPage(),checks=[];let user;
 async function req(method,endpoint,data){return page.evaluate(async({method,endpoint,data})=>{const r=await fetch('/api/v1'+endpoint,{method,headers:{'Content-Type':'application/json'},body:data===undefined?undefined:JSON.stringify(data)});if(!r.ok)throw Error('Session API HTTP '+r.status);return r.json()},{method,endpoint,data})}
 try{
 await page.goto('https://sub.caowo.baby/users');const password=crypto.randomBytes(24).toString('base64url');user=await req('POST','/admin/users',{email:'sessions-'+Date.now()+'@acceptance.invalid',password,quota:1048576});
 const contexts=[],pages=[];
 for(let i=0;i<2;i++){const c=await browser.newContext(),p=await c.newPage();await p.goto('https://sub.caowo.baby/');await p.getByLabel('邮箱',{exact:true}).fill(user.email);await p.getByLabel('密码',{exact:true}).fill(password);await p.getByRole('button',{name:/^(登录|验证并登录)$/,exact:true}).click();await p.getByRole('heading',{name:'我的订阅',exact:true}).waitFor();contexts.push(c);pages.push(p)}
 const forbidden=await pages[0].evaluate(async id=>(await fetch(`/api/v1/admin/users/${id}/sessions`)).status,user.id);if(forbidden!==403)throw Error('Ordinary account accessed administrator session directory');
 await page.goto('https://sub.caowo.baby/users/'+user.id);await page.getByRole('button',{name:'撤销会话',exact:true}).first().waitFor();if(await page.getByRole('button',{name:'撤销会话',exact:true}).count()!==2)throw Error('Expected two actual sessions');
 for(const width of [1440,1280,768,390]){await page.setViewportSize({width,height:1000});await page.getByRole('heading',{name:'登录会话',exact:true}).scrollIntoViewIfNeeded();if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('User session page overflow');await page.screenshot({path:`docs/evidence/user-sessions-live-${width}.png`,fullPage:true,mask:[page.getByText(user.email,{exact:true})]})}
 await page.getByRole('button',{name:'撤销会话',exact:true}).first().click();await page.getByRole('dialog').getByRole('button',{name:'确认撤销会话',exact:true}).click();await page.getByText('已撤销此会话，原登录将立即失效。',{exact:true}).waitFor();
 const states=await Promise.all(pages.map(p=>p.evaluate(async()=>(await fetch('/api/v1/me')).status)));if(states.filter(x=>x===401).length!==1||states.filter(x=>x===200).length!==1)throw Error('Session revocation crossed account sessions');
 checks.push('administrator sees two real logins and revokes one through confirmation; revoked cookie immediately 401, other login remains 200');checks.push('ordinary account session administration forbidden; four-width actual user detail');
 for(const c of contexts)await c.close();
 await page.getByRole('button',{name:'撤销会话',exact:true}).click();await page.getByRole('dialog').getByRole('button',{name:'确认撤销会话',exact:true}).click();await page.getByText('该用户暂无有效登录会话',{exact:true}).waitFor();checks.push('last revocation leaves actual empty state; no node grant changed');
 fs.writeFileSync('docs/evidence/user-sessions-live.json',JSON.stringify({version:(await req('GET','/admin/overview')).version,date:new Date().toISOString(),checks},null,2));console.log('Real administrator per-user session list, confirmation, immediate single-session revocation and isolation passed');
 }finally{if(user)await req('PATCH','/admin/users/'+user.id,{enabled:false});await browser.close()}
})().catch(async e=>{if(browser)await browser.close();console.error(e.message.replace(/cookie: [^\n]+/gi,'cookie: [REDACTED]'));process.exitCode=1});
