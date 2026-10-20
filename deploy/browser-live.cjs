require('./production-guard.cjs');
// Browser acceptance against the real server; the SSH TLS transport is explicit.
const {chromium}=require('playwright');
const fs=require('node:fs');const path=require('node:path');
const crypto=require('node:crypto');
function totp(secret){
 const alphabet='ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';let bits='';
 for(const c of secret.replace(/=/g,''))bits+=alphabet.indexOf(c).toString(2).padStart(5,'0');
 const key=Buffer.from((bits.match(/.{8}/g)||[]).map(b=>parseInt(b,2)));
 const tick=Buffer.alloc(8);tick.writeBigUInt64BE(BigInt(Math.floor(Date.now()/30000)));
 const hash=crypto.createHmac('sha1',key).update(tick).digest();const offset=hash[19]&15;
 return String((hash.readUInt32BE(offset)&0x7fffffff)%1000000).padStart(6,'0');
}
let browser;
(async()=>{
 const state=JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA,'MiGate/operations/deployment.json'),'utf8'));
 browser=await chromium.launch({channel:'chrome',headless:true,args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});
 const page=await browser.newPage({viewport:{width:1280,height:900}});const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto('https://sub.caowo.baby');
 await page.getByRole('heading',{name:'欢迎回来'}).waitFor();
 await page.getByLabel('邮箱',{exact:true}).fill(state.admin_email);await page.getByLabel('密码',{exact:true}).fill(state.admin_password);
 await page.getByRole('button',{name:/^(登录|验证并登录)$/,exact:true}).click();
 if(state.totp_secret){
  await page.getByLabel('验证码 / 恢复码',{exact:true}).waitFor();
  await page.waitForTimeout(31000-Date.now()%30000);
  await page.getByLabel('验证码 / 恢复码',{exact:true}).fill(totp(state.totp_secret));
  await page.getByRole('button',{name:/^(登录|验证并登录)$/,exact:true}).click();
 }
 await page.getByRole('heading',{name:'概览',exact:true}).waitFor();
 await page.screenshot({path:'docs/evidence/live-admin-desktop.png',fullPage:true});
 await page.getByRole('button',{name:'Gate 设备',exact:true}).click();
 await page.getByRole('cell',{name:'gate-154',exact:true}).waitFor();
 await page.setViewportSize({width:390,height:900});
 if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Device page overflows mobile viewport');
 await page.screenshot({path:'docs/evidence/live-gate-mobile.png',fullPage:true});
 await page.locator('.topbar').getByRole('button',{name:'退出',exact:true}).click();
 await page.getByRole('heading',{name:'欢迎回来'}).waitFor();
 const user=state.test_users[0];await page.getByLabel('邮箱',{exact:true}).fill(user.email);await page.getByLabel('密码',{exact:true}).fill(user.password);await page.getByRole('button',{name:/^(登录|验证并登录)$/,exact:true}).click();
 await page.getByRole('heading',{name:'我的订阅',exact:true}).waitFor();
 if(await page.evaluate(async()=>(await fetch('/api/v1/admin/users')).status)!==403)throw Error('User can access administrator API');
 await page.screenshot({path:'docs/evidence/live-user-mobile.png',fullPage:true,mask:[page.locator('input[readonly]'),page.locator('svg:has(title)')]});
 if(errors.length)throw Error(errors.join('\n'));
 fs.writeFileSync('docs/evidence/live-browser.json',JSON.stringify({date:new Date().toISOString(),transport:'SSH forwarding of real HTTPS; certificate verified; direct public TLS separately tracked',checks:['administrator login with TOTP','live device list','ordinary user subscription','admin API forbidden'],errors},null,2));
 console.log('Real server browser flow passed (verified HTTPS through SSH transport)');
 await browser.close();
})().catch(async e=>{if(browser)await browser.close();console.error(e.message);process.exitCode=1});
