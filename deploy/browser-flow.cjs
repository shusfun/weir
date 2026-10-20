// Real browser + disposable PG/Redis integration. Requires fresh test.compose
// services and a local center initialized with the supplied token-file path.
const {chromium}=require('playwright');
const fs=require('node:fs');
const path=require('node:path');
let browser;
(async()=>{
 const tokenFile=process.env.MIGATE_QA_TOKEN_FILE;if(!tokenFile)throw Error('MIGATE_QA_TOKEN_FILE required');
 const token=fs.readFileSync(tokenFile,'utf8').trim();
 const password='browser-local-test-only';const admin='admin@browser.example';const user='alice@browser.example';
 browser=await chromium.launch({channel:'chrome',headless:true});
 const page=await browser.newPage({viewport:{width:1280,height:900}});const errors=[];page.on('pageerror',e=>errors.push(String(e)));
 await page.goto('http://localhost:19810');await page.getByLabel('短时初始化凭证').fill(token);await page.getByRole('button',{name:'下一步',exact:true}).click();
 await page.getByLabel('端口',{exact:true}).fill('25432');await page.getByLabel('密码',{exact:true}).fill('local-test-only');await page.getByRole('button',{name:'下一步',exact:true}).click();
 await page.getByLabel('连接地址').fill('127.0.0.1:26379');await page.getByLabel('密码',{exact:true}).fill('local-test-only');await page.getByRole('button',{name:'下一步',exact:true}).click();
 await page.getByLabel('管理员邮箱').fill(admin);await page.getByLabel('密码（12–72 字节）').fill(password);await page.getByRole('button',{name:'安装并锁定'}).click();
 await page.getByRole('heading',{name:'欢迎回来'}).waitFor();
 await page.getByLabel('邮箱',{exact:true}).fill(admin);await page.getByLabel('密码',{exact:true}).fill(password);await page.getByRole('button',{name:/^(登录|验证并登录)$/,exact:true}).click();
 await page.getByRole('heading',{name:'概览',exact:true}).waitFor();await page.getByText('观测状态',{exact:true}).waitFor();
 await page.screenshot({path:'docs/evidence/admin-1280.png',fullPage:true});
 await page.getByRole('button',{name:'用户管理',exact:true}).click();const form=page.locator('form').filter({has:page.getByLabel('初始密码')});
 await form.getByLabel('邮箱',{exact:true}).fill(user);await form.getByLabel('初始密码').fill(password);await form.getByLabel('额度（字节）',{exact:true}).fill(String(32*1024*1024));await form.getByLabel('到期 Unix 时间；0 为不限').fill('0');await form.getByRole('button',{name:'保存',exact:true}).click();
 await page.getByRole('cell',{name:user,exact:true}).waitFor();
 const row=page.getByRole('row').filter({has:page.getByRole('cell',{name:user,exact:true})});await row.getByRole('button',{name:'停用',exact:true}).click();await page.getByRole('dialog').waitFor();await page.getByRole('button',{name:'取消',exact:true}).click();
 await page.getByRole('dialog').waitFor({state:'hidden'});
 await page.screenshot({path:'docs/evidence/users-1280.png',fullPage:true});
 await page.setViewportSize({width:390,height:900});if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('mobile user page overflow');await page.screenshot({path:'docs/evidence/users-390.png',fullPage:true});
 await page.locator('.topbar').getByRole('button',{name:'退出',exact:true}).click();await page.getByRole('heading',{name:'欢迎回来'}).waitFor();await page.getByLabel('邮箱',{exact:true}).fill(user);await page.getByLabel('密码',{exact:true}).fill(password);await page.getByRole('button',{name:/^(登录|验证并登录)$/,exact:true}).click();await page.getByRole('heading',{name:'我的订阅',exact:true}).waitFor();
 if(await page.getByRole('button',{name:'用户管理',exact:true}).count())throw Error('user can see admin menu');
 const status=await page.evaluate(async()=>(await fetch('/api/v1/admin/users')).status);if(status!==403)throw Error('admin permission not enforced');
 await page.getByText('有效节点',{exact:true}).waitFor();if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('mobile subscription overflow');
 await page.screenshot({path:'docs/evidence/user-390.png',fullPage:true,mask:[page.locator('input[readonly]'),page.locator('svg:has(title)')]});
 await browser.close();if(errors.length)throw Error(errors.join('\n'));
 fs.writeFileSync('docs/evidence/browser-flow.json',JSON.stringify({date:new Date().toISOString(),checks:['real setup wizard PG + Redis ACL','admin password login','admin creates user','destructive confirmation cancelled','desktop/mobile users table','ordinary user subscription','admin menu isolation','direct admin endpoint rejected 403'],pageErrors:errors},null,2));console.log('Real browser flow passed; subscription credentials masked in evidence');
})().catch(async e=>{if(browser)await browser.close();console.error(e.message);process.exitCode=1});
