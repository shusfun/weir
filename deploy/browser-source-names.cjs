require('./production-guard.cjs');
const { chromium } = require('playwright');
const fs = require('fs'), path = require('path');
let browser;
(async()=>{
 const id=Number(process.argv[2]),group=Number(process.argv[3]);
 if(!Number.isInteger(id)||id<=0||!Number.isInteger(group)||group<=0)throw Error('Owned fixture IDs required');
 const s=JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA,'MiGate/operations/deployment.json'),'utf8'));
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});
 const c=await browser.newContext({viewport:{width:1440,height:1000}});
 await c.addCookies([{name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);
 const p=await c.newPage(),checks=[];
 async function api(method,endpoint,data){return p.evaluate(async({method,endpoint,data})=>{const r=await fetch('/api/v1'+endpoint,{method,headers:{'Content-Type':'application/json'},body:data?JSON.stringify(data):undefined});if(!r.ok)throw Error('API rejected');return r.json();},{method,endpoint,data});}
 async function source(){return (await api('GET','/admin/resources/sources')).items.find(x=>x.id===id)?.data;}
 try{
  await p.goto('https://sub.caowo.baby/subscriptions?kind=sources');
  if((await source())?.name!=='Acceptance source fixture')throw Error('Owned fixture missing');
  const row=p.locator('article.inventory-item').filter({has:p.getByRole('heading',{name:'Acceptance source fixture',exact:true})});
  for(const enabled of [true,false]){
   await row.getByRole('button',{name:'编辑',exact:true}).click();
   const d=p.getByRole('dialog'),toggle=d.getByRole('switch',{name:'URI 节点名称中的 + 视为空格',exact:true});
   if((await toggle.getAttribute('aria-checked'))===(enabled?'true':'false'))throw Error('Unexpected fixture option');
   await toggle.click();
   if(enabled)for(const width of [1440,1280,768,390]){
    await p.setViewportSize({width,height:1000});await toggle.scrollIntoViewIfNeeded();
    const b=await d.boundingBox();if(!b||b.x<0||b.x+b.width>width+1||await d.evaluate(el=>el.scrollWidth>el.clientWidth+1))throw Error('Name option dialog clipped');
    await p.screenshot({path:`docs/evidence/source-names-${width}.png`,mask:[p.locator('.sensitive-output')],fullPage:false});
   }
   const saved=p.waitForResponse(r=>r.url().endsWith('/api/v1/admin/resources/sources')&&r.request().method()==='POST');
   await d.getByRole('button',{name:'保存',exact:true}).click();if((await saved).status()!==200)throw Error('Toggle save rejected');await d.waitFor({state:'hidden'});
   const data=await source();if(data.plus_as_space!==enabled||data.links||data.upstream_usage)throw Error('Toggle retained stale inventory');
   const refreshed=p.waitForResponse(r=>r.url().endsWith(`/api/v1/admin/sources/${id}/refresh`));
   await row.getByRole('button',{name:'刷新',exact:true}).click();if((await refreshed).status()!==200)throw Error('UI refresh failed');
   const preview=await api('POST','/admin/profiles/preview',{id:group,target:'singbox'}),out=JSON.parse(preview.output).outbounds.find(x=>x.type==='trojan');
   if(out?.tag!==(enabled?'HK + %2B':'HK+++%2B')||out.password!=='p+a+%25'||out.transport.path!=='/a+b%2B')throw Error('Name/credential/transport preservation failed');
   checks.push(enabled?'UI toggle on, cache cleared, UI refresh yields space; encoded plus/password/path preserved':'UI toggle off clears transformed cache; literal name restored after refresh');
  }
  fs.writeFileSync('docs/evidence/source-names-ui.json',JSON.stringify({version:(await api('GET','/admin/overview')).version,date:new Date().toISOString(),checks,visual:'four widths captured; manual review pending'},null,2));
 }finally{await browser.close();}
 console.log('Real source name toggle, invalidation, UI refresh and output preservation passed');
})().catch(async()=>{if(browser)await browser.close();console.error('Source name workflow failed; private inputs omitted');process.exitCode=1;});
