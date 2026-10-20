require('./production-guard.cjs');
// Owned live acceptance fixture; secrets are handed off locally and never captured.
const {chromium}=require('playwright');
const fs=require('fs'),path=require('path');
let browser,stage='launch';
(async()=>{
 const root=path.join(process.env.LOCALAPPDATA,'MiGate/operations');
 const fixture=JSON.parse(fs.readFileSync(path.join(root,'announcement-fixture.json'),'utf8'));
 const state=JSON.parse(fs.readFileSync(path.join(root,'deployment.json'),'utf8'));
 const mode=process.argv[2];
 browser=await chromium.launch({channel:'chrome',args:['--no-proxy-server','--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443']});
 const context=await browser.newContext({viewport:{width:1440,height:1000}});
 if(mode!=='public')await context.addCookies([{name:'migate_session',value:(mode==='save'?state.admin_cookie:fixture.user_cookie).slice('migate_session='.length),domain:'sub.caowo.baby',path:'/',httpOnly:true,secure:true}]);
 const page=await context.newPage(),errors=[];page.on('pageerror',()=>errors.push('pageerror'));
 async function shots(label,locator){for(const width of [1440,1280,768,390]){await page.setViewportSize({width,height:1000});await locator.scrollIntoViewIfNeeded();if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('Horizontal overflow');await page.screenshot({path:`docs/evidence/global-announcement-${label}-${width}.png`,fullPage:true});}}
 if(mode==='save'){
  stage='editor';await page.goto('https://sub.caowo.baby/settings?tab=announcement');
  const editor=page.locator('.announcement-editor'),a=fixture.announcement;
  await editor.getByRole('combobox',{name:'全局公告内容格式',exact:true}).click();await page.getByRole('option',{name:a.content_format==='html'?'受限富文本':'纯文本',exact:true}).click();
  await editor.getByLabel('全局公告标题',{exact:true}).fill(a.title);await editor.getByLabel('全局公告正文（纯文本）',{exact:true}).fill(a.content);
  await editor.getByRole('combobox',{name:'全局公告类型',exact:true}).click();await page.getByRole('option',{name:({info:'通知',success:'成功',warning:'提醒',error:'重要'})[a.type],exact:true}).click();
  for(const [label,key]of [['发布全局公告','enabled'],['向登录用户显示','authenticated'],['向匿名公开页显示','public'],['允许关闭全局公告','dismissible']]){const control=editor.getByRole('switch',{name:label,exact:true});if((await control.getAttribute('aria-checked')==='true')!==a[key])await control.click();}
  if(a.content_format==='html'){const content=editor.getByLabel('全局公告正文（纯文本）',{exact:true});await content.fill('<p onclick=alert(1)>reject</p>');await editor.getByText('富文本包含不支持的标签、属性或链接。请修正后再发布。',{exact:true}).waitFor();if(await editor.getByRole('button',{name:'核对并保存公告',exact:true}).isEnabled())throw Error('Invalid markup publish enabled');await content.fill(a.content);await editor.locator('.announcement-preview strong').waitFor();}
  if(a.content)await editor.locator('.announcement-preview').getByText(a.title,{exact:true}).waitFor();
  await shots('editor',editor.getByRole('button',{name:'核对并保存公告',exact:true}));
  await editor.getByRole('button',{name:'核对并保存公告',exact:true}).click();
  const dialog=page.getByRole('dialog');await dialog.getByLabel('管理员当前密码',{exact:true}).fill(fixture.password);await dialog.getByLabel('二次验证码或恢复码',{exact:true}).fill(fixture.code);
  stage='save';const saved=page.waitForResponse(r=>r.url().endsWith('/api/v1/admin/announcement')&&r.request().method()==='PUT');await dialog.getByRole('button',{name:'确认保存公告',exact:true}).click();if((await saved).status()!==200)throw Error('Announcement UI publish failed');await dialog.waitFor({state:'hidden'});
 }else{
  stage=mode+' announcement';await page.goto('https://sub.caowo.baby'+(mode==='public'?'/p/'+fixture.slug:'/security'));
  const card=page.locator('.global-announcement .published-announcement');await card.getByRole('heading',{name:fixture.announcement.title,exact:true}).waitFor();
  if(fixture.announcement.content_format==='html'){await card.locator('strong').getByText('维护窗口',{exact:true}).waitFor();const link=card.getByRole('link',{name:'查看指南',exact:true});if(await link.getAttribute('href')!=='https://example.com/help'||await link.getAttribute('rel')!=='noopener noreferrer'||await link.getAttribute('target')!=='_blank')throw Error('Unsafe rich link');if(await card.locator('script,img,[onclick],[style]').count())throw Error('Unsafe rich content');}
  if(await page.evaluate(()=>window.__globalXSS))throw Error('Plaintext executed');
  if(mode==='user'){await page.getByRole('button',{name:'修改密码',exact:true}).waitFor();await page.getByRole('button',{name:'刷新会话',exact:true}).waitFor();await page.locator('.security-page [role=status]').waitFor({state:'hidden'});}
  await shots(mode,card);
  await card.getByRole('button',{name:'关闭公告',exact:true}).focus();await page.keyboard.press('Enter');await card.waitFor({state:'hidden'});await page.reload();await page.waitForTimeout(500);if(await card.count())throw Error('Dismissal not retained');
  await page.evaluate(()=>{for(const key of Object.keys(localStorage))if(key.startsWith('migate:notice:global-'))localStorage.removeItem(key)});await page.reload();await card.waitFor();
  stage=mode+' failure';const endpoint=mode==='public'?'/api/v1/announcements/public':'/api/v1/announcements';await page.route('**'+endpoint,route=>route.fulfill({status:503,contentType:'application/json',body:'{"error":"announcement_unavailable"}'}));await page.reload();await page.getByText('服务公告暂不可用。',{exact:false}).waitFor();if(await card.count())throw Error('Stale announcement displayed');await page.setViewportSize({width:390,height:1000});await page.getByRole('button',{name:'重试公告',exact:true}).scrollIntoViewIfNeeded();await page.screenshot({path:`docs/evidence/global-announcement-${mode}-failure-390.png`});await page.unroute('**'+endpoint);await page.getByRole('button',{name:'重试公告',exact:true}).click();await card.waitFor();
 }
 if(errors.length)throw Error('Browser exception');await browser.close();browser=null;console.log('Announcement browser acceptance passed: '+mode);
})().catch(async()=>{console.error('Announcement browser failed at '+stage);if(browser)await browser.close();process.exitCode=1});
