require('./production-guard.cjs');
const {chromium}=require('playwright');
const fs=require('fs'),path=require('path');let browser;
(async()=>{
 const root=path.join(process.env.LOCALAPPDATA,'MiGate/operations');const f=JSON.parse(fs.readFileSync(path.join(root,'release-fixture.json'),'utf8'));
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});
 const c=await browser.newContext({viewport:{width:1440,height:1000}});await c.addCookies([{name:'migate_session',value:f.cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);
 const p=await c.newPage();const errors=[];p.on('pageerror',()=>errors.push('runtime'));
 await p.goto('https://sub.caowo.baby/subscription');const card=p.locator('article.client-choice').filter({has:p.getByRole('heading',{name:f.name,exact:true})});await card.waitFor();
 const release=card.getByLabel('客户端发布版本',{exact:true});await release.getByRole('link',{name:f.version,exact:true}).waitFor();
 await p.waitForFunction(()=>Array.from(document.querySelectorAll('.client-release')).every(el=>!el.textContent.includes('查询中')&&!el.textContent.includes('正在查询')));
 const link=release.getByRole('link');if(await link.getAttribute('href')!==f.url)throw Error('Release target mismatch');if(!(await link.getAttribute('title')).includes('查询于'))throw Error('Missing observation time');
 for(const width of [1440,1280,768,390]){await p.setViewportSize({width,height:1000});await release.scrollIntoViewIfNeeded();if(await release.evaluate(el=>getComputedStyle(el).display)!=='block')throw Error('Release metadata must have own row');if(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Overflow');await p.screenshot({path:`docs/evidence/client-release-${width}.png`,mask:[p.locator('.subscription-url'),p.locator('.qr-code')]});}
 // Genuine browser request failure: unrelated own subscription remains available.
 await p.route('**/api/v1/clients/*/release',route=>route.abort());await p.reload();await card.getByText('版本未知（查询失败）',{exact:true}).waitFor();await p.getByRole('textbox',{name:'clash订阅地址',exact:true}).waitFor();
 await card.scrollIntoViewIfNeeded();await p.screenshot({path:'docs/evidence/client-release-failure-390.png',mask:[p.locator('.subscription-url'),p.locator('.qr-code')]});
 if(errors.length)throw Error('Runtime errors');await browser.close();console.log('Release card four widths and network failure isolation passed');
})().catch(async e=>{if(browser)await browser.close();console.error('Release UI acceptance failed ('+(e?.name||'Error')+')');process.exitCode=1;});
