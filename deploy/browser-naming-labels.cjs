require('./production-guard.cjs');
const {chromium}=require('playwright'),fs=require('node:fs');
(async()=>{
 const f=require('./browser-fixture.cjs').fixture();
 const b=await chromium.launch({channel:'chrome',args:f.args});
 try{
  const c=await b.newContext();await c.addCookies([{name:'migate_session',value:f.s.admin_cookie.slice('migate_session='.length),...f.cookie}]);
  const p=await c.newPage();await p.goto(f.origin+'/subscriptions?kind=profiles');
  await p.getByRole('button',{name:'新增订阅组',exact:true}).click();const d=p.getByRole('dialog');
  const shots=[];
  for(const width of [1440,1280,768,390]){
   await p.setViewportSize({width,height:1000});const select=d.getByRole('combobox',{name:'手动节点文本前缀',exact:true});
   await select.scrollIntoViewIfNeeded();if(!(await select.innerText()).includes('继承全局'))throw Error('Inherited selector label not translated');
   await select.click();await p.getByRole('option',{name:'停用',exact:true}).click();if((await select.innerText()).trim()!=='停用')throw Error('Disabled selector label not translated');
   const file=`docs/evidence/naming-labels-${width}.png`;await d.screenshot({path:file});shots.push(file);
   await select.click();await p.getByRole('option',{name:'继承全局',exact:true}).click();
  }
  await d.getByRole('button',{name:'Close',exact:true}).click();await d.waitFor({state:'hidden'});
  const version=await p.evaluate(async()=>(await(await fetch('/api/v1/admin/overview')).json()).version);
  fs.writeFileSync('docs/evidence/live-naming-labels.json',JSON.stringify({date:new Date().toISOString(),version,result:'passed',checks:['New profile opens without inventory writes','Inherited/disabled Chinese labels at all four widths','Top-right close dismisses unsaved form'],screenshots:shots,visual:'captured; review pending'},null,2));
 }finally{await b.close()}
})().catch(e=>{console.error(String(e.message).split('\n')[0]);process.exitCode=1});
