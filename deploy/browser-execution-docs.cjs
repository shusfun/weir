// File-only HTML verification; never opens an application or production origin.
const {chromium}=require('playwright'), fs=require('node:fs'), path=require('node:path');
const {pathToFileURL}=require('node:url');
(async()=>{
 const browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server']});
 const checks=[],shots=[],errors=[];
 try{
  const page=await browser.newPage();
  page.on('pageerror',error=>errors.push(error.message));
  for(const name of ['batches','implementation','status']){
   await page.goto(pathToFileURL(path.resolve(`docs/${name}.html`)).href+(name==='implementation'?'#execution-tools':''));
   for(const width of [1440,1280,768,390]){
    await page.setViewportSize({width,height:1000});
    if(name==='implementation')await page.locator('#execution-tools').scrollIntoViewIfNeeded();
    if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error(`Document overflow: ${name}/${width}`);
    const file=`docs/evidence/execution-verified-${name}-${width}.png`;
    await page.screenshot({path:file});shots.push(file);checks.push(`${name} ${width}`);
   }
   if(name==='status'){
    const rows=page.locator('#evidence tbody tr'), count=await rows.count();
    await page.locator('#filter').fill('no-such-evidence-match');
    if(await rows.filter({visible:true}).count())throw Error('Status filter did not hide unmatched rows');
    await page.locator('#filter').fill('');
    if(await rows.filter({visible:true}).count()!==count)throw Error('Clearing filter did not restore records');
    checks.push('generated status filter and clear restore all records');
   }
  }
  if(errors.length)throw Error('Document script error');
  fs.writeFileSync('docs/evidence/execution-docs-browser.json',JSON.stringify({date:new Date().toISOString(),result:'passed',checks,screenshots:shots,visual:'captured; human review pending'},null,2));
  console.log('Three execution HTML documents: four widths and status filter passed');
 }finally{await browser.close()}
})().catch(error=>{console.error(error.message);process.exitCode=1});
