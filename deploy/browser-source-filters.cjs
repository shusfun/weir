require('./production-guard.cjs');
const {chromium}=require('playwright');
const fs=require('fs'),path=require('path');
let browser;
(async()=>{
 const id=Number(process.argv[2]),group=Number(process.argv[3]);
 if(!Number.isInteger(id)||id<=0||!Number.isInteger(group)||group<=0)throw Error('Owned fixture required');
 const state=JSON.parse(fs.readFileSync(path.join(process.env.LOCALAPPDATA,'MiGate/operations/deployment.json'),'utf8'));
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});
 const c=await browser.newContext({viewport:{width:1440,height:1000}});
 await c.addCookies([{name:'migate_session',value:state.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);
 const p=await c.newPage();
 async function api(endpoint,data){return p.evaluate(async({endpoint,data})=>{const r=await fetch('/api/v1'+endpoint,{method:data?'POST':'GET',headers:{'Content-Type':'application/json'},body:data?JSON.stringify(data):undefined});if(!r.ok)throw Error('API rejected');return r.json();},{endpoint,data});}
 async function source(){return (await api('/admin/resources/sources')).items.find(x=>x.id===id)?.data;}
 const row=p.locator('article.inventory-item').filter({has:p.getByRole('heading',{name:'Acceptance source fixture',exact:true})}),d=p.getByRole('dialog');
 async function select(label,value){await d.getByRole('combobox',{name:label,exact:true}).click();await p.getByRole('option',{name:value,exact:true}).click();}
 async function save(expected){const pending=p.waitForResponse(r=>r.url().endsWith('/api/v1/admin/resources/sources')&&r.request().method()==='POST');await d.getByRole('button',{name:'保存',exact:true}).click();if((await pending).status()!==expected)throw Error('Unexpected source save result');if(expected===200)await d.waitFor({state:'hidden'});}
 try{
  await p.goto('https://sub.caowo.baby/subscriptions?kind=sources');
  const original=await source();if(original?.name!=='Acceptance source fixture'||original.node_count!==4)throw Error('Wrong fixture');
  await row.getByRole('button',{name:'编辑',exact:true}).click();
  await select('筛选动作','仅保留匹配节点');await select('节点协议','trojan');
  await d.getByRole('button',{name:'添加协议规则',exact:true}).click();
  await d.getByLabel('节点名称关键词',{exact:true}).fill('香港');await d.getByRole('button',{name:'添加关键词',exact:true}).click();
  await select('筛选动作','排除匹配节点');await d.getByLabel('节点名称关键词',{exact:true}).fill('Expire');await d.getByRole('button',{name:'添加关键词',exact:true}).click();
  if(!(await d.getByRole('combobox',{name:'筛选动作',exact:true}).innerText()).includes('排除匹配节点'))throw Error('Internal enum shown instead of action label');
  for(const width of [1440,1280,768,390]){
   await p.setViewportSize({width,height:1000});await d.getByRole('button',{name:'添加协议规则',exact:true}).scrollIntoViewIfNeeded();
   const b=await d.boundingBox();if(!b||b.x<0||b.x+b.width>width+1||await d.evaluate(el=>el.scrollWidth>el.clientWidth+1))throw Error('Filter editor clipped');
   await p.screenshot({path:`docs/evidence/source-filters-${width}.png`,mask:[p.locator('.sensitive-output')],fullPage:false});
  }
  await save(200);
  let current=await source();if(current.last_update!==original.last_update||current.node_count!==4)throw Error('Read-time filter erased raw cache');
  let preview=await api('/admin/profiles/preview',{id:group,target:'singbox'}),outs=JSON.parse(preview.output).outbounds.filter(x=>x.server);
  if(outs.length!==1||outs[0].tag!=='香港-A'||outs[0].password!=='synthetic')throw Error('Visual filter result incorrect');
  await row.getByRole('button',{name:'编辑',exact:true}).click();
  await d.getByRole('button',{name:'删除排除规则 1',exact:true}).click();await save(200);
  preview=await api('/admin/profiles/preview',{id:group,target:'links'});if(preview.node_count!==2)throw Error('Remove rule did not restore cached nodes');
  await row.getByRole('button',{name:'编辑',exact:true}).click();await d.getByRole('button',{name:'高级规则',exact:true}).click();
  await d.getByLabel('源排除规则',{exact:true}).fill('[');await save(400);
  if((await source()).filter_exclude)throw Error('Invalid rule changed saved source');
  await d.getByLabel('源排除规则',{exact:true}).fill('Expire');await save(200);
  preview=await api('/admin/profiles/preview',{id:group,target:'links'});if(preview.node_count!==1)throw Error('Recovery result wrong');
  fs.writeFileSync('docs/evidence/source-filters-ui.json',JSON.stringify({version:(await api('/admin/overview')).version,date:new Date().toISOString(),checks:['visual protocol/name include OR then exclusion produces exact source group','remove exclusion restores cached nodes without fetching','invalid regex rejected, previous saved config retained; UI correction saves','four width actual business-form screenshots captured'],visual:'manual review pending'},null,2));
 }finally{await browser.close();}
 console.log('Actual source filter form, cached group output, remove, rejection and recovery passed');
})().catch(async()=>{if(browser)await browser.close();console.error('Source filter workflow failed; private request inputs omitted');process.exitCode=1;});
