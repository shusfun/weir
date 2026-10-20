// Required by real browser entrypoints before credentials or a browser are opened.
const cp = require('node:child_process'), path = require('node:path');
function check(envelope) {
  const r = cp.spawnSync('python', [path.join(__dirname, 'production_guard.py'), ...(envelope?['--request']:[])], { windowsHide: true, stdio: 'pipe', input:envelope?JSON.stringify(envelope):undefined });
  if (r.status !== 0) throw Error('Production lease unavailable; run through deploy/production-run.py');
}
if (process.env.MIGATE_LOCAL_BROWSER) {
  if (!['browser-guestbook.cjs','browser-subscription-naming.cjs','browser-outbound-pools.cjs','browser-protocols.cjs'].includes(path.basename(process.argv[1]||''))) throw Error('Workflow has no isolated fixture adapter');
  require('./browser-fixture.cjs').fixture();
} else {
  check();
  const {chromium}=require('playwright'), original=chromium.launch.bind(chromium);
  chromium.launch=async(...args)=>{
    const browser=await original(...args), newContext=browser.newContext.bind(browser);
    browser.newContext=async(...options)=>{
      const context=await newContext(...options);
      await context.route('**/api/v1/**',async route=>{
        const request=route.request(), url=new URL(request.url());
        try {
          let data;try{data=request.postDataJSON()}catch{}
          check({method:request.method(),path:url.pathname+url.search,data});
          await route.continue();
        }catch{
          await route.abort('blockedbyclient');
        }
      });
      return context;
    };
    return browser;
  };
  // A killed runner closes the broker; an orphaned browser must stop too.
  const timer = setInterval(() => { try { check(); } catch { process.exit(75); } }, 2000);
  timer.unref();
}
module.exports = { check };
