require('./production-guard.cjs');
const {chromium}=require('playwright');
const fs=require('fs'),path=require('path');
let browser,stage='launch';
(async()=>{
 const root=path.join(process.env.LOCALAPPDATA,'MiGate/operations'),file=path.join(root,'topology-fixture.json');
 const s=JSON.parse(fs.readFileSync(path.join(root,'deployment.json'),'utf8')),f=JSON.parse(fs.readFileSync(file,'utf8'));
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});
 const c=await browser.newContext({viewport:{width:1440,height:1100}});
 await c.addCookies([{name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);
 const p=await c.newPage();const errors=[];p.on('pageerror',()=>errors.push('runtime error'));
 const choose=async(label,name)=>{await p.getByRole('combobox',{name:label,exact:true}).click();await p.getByRole('option',{name,exact:true}).click();};
 if(f.mode==='create-route'){
  stage='create scoped route';await p.goto(`https://sub.caowo.baby/devices/${s.device_id}?tab=routing`);await p.getByRole('button',{name:'新增路由',exact:true}).click();const d=p.getByRole('dialog');
  await choose('源入站',f.inbound_name);await d.getByLabel('搜索授权用户',{exact:true}).fill(f.user_email);
  await choose('匹配客户端授权',`${f.user_email} · 授权 #${f.grant_id}`);await choose('目标出站',f.tag);
  // Changing inlet clears the previous grant. Restore the actual named inlet
  // and grant, with no resource ID entry needed.
  await choose('源入站','全部入站');if(!(await d.getByRole('combobox',{name:'匹配客户端授权',exact:true}).innerText()).includes('全部客户端'))throw Error('Changing inbound retained stale client');
  await choose('源入站',f.inbound_name);await choose('匹配客户端授权',`${f.user_email} · 授权 #${f.grant_id}`);
  for(const width of [1440,1280,768,390]){await p.setViewportSize({width,height:1100});await d.getByRole('button',{name:'核对配置',exact:true}).scrollIntoViewIfNeeded();await p.screenshot({path:`docs/evidence/routing-client-${width}.png`});}
  await d.getByRole('button',{name:'核对配置',exact:true}).click();const response=p.waitForResponse(r=>/\/devices\/\d+\/tasks$/.test(r.url())&&r.request().method()==='POST');await d.getByRole('button',{name:'确认下发任务',exact:true}).click();const r=await response;if(r.status()!==202)throw Error('Named client route rejected');const task=await r.json();f.route_id=task.resource_id;f.task_id=task.id;fs.writeFileSync(file,JSON.stringify(f));
 }else{
  stage='canvas';await p.goto(`https://sub.caowo.baby/devices/${s.device_id}?tab=topology`);await p.getByRole('heading',{name:'链路画布',exact:true}).waitFor();
  const canvas=p.getByLabel('Gate 配置关系画布',{exact:true}),node=canvas.locator(`.react-flow__node[data-id="client:${f.grant_id}"]`);
  stage='canvas selected client';await node.waitFor();stage='canvas selected edge';await canvas.locator(`.react-flow__edge[data-id^="rule-${f.route_id}-client-${f.grant_id}-"]`).waitFor();
  const other=canvas.locator(`.react-flow__edge[data-id^="rule-${f.route_id}-client-${f.other_grant_id}-"]`);if(await other.count())throw Error('Rule shown for wrong user');
  if(!(await node.innerText()).includes('未知'))throw Error('Missing traffic fabricated as zero');
  stage='canvas fold search';
  await canvas.locator('.react-flow__node[data-id^="client-summary:"]').waitFor();
  const search=p.getByLabel('搜索客户端授权',{exact:true});await search.fill(f.user_email);
  await node.waitFor();if(await canvas.locator('.react-flow__node[data-id^="client-summary:"]').count())throw Error('Search retained folded summary');
  await search.fill('');await canvas.locator('.react-flow__node[data-id^="client-summary:"]').waitFor();
  const expand=p.getByRole('switch',{name:'展开所有客户端',exact:true});await expand.click();
  await p.waitForTimeout(300);if(await canvas.locator('.react-flow__node[data-id^="client-summary:"]').count())throw Error('Expand retained summary');
  await expand.click();await canvas.locator('.react-flow__node[data-id^="client-summary:"]').waitFor();
  stage='canvas minimap';await canvas.locator('.react-flow__minimap rect.react-flow__minimap-node').first().waitFor();
  stage='canvas zoom';const viewport=canvas.locator('.react-flow__viewport');const before=await viewport.getAttribute('style');await p.getByRole('button',{name:'放大拓扑',exact:true}).click();await p.waitForTimeout(450);if(await viewport.getAttribute('style')===before)throw Error('Zoom did not change');
  await p.getByRole('button',{name:'缩小拓扑',exact:true}).click();await p.getByRole('button',{name:'适应画布',exact:true}).click();
  await choose('拓扑布局','从上向下');await choose('拓扑资源筛选','客户端授权');await choose('拓扑资源筛选','全部资源');
  for(const width of [1440,1280,768,390]){
   await p.setViewportSize({width,height:1100});await p.getByRole('button',{name:'适应画布',exact:true}).click();await canvas.scrollIntoViewIfNeeded();await p.waitForTimeout(300);
   if(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('Graph page overflow');
   const minimap=await canvas.locator('.react-flow__minimap').evaluate(e=>{const s=e.querySelector('svg');return [e.clientWidth,e.clientHeight,Number(s.getAttribute('width')),Number(s.getAttribute('height'))]});if(minimap[0]!==minimap[2]||minimap[1]!==minimap[3])throw Error('Minimap clipped by mismatched SVG dimensions');await p.screenshot({path:`docs/evidence/topology-canvas-${width}.png`,fullPage:true});
  }
  stage='keyboard node';await p.setViewportSize({width:1440,height:1100});await node.focus();await node.press('Enter');await p.getByRole('button',{name:'查看对应资源',exact:true}).waitFor();
  await p.getByRole('button',{name:'查看对应资源',exact:true}).click();await p.waitForURL(/tab=grants/);
  if(errors.length)throw Error('Graph runtime errors');
 }
 await browser.close();console.log('Real named-client form / configuration graph interaction passed; receipt and probes separate');
})().catch(async()=>{if(browser)await browser.close();console.error('Topology workflow failed at '+stage+'; private fixture omitted');process.exitCode=1;});
