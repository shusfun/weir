require('./production-guard.cjs');
const {chromium}=require('playwright');const fs=require('fs'),path=require('path');let browser;
(async()=>{
 const s=JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA,'MiGate/operations/deployment.json'),'utf8'));
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});
 const context=await browser.newContext({viewport:{width:1440,height:1000}});await context.addCookies([{name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);
 const page=await context.newPage(),owned=[],checks=[];
 async function req(method,url,data){return page.evaluate(async({method,url,data})=>{const r=await fetch('/api/v1'+url,{method,headers:{'Content-Type':'application/json'},body:data===undefined?undefined:JSON.stringify(data)});if(!r.ok)throw Error('Remote rule UI HTTP '+r.status);return r.json()},{method,url,data})}
 try{
  await page.goto('https://sub.caowo.baby/subscriptions?kind=templates');await page.getByRole('button',{name:'新增输出模板',exact:true}).click();const dialog=page.getByRole('dialog');
  await dialog.getByLabel('名称',{exact:true}).fill('ruleset-ui-'+Date.now());
  await dialog.getByRole('button',{name:'添加远程规则集',exact:true}).click();
  await dialog.getByLabel('规则集 1 名称',{exact:true}).fill('PublicDomains');await dialog.getByLabel('规则集 1 HTTPS 地址',{exact:true}).fill('https://example.org/domains.yaml');
  await dialog.getByRole('button',{name:'添加规则',exact:true}).click();await dialog.getByLabel('规则 1 类型',{exact:true}).click();await page.getByRole('option',{name:'远程规则集',exact:true}).click();
  if(!(await dialog.getByLabel('规则 1 规则集',{exact:true}).innerText()).includes('PublicDomains'))throw Error('Rule selector did not choose named declaration');
  await dialog.getByLabel('规则集 1 名称',{exact:true}).fill('RenamedDomains');
  if(!(await dialog.getByLabel('规则 1 规则集',{exact:true}).innerText()).includes('RenamedDomains'))throw Error('Rule-set rename lost reference');
  if(!await dialog.getByRole('button',{name:'删除远程规则集 1',exact:true}).isDisabled())throw Error('Referenced rule set can be deleted');
  for(const width of [1440,1280,768,390]){await page.setViewportSize({width,height:1000});await dialog.getByLabel('规则集 1 HTTPS 地址',{exact:true}).scrollIntoViewIfNeeded();const box=await dialog.boundingBox();if(box.x<0||box.x+box.width>width)throw Error('Rule-set dialog clipped '+width);await page.screenshot({path:`docs/evidence/template-rulesets-${width}.png`});await dialog.getByLabel('规则 1 规则集',{exact:true}).scrollIntoViewIfNeeded();await page.screenshot({path:`docs/evidence/template-ruleset-reference-${width}.png`})}
  const savedResponse=page.waitForResponse(r=>r.url().endsWith('/api/v1/admin/resources/templates')&&r.request().method()==='POST');await dialog.getByRole('button',{name:'保存',exact:true}).click();const response=await savedResponse;if(response.status()!==200)throw Error('Remote template UI save failed');const saved=await response.json();owned.push(['templates',saved.id]);await dialog.waitFor({state:'hidden'});
  const node=await req('POST','/admin/resources/nodes',{data:{name:'ruleset-preview',url:'socks5://u:fixture@example.org:1080#HK-fixture'}});owned.push(['nodes',node.id]);
  const profile=await req('POST','/admin/resources/profiles',{data:{name:'ruleset-preview',node_ids:[node.id],template_ids:[saved.id]}});owned.push(['profiles',profile.id]);
  const result=await req('POST','/admin/profiles/preview',{id:profile.id,target:'clash'});
  if(!result.output.includes('RULE-SET,RenamedDomains,')||!result.output.includes('https://example.org/domains.yaml')||!result.output.includes('rule-providers:'))throw Error('Actual profile preview lost remote rule-set mapping');
  checks.push('actual UI creates HTTPS declaration; named selector, rename reference update and used-delete protection');checks.push('four-width declaration and reference screenshots; actual profile output includes rule-provider and ordered RULE-SET');
  fs.writeFileSync('docs/evidence/template-rulesets-ui.json',JSON.stringify({version:(await req('GET','/admin/overview')).version,date:new Date().toISOString(),checks,limitations:'public example URI is emitted only, never fetched by center; real client download separately recorded'},null,2));
 }finally{for(const [kind,id]of owned.reverse())await req('DELETE','/admin/resources/'+kind+'/'+id);await browser.close()}
 console.log('Actual remote rule-set editor, references, output and four-width interaction passed');
})().catch(async e=>{if(browser)await browser.close();console.error(e.message);process.exitCode=1});
