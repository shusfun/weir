const fs = require('node:fs'), path = require('node:path');
function fixture() {
  if (process.env.MIGATE_LOCAL_BROWSER) {
    const file = process.env.MIGATE_LOCAL_FIXTURE;
    if (!file) throw Error('Local fixture is required');
    const f = JSON.parse(fs.readFileSync(file, 'utf8'));
    if (f.origin !== 'http://localhost:19812' || f.target !== 'disposable-local') throw Error('Invalid isolated origin');
    return {...f, args:['--no-proxy-server'], cookie:{domain:'localhost',path:'/',httpOnly:true,secure:false}, evidence:'docs/evidence/local/'};
  }
  const root = path.join(process.env.LOCALAPPDATA, 'MiGate/operations');
  return {s:JSON.parse(fs.readFileSync(path.join(root,'deployment.json'),'utf8')),
    sessions:JSON.parse(fs.readFileSync(path.join(root,'acceptance-sessions.json'),'utf8')),
    origin:'https://sub.caowo.baby', args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443'],
    cookie:{domain:'sub.caowo.baby',path:'/',httpOnly:true,secure:true}, evidence:'docs/evidence/'};
}
module.exports={fixture};
