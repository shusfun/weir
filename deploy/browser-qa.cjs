// Local-only browser checks. No production credentials or external services.
const { chromium } = require('playwright');
const fs = require('node:fs');
const path = require('node:path');
(async()=>{
 const docsOnly=process.argv.includes('--docs-only');
 const output=path.resolve('docs/evidence');fs.mkdirSync(output,{recursive:true});
 const browser=await chromium.launch({channel:'chrome',headless:true});
 const checks=[];
 for(const width of [1280,390]){
  const page=await browser.newPage({viewport:{width,height:900}});const errors=[];page.on('pageerror',e=>errors.push(String(e)));
  for(const doc of ['design','implementation','migration']){
   await page.goto('file:///'+path.resolve('docs/'+doc+'.html').replaceAll('\\','/'));
   const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth);
   if(overflow)throw Error(doc+' horizontal overflow at '+width);
   if(doc==='design'){
    const count=await page.locator('.check').count();if(count!==41)throw Error('expected 41 acceptance cases, got '+count);
    await page.locator('#search').fill('LIFE-');const visible=await page.locator('.check:visible').count();if(visible!==2)throw Error('acceptance filter failed');await page.locator('#search').fill('');
   }
   await page.screenshot({path:path.join(output,doc+'-'+width+'.png'),fullPage:true});checks.push({target:doc,width,overflow,pageErrors:[...errors]});
  }
  if(docsOnly){if(errors.length)throw Error(errors.join('\n'));await page.close();continue;}
  await page.goto('http://localhost:19810');await page.getByRole('heading',{name:'初始化你的控制中心'}).waitFor();
  await page.getByLabel('短时初始化凭证').fill('invalid-test-token');await page.getByRole('button',{name:'下一步',exact:true}).click();
  await page.getByRole('alert').filter({hasText:'初始化凭证无效或已过期'}).waitFor();
  const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth);if(overflow)throw Error('setup overflow');
  await page.screenshot({path:path.join(output,'setup-'+width+'.png'),fullPage:true});checks.push({target:'setup invalid token',width,overflow,pageErrors:[...errors]});
  if(errors.length)throw Error(errors.join('\n'));await page.close();
 }
 await browser.close();fs.writeFileSync(path.join(output,docsOnly?'browser-docs.json':'browser-qa.json'),JSON.stringify({date:new Date().toISOString(),checks},null,2));console.log(docsOnly?'Browser QA passed: desktop/mobile docs and filter':'Browser QA passed: desktop/mobile docs, filter, setup invalid-token rejection');
})().catch(e=>{console.error(e);process.exitCode=1});
