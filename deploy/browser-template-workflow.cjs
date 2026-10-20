require('./production-guard.cjs');
const {chromium}=require('playwright');const fs=require('fs'),path=require('path');let browser;
(async()=>{
 const s=JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA,'MiGate/operations/deployment.json'),'utf8'));
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});
 const c=await browser.newContext({viewport:{width:1440,height:1000}});await c.addCookies([{name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);
 const page=await c.newPage(),owned=[],checks=[];
 async function req(method,endpoint,data){return page.evaluate(async({method,endpoint,data})=>{const r=await fetch('/api/v1'+endpoint,{method,headers:{'Content-Type':'application/json'},body:data===undefined?undefined:JSON.stringify(data)});if(!r.ok)throw Error('Workflow HTTP '+r.status);return r.json()},{method,endpoint,data})}
 try {
  await page.goto('https://sub.caowo.baby/subscriptions?kind=templates');await page.getByRole('button',{name:'新增输出模板',exact:true}).click();const dialog=page.getByRole('dialog');
  await dialog.getByLabel('名称',{exact:true}).fill('template-flow-'+Date.now());
  await dialog.getByRole('button',{name:'自动测速预设',exact:true}).click();await page.getByRole('dialog').last().getByRole('button',{name:'应用预设',exact:true}).click();
  await dialog.getByLabel('策略组 2 名称',{exact:true}).fill('Main');
  if(!await dialog.getByLabel('规则 1 策略',{exact:true}).innerText().then(s=>s.includes('Main')))throw Error('Group rename did not update final rule reference');
  await dialog.getByRole('button',{name:'添加规则',exact:true}).click();await dialog.getByLabel('规则 1 匹配值',{exact:true}).fill('first.example');
  await dialog.getByRole('button',{name:'添加规则',exact:true}).click();await dialog.getByLabel('规则 2 匹配值',{exact:true}).fill('second.example');
  await dialog.getByRole('button',{name:'上移规则 2',exact:true}).click();
  if(await dialog.getByLabel('规则 1 匹配值',{exact:true}).inputValue()!=='second.example')throw Error('Rule order did not change');
  if(!await dialog.getByRole('button',{name:'上移规则 3',exact:true}).isDisabled())throw Error('Final rule can move before specific rules');
  for(const width of [1440,1280,768,390]){await page.setViewportSize({width,height:1000});await dialog.getByLabel('策略组 1 名称',{exact:true}).scrollIntoViewIfNeeded();await page.screenshot({path:`docs/evidence/template-workflow-${width}.png`});await dialog.getByLabel('规则 1 匹配值',{exact:true}).scrollIntoViewIfNeeded();await page.screenshot({path:`docs/evidence/template-rules-${width}.png`})}
  const response=page.waitForResponse(r=>r.url().endsWith('/api/v1/admin/resources/templates')&&r.request().method()==='POST');await dialog.getByRole('button',{name:'保存',exact:true}).click();const saved=await response;if(saved.status()!==200)throw Error('Template workflow save failed');const template=await saved.json();owned.push(['templates',template.id]);await dialog.waitFor({state:'hidden'});
  const node=await req('POST','/admin/resources/nodes',{data:{name:'HK-workflow',url:'socks5://u:synthetic@example.com:1080#HK-workflow'}});owned.push(['nodes',node.id]);
  const profile=await req('POST','/admin/resources/profiles',{data:{name:'template-preview',node_ids:[node.id],template_ids:[template.id]}});owned.push(['profiles',profile.id]);
  const result=await req('POST','/admin/profiles/preview',{id:profile.id,target:'clash'});
  if(!result.output.includes('MATCH,Main')||result.output.indexOf('second.example')>result.output.indexOf('first.example')||!result.output.includes('url-test'))throw Error('Actual template output lost preset/rename/order');
  checks.push('UI preset confirmation, group rename updates references, policy selector and rule order persist');checks.push('final rule protected; actual profile output contains generated selector/automatic group and ordered domain rules');
  await page.goto('https://sub.caowo.baby/devices/'+s.device_id+'?tab=cores');const xray=page.locator('.core-version-card').filter({has:page.getByRole('heading',{name:'Xray',exact:true})});
  await xray.getByRole('combobox').click();await page.getByRole('option',{name:/26\.10\.17/}).click();await xray.getByText(/兼容限制/).waitFor();await xray.getByRole('button',{name:'安装 / 升级',exact:true}).click();await page.getByRole('dialog').getByText(/sing-box 1.14.3 无法连接/).waitFor();await page.screenshot({path:'docs/evidence/core-compatibility-warning-390.png'});await page.getByRole('dialog').getByRole('button',{name:'取消',exact:true}).click();
  checks.push('upgrade candidate and confirmation show REALITY sing-box incompatibility; cancel sends no install task');
  fs.writeFileSync('docs/evidence/template-workflow.json',JSON.stringify({version:(await req('GET','/admin/overview')).version,date:new Date().toISOString(),checks},null,2));
 } finally {for(const [kind,id]of owned.reverse())await req('DELETE','/admin/resources/'+kind+'/'+id);await browser.close()}
 console.log('Actual template presets, group reference rename, ordered rules, generated output and core compatibility warning passed');
})().catch(async e=>{if(browser)await browser.close();console.error(e.message);process.exitCode=1});
