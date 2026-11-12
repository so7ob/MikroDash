// Actual protected HTTP/SQLite and browser form submission; synthetic NAS verdicts only.
import { createRequire } from 'node:module';
import assert from 'node:assert/strict';
const require=createRequire(process.env.SUBSCRIBER_BROWSER_MODULE+'/package.json');
const {chromium}=require('playwright');
const browser=await chromium.launch({headless:true,...(process.env.SUBSCRIBER_CHROMIUM?{executablePath:process.env.SUBSCRIBER_CHROMIUM}:{})});
try {
 const context=await browser.newContext({ignoreHTTPSErrors:true}), page=await context.newPage();
 page.setDefaultTimeout(12000);
 const origin=process.env.SUBSCRIBER_TEST_URL,hotspot=process.env.SUBSCRIBER_TEST_HOTSPOT,denied=process.env.SUBSCRIBER_TEST_DENIED==='true';
 const counts={login:0,connect:0,hotspot:0,disconnect:0};
 page.on('request',r=>{if(r.method()!=='POST')return;const u=new URL(r.url());if(u.origin===hotspot)counts.hotspot++;else if(u.pathname==='/api/login')counts.login++;else if(u.pathname==='/api/connect')counts.connect++;else if(u.pathname==='/api/disconnect')counts.disconnect++;});
 await page.goto(origin+'/?connect=C');await page.locator('#accountMode').click();
 await page.locator('#subscriberUsername').fill('synthetic-account');await page.locator('#subscriberPassword').fill('synthetic-password');await page.locator('#subscriberPassword').press('Enter');
 await page.locator('#internetState').filter({hasText:denied?'رُفض الاتصال بالإنترنت':'متصل بالإنترنت'}).waitFor();
 assert.equal(new URL(page.url()).origin,origin);assert.equal(new URL(page.url()).search,'');
 assert.deepEqual(counts,{login:1,connect:1,hotspot:1,disconnect:0});
 assert.equal((await context.request.get(origin+'/api/routers')).status(),404);
 if(denied) {
  assert.match(await page.locator('#internetDetail').innerText(),/نفد الرصيد: باقة تجريبية/);
  assert.equal(await page.locator('#portalAccount').isVisible(),true);
 } else {
  assert.equal(await page.locator('#internetConnect').isDisabled(),true);
  const before=await(await context.request.get(origin+'/api/account')).json();assert.equal(before.sessions[0].state,'active');
  page.once('dialog',d=>d.dismiss());await page.getByRole('button',{name:'فصل الجهاز',exact:true}).click();assert.equal(counts.disconnect,0);
  page.once('dialog',d=>d.accept());const response=page.waitForResponse(r=>r.url().endsWith('/api/disconnect'));await page.getByRole('button',{name:'فصل الجهاز',exact:true}).click();
  assert.equal((await response).status(),400);assert.equal(counts.disconnect,1,'device control did not call the guarded API');
  const after=await(await context.request.get(origin+'/api/account')).json();assert.equal(after.sessions[0].id,before.sessions[0].id);assert.equal(after.sessions[0].state,'active','UI bypassed disabled disconnect capability');
  await page.locator('#internetState').filter({hasText:'الاتصال غير مؤكد'}).waitFor();
  assert.equal(await page.locator('#internetConnect').isDisabled(),false,'stale observation claimed connected');
 }
 await page.goto(origin+'/?connect=C');await page.locator('#portalAccount').waitFor({state:'visible'});await page.locator('#connectionState').filter({hasText:'تحديث مباشر'}).waitFor();
 assert.equal(counts.connect,1,'a saved portal cookie silently opened another Internet session');
 await page.locator('#portalLogout').click();await page.locator('#portalLogin').waitFor({state:'visible'});assert.equal((await context.request.get(origin+'/api/account')).status(),401);
 console.log(`Synthetic NAS ${denied?'denial':'accepted/stale'} passed: single credential, owned HTTPS ticket POST/return, no automatic cookie admission, guarded disconnect and independent portal logout.`);
}finally{await browser.close();}
