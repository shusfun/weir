// Real disposable app/PG/Redis: runs complete affected workflows before VPS work.
const fs=require('node:fs'), os=require('node:os'), path=require('node:path'), cp=require('node:child_process'), crypto=require('node:crypto');
let server, scratch, owned=false;
const root=path.resolve(__dirname,'..'), origin='http://localhost:19812';
function run(binary,args,options={}) {const r=cp.spawnSync(binary,args,{cwd:root,encoding:'utf8',windowsHide:true,...options});if(r.status!==0)throw Error(binary+' '+args.join(' ')+' failed: '+[r.error?.message,r.stdout,r.stderr].filter(Boolean).join('\n').slice(-6000));return r.stdout;}
const compose=(...args)=>run('docker',['compose','-f','deploy/test.compose.yml',...args]);
async function request(method,url,data,cookie='') {const r=await fetch(origin+'/api/v1'+url,{method,headers:{'Content-Type':'application/json','Origin':origin,'Cookie':cookie},body:data===undefined?undefined:JSON.stringify(data)});if(!r.ok)throw Error('Local fixture HTTP '+r.status+' '+url);return {data:await r.json(),cookie:r.headers.get('set-cookie')?.split(';')[0]};}
(async()=>{
  if(run('docker',['ps','-aq','--filter','label=com.docker.compose.project=migate-local-test']).trim())throw Error('Dedicated test stack in use; refusing to reset it');
  const fingerprint=run('python',['deploy/source_fingerprint.py']).trim();
  const batch=process.argv[2]||'B0', definition=JSON.parse(fs.readFileSync('deploy/local-workflows.json','utf8'))[batch];
  if(!definition)throw Error('Batch has no complete local workflow definition');
  fs.mkdirSync('docs/evidence/local',{recursive:true});
  run('cmd',['/d','/s','/c','pnpm check && pnpm build']);
  run('go',['test','./internal/control','./internal/agent','./internal/db','./internal/subscription','./internal/lifecycle','./services/subscriptions']);
  compose('up','-d','--wait');owned=true;
  run('go',['test','./internal/control','-run','^TestIntegration$','-count=1'],{env:{...process.env,MIGATE_INTEGRATION:'1'}});
  // Integration owns its schema; start a genuinely fresh installation afterwards.
  compose('down');compose('up','-d','--wait');
  const exe=path.resolve('dist/migate-local-gate.exe');
  run('go',['build','-o',exe,'./apps/control']);
  scratch=fs.mkdtempSync(path.join(os.tmpdir(),'migate-local-gate-'));
  const args=['--config',path.join(scratch,'config.json'),'--listen','127.0.0.1:19812','--public-url',origin];
  const token=run(exe,[...args,'--init']).trim().split(/\s+/).at(-1);
  server=cp.spawn(exe,args,{windowsHide:true,stdio:'ignore'});
  let ready=false;for(let n=0;n<100;n++){try{if((await fetch(origin+'/healthz')).ok){ready=true;break;}}catch{}await new Promise(r=>setTimeout(r,100));}if(!ready)throw Error('Local control not ready');
  await request('POST','/setup/verify',undefined).then(()=>{throw Error('Unauthenticated setup passed');},()=>{});
  const auth={'Authorization':'Bearer '+token,'Content-Type':'application/json','Origin':origin};
  const install=await fetch(origin+'/api/v1/setup/install',{method:'POST',headers:auth,body:JSON.stringify({database:{host:'127.0.0.1',port:25432,user:'gate',password:'local-test-only',database:'gate',ssl_mode:'disable'},redis:{address:'127.0.0.1:26379',username:'migate',password:'local-test-only'},email:'admin@isolated.test',password:'isolated-browser-password'})});if(install.status!==201)throw Error('Local actual initialization failed');
  const admin=(await request('POST','/auth/login',{email:'admin@isolated.test',password:'isolated-browser-password'})).cookie;
  const account={email:'user@isolated.test',password:'isolated-user-password'};
  await request('POST','/admin/users',account,admin);
  const user=(await request('POST','/auth/login',account)).cookie;
  const fixture={target:'disposable-local',origin,configPath:path.join(scratch,'config.json'),s:{admin_cookie:admin,admin_email:'admin@isolated.test',admin_password:'isolated-browser-password',test_users:[account]},sessions:{[account.email]:user}};
  const file=path.join(scratch,'browser.json');fs.writeFileSync(file,JSON.stringify(fixture),{mode:0o600});
  const workflows=['browser-auth-layout.cjs', ...definition.workflows];
  for(const workflow of workflows){console.log('Local full workflow: '+workflow);run('node',['deploy/'+workflow],{env:{...process.env,MIGATE_LOCAL_BROWSER:'1',MIGATE_LOCAL_FIXTURE:file,MIGATE_LAYOUT_ORIGIN:origin}});}
  const files=fs.readdirSync('docs/evidence/local').filter(x=>x.endsWith('.png')&&(batch!=='B1'||/^(pool|protocol)-/.test(x))).sort();
  const report={date:new Date().toISOString(),batch,target:'disposable-local control + PG18 + Redis8',result:'passed',source_fingerprint:fingerprint,
    workflows,checks:['TypeScript and production frontend build','Go affected packages','PG/Redis integration','actual initialization and ordinary account','complete real UI workflows, filters/cache invalidation, submitted forms, Chinese selector labels, failures and ownership'],
    screenshots:[...files.map(name=>'docs/evidence/local/'+name),...fs.readdirSync('docs/evidence/auth-layout-local').filter(name=>name.endsWith('.png')&&!name.startsWith('before-')).map(name=>'docs/evidence/auth-layout-local/'+name)].map(file=>({path:file,sha256:crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex')})),visual:'Human review required; not passed by this script'};
  fs.writeFileSync('docs/evidence/local-gate.json',JSON.stringify(report,null,2));
  console.log('Local interaction gate passed; screenshot review still required before deployment');
})().catch(e=>{console.error(e.message);fs.writeFileSync('docs/evidence/local-gate.json',JSON.stringify({date:new Date().toISOString(),result:'failed',detail:e.message},null,2));process.exitCode=1;}).finally(async()=>{
  if(server){server.kill();await new Promise(r=>setTimeout(r,500));}
  if(owned)compose('down');
  if(scratch&&path.dirname(scratch)===os.tmpdir()&&path.basename(scratch).startsWith('migate-local-gate-'))fs.rmSync(scratch,{recursive:true});
});
