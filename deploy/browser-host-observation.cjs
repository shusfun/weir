require('./production-guard.cjs');
const {chromium}=require('playwright');const fs=require('fs'),path=require('path');let browser;
(async()=>{
 const mode=process.argv[2];if(!['fresh','stale','recovered'].includes(mode))throw Error('Mode required');
 const s=JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA,'MiGate/operations/deployment.json'),'utf8'));
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});const c=await browser.newContext({viewport:{width:1440,height:1000}});
 await c.addCookies([{name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);const p=await c.newPage();const errors=[];p.on('pageerror',()=>errors.push(true));await p.goto(`https://sub.caowo.baby/devices/${s.device_id}`);
 const panel=p.getByLabel('主机观测',{exact:true});await panel.getByRole('heading',{name:'主机观测',exact:true}).waitFor();
 if(mode==='stale'){await panel.getByText('未知 / 观测已过期',{exact:false}).waitFor();for(const el of await panel.locator('.metric-number').all()){if(await el.textContent()!=='未知')throw Error('Stale host values presented as fresh');}}
 else {await panel.getByText('只读系统采样 · 已更新',{exact:true}).waitFor();if((await panel.locator('.metric-number').allTextContents()).includes('未知'))throw Error('Live host data missing');}
 for(const width of [1440,1280,768,390]){await p.setViewportSize({width,height:1000});await panel.scrollIntoViewIfNeeded();if(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Host page overflow');await panel.screenshot({path:`docs/evidence/host-${mode}-${width}.png`});}
 if(errors.length)throw Error('Host page runtime error');await browser.close();console.log('Real host observation UI '+mode+' passed');
})().catch(async()=>{console.error('Host observation browser failed');if(browser)await browser.close();process.exitCode=1;});
