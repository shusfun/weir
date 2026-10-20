require('./production-guard.cjs');
// Operates only owned source and profile fixtures from the companion verifier.
const {chromium}=require('playwright');const fs=require('fs'),path=require('path');let browser,stage='launch';
(async()=>{
 const root=path.join(process.env.LOCALAPPDATA,'MiGate/operations'),s=JSON.parse(fs.readFileSync(path.join(root,'deployment.json'),'utf8'));
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});const c=await browser.newContext({viewport:{width:1440,height:1000}});
 await c.addCookies([{name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);const p=await c.newPage();await p.goto('https://sub.caowo.baby/subscriptions?kind=sources');
 const row=p.locator('article.inventory-item').filter({has:p.getByRole('heading',{name:'Owned provenance source',exact:true})});await row.getByRole('button',{name:'编辑',exact:true}).click();const d=p.getByRole('dialog');
 stage='source group';await d.getByLabel('订阅源分组',{exact:true}).fill('Z');
 for(const width of [1440,1280,768,390]){await p.setViewportSize({width,height:1000});await d.getByLabel('订阅源分组',{exact:true}).scrollIntoViewIfNeeded();if(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Page overflow');await p.screenshot({path:`docs/evidence/provenance-source-${width}.png`,mask:[d.getByLabel('订阅源地址',{exact:true})]});}
 const [r]=await Promise.all([p.waitForResponse(r=>r.url().endsWith('/admin/resources/sources')&&r.request().method()==='POST'),d.getByRole('button',{name:'保存',exact:true}).click()]);if(r.status()!==200)throw Error('Source metadata save rejected');await d.waitFor({state:'hidden'});
 stage='profile preview';await p.getByRole('tab',{name:'订阅组',exact:true}).click();const group=p.locator('article.inventory-item').filter({has:p.getByRole('heading',{name:'Owned provenance group',exact:true})});await group.getByRole('button',{name:'预览',exact:true}).click();const preview=p.getByLabel('库存预览',{exact:true});await preview.waitFor();
 const output=await preview.inputValue();if(!output.includes('A-Owned%20provenance%20manual-original')||!output.includes('Z-Owned%20provenance%20source-original'))throw Error('Actual UI preview lost provenance');
 for(const width of [1440,1280,768,390]){await p.setViewportSize({width,height:1000});await preview.scrollIntoViewIfNeeded();const box=await p.getByRole('dialog').boundingBox();if(!box||box.x<0||box.x+box.width>width+1)throw Error('Preview clipped');await p.screenshot({path:`docs/evidence/provenance-preview-${width}.png`,mask:[preview]});}
 await p.keyboard.press('Escape');await p.getByRole('tab',{name:'处理链',exact:true}).click();const chain=p.locator('article.inventory-item').filter({has:p.getByRole('heading',{name:'Owned provenance chain',exact:true})});await chain.getByRole('button',{name:'编辑',exact:true}).click();await p.getByText(/可用字段：/).waitFor();
 for(const width of [1440,1280,768,390]){await p.setViewportSize({width,height:1000});await p.getByText(/可用字段：/).scrollIntoViewIfNeeded();await p.screenshot({path:`docs/evidence/provenance-template-${width}.png`});}
 await browser.close();console.log('Real source-group form and source-aware profile preview passed');
})().catch(async()=>{fs.writeFileSync('docs/evidence/provenance-ui-failure.json',JSON.stringify({stage,date:new Date().toISOString()}));console.error('Provenance browser failed at '+stage);if(browser)await browser.close();process.exitCode=1;});
