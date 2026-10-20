require('./production-guard.cjs');
const {chromium}=require('playwright');
const fs=require('fs'),path=require('path');
let browser,stage='launch';
(async()=>{
 const dir=path.join(process.env.LOCALAPPDATA,'MiGate/operations'),s=JSON.parse(fs.readFileSync(path.join(dir,'deployment.json'),'utf8')),fixture=JSON.parse(fs.readFileSync(path.join(dir,'outbound-transport-fixture.json'),'utf8'));
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});
 const c=await browser.newContext({viewport:{width:1440,height:1000}});
 await c.addCookies([{name:'migate_session',value:s.admin_cookie.slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',secure:true,httpOnly:true}]);
 const p=await c.newPage(),d=p.getByRole('dialog');
 async function choose(label,name){await d.getByRole('combobox',{name:label,exact:true}).click();await p.getByRole('option',{name,exact:true}).click();}
 try{
  stage='open';await p.goto(`https://sub.caowo.baby/devices/${s.device_id}?tab=outbounds`);
  if(!fixture.outbound_id)await p.getByRole('button',{name:'新增出站',exact:true}).click();
  else await p.locator('article.inventory-item').filter({has:p.getByRole('heading',{name:fixture.tag,exact:true})}).getByRole('button',{name:'编辑',exact:true}).click();
  if(fixture.mode==='vmess-ws'){
   await d.getByLabel('出站名称',{exact:true}).fill(fixture.tag);await choose('出站协议','VMess');
   await d.getByLabel('远端服务器',{exact:true}).fill('127.0.0.1');await d.getByLabel('远端端口',{exact:true}).fill('24446');await d.getByLabel('出站 UUID',{exact:true}).fill(fixture.uuid);
   await choose('出站传输方式','WebSocket');await d.getByLabel('出站 WebSocket 路径',{exact:true}).fill('/fixture');await d.getByLabel('出站 WebSocket Host',{exact:true}).fill('localhost');
  }else if(fixture.mode==='vmess-grpc'){
   if(await d.getByLabel('出站 WebSocket 路径',{exact:true}).inputValue()!=='/fixture')throw Error('Saved WS options missing');
   await choose('出站传输方式','gRPC');await d.getByLabel('出站 gRPC 服务名',{exact:true}).fill('fixture');
  }else if(fixture.mode==='https'){
   await choose('出站协议','HTTPS');if(!fixture.outbound_id){await d.getByLabel('出站名称',{exact:true}).fill(fixture.tag);await d.getByLabel('远端服务器',{exact:true}).fill('127.0.0.1');await d.getByLabel('远端端口',{exact:true}).fill('24446');}await d.getByLabel('出站用户名',{exact:true}).fill('fixture');await d.getByLabel('连接密码',{exact:true}).fill(fixture.password);await d.getByLabel('出站 TLS SNI',{exact:true}).fill('localhost');
  }else if(fixture.mode==='https-trusted'){
   if(await d.getByLabel('出站 TLS SNI',{exact:true}).inputValue()!=='localhost')throw Error('Saved TLS options missing');
   await d.getByLabel('出站信任证书（可选 PEM）',{exact:true}).fill(fixture.certificate);
  }else if(fixture.mode==='vless-reality'){
   await choose('出站协议','VLESS');await d.getByLabel('远端端口',{exact:true}).fill('24445');await d.getByLabel('出站 UUID',{exact:true}).fill(fixture.uuid);
   await choose('出站传输安全','Reality');await d.getByLabel('出站 TLS SNI',{exact:true}).fill(fixture.sni);await d.getByLabel('出站 Reality 公钥',{exact:true}).fill(fixture.pbk);await d.getByLabel('出站 Reality Short ID',{exact:true}).fill(fixture.sid);
   if(fixture.flow==='xtls-rprx-vision')await choose('出站 Flow','XTLS Vision');
  }else throw Error('Unknown fixture mode');
  stage='screenshots';
  for(const width of [1440,1280,768,390]){
   await p.setViewportSize({width,height:1000});await d.getByRole('button',{name:'核对配置',exact:true}).scrollIntoViewIfNeeded();
   const b=await d.boundingBox();if(!b||b.x<0||b.x+b.width>width+1||await d.evaluate(el=>el.scrollWidth>el.clientWidth+1))throw Error('Outbound form clipped');
   await p.screenshot({path:`docs/evidence/outbound-${fixture.mode}-${width}.png`,fullPage:false,mask:[d.locator('input[type=password]'),d.getByLabel('出站 UUID',{exact:true})]});
  }
  stage='submit';await d.getByRole('button',{name:'核对配置',exact:true}).click();
  const response=p.waitForResponse(r=>/\/devices\/\d+\/tasks$/.test(r.url())&&r.request().method()==='POST');await d.getByRole('button',{name:'确认下发任务',exact:true}).click();
  const r=await response;if(r.status()!==202)throw Error('Transport rejected');
  const result=await r.json();fixture.outbound_id=result.resource_id||fixture.outbound_id;fixture.task_id=result.id;
  fs.writeFileSync(path.join(dir,'outbound-transport-fixture.json'),JSON.stringify(fixture));
  await d.waitFor({state:'hidden'});
 }finally{await browser.close();}
 console.log('Actual outbound transport form queued; device receipt/client acceptance still required');
})().catch(async()=>{if(browser)await browser.close();console.error('Outbound form failed at stage: '+stage+'; private inputs omitted');process.exitCode=1;});
