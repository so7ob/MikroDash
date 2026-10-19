// Real protected HTTP/SQLite acceptance on a synthetic local TLS fixture.
import {createRequire} from 'node:module';
import {mkdirSync} from 'node:fs';
import assert from 'node:assert/strict';
const require=createRequire(process.env.SUBSCRIBER_BROWSER_MODULE+'/package.json');
const {chromium}=require('playwright');
const {default:AxeBuilder}=require('@axe-core/playwright');
const out=process.env.SUBSCRIBER_EVIDENCE_DIR;
mkdirSync(out,{recursive:true});
const browser=await chromium.launch({headless:true,...(process.env.SUBSCRIBER_CHROMIUM?{executablePath:process.env.SUBSCRIBER_CHROMIUM}:{})});
try {
 const context=await browser.newContext({ignoreHTTPSErrors:true}); // Synthetic httptest identity only.
 const origin=process.env.SUBSCRIBER_TEST_URL;
 await context.addCookies([{name:'mikrodash_sid',value:'synthetic-token',url:origin,httpOnly:true,secure:true,sameSite:'Lax'}]);
 const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(origin);
 await page.getByText('Unicode fixture: اختبار',{exact:true}).waitFor();
 await page.getByRole('tab',{name:'Subscribers',exact:true}).focus();
 await page.keyboard.press('End');
 assert.equal(await page.getByRole('tab',{name:'Reconciliation'}).getAttribute('aria-selected'),'true');
 const account=page.locator('#subReviewAccount');
 await account.selectOption(process.env.SUBSCRIBER_ACCOUNT_ID);
 // Delay the genuine first API response, change accounts, then release it.
 let release;const gate=new Promise(resolve=>{release=resolve;});
 let fetched;const ready=new Promise(resolve=>{fetched=resolve;});
 await page.route('**/api/subscribers/reconciliation?account=*',async route=>{const response=await route.fetch();fetched();await gate;await route.fulfill({response});},{times:1});
 await page.getByRole('button',{name:'Load reconciliation'}).click();await ready;
 await account.selectOption({label:'synthetic-account-two'});release();
 await page.waitForLoadState('networkidle');
 assert.equal(await page.locator('form[data-review]').count(),0,'late account response leaked old records');
 await page.getByRole('button',{name:'Load reconciliation'}).click();
 await page.getByText('No reconciliation records for this subscriber.',{exact:true}).waitFor();
 await account.selectOption(process.env.SUBSCRIBER_ACCOUNT_ID);
 await page.getByRole('button',{name:'Load reconciliation'}).click();
 const form=page.locator('form[data-review="byte-gap"]');await form.waitFor();
 const periods=form.locator('[data-reconcile-period]');assert.equal(await periods.count(),2);
 await periods.nth(0).fill('39');await periods.nth(1).fill('60');
 await form.getByLabel('Reason',{exact:true}).fill('Synthetic approved distribution');
 await form.getByLabel('Evidence reference',{exact:true}).fill('Synthetic browser fixture approval; distribution is not measured');
 page.once('dialog',d=>d.accept());
 const rejected=page.waitForResponse(r=>r.url().endsWith('/api/subscribers/reconciliation')&&r.request().method()==='POST');
 await form.getByRole('button',{name:'Approve distribution'}).click();assert.equal((await rejected).status(),400);
 await page.waitForFunction(()=>document.querySelector('#subReviewNotice').textContent!=='Loading reconciliation records…');
 assert.equal(await periods.nth(0).inputValue(),'39','failed approval cleared evidence');
 for(const width of [375,1280]) {
  await page.setViewportSize({width,height:1000});
  assert.equal(await page.locator('html').getAttribute('dir'),'ltr');
  const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1);
  if(overflow){
   console.log('Overflow elements',await page.locator('*').evaluateAll(nodes=>nodes.filter(n=>n.getBoundingClientRect().right>innerWidth+1).slice(0,15).map(n=>({tag:n.tagName,id:n.id,class:n.className,width:n.getBoundingClientRect().width,text:n.textContent.slice(0,65)}))));
   await page.screenshot({path:`${out}/subscriber-admin-overflow-${width}.png`,fullPage:true});
  }
  assert.equal(overflow,false,`overflow at ${width}`);
  for(const tab of await page.getByRole('tab').all()) {
   const box=await tab.boundingBox();assert.ok(box&&box.x>=0&&box.x+box.width<=width+1,'a tab is clipped at '+width);
  }
  const result=await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();
  assert.deepEqual(result.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)})),[]);
  await page.screenshot({path:`${out}/subscriber-admin-reconciliation-synthetic-${width}.png`,fullPage:true});
 }
 await periods.nth(0).fill('40');page.once('dialog',d=>d.accept());
 const approved=page.waitForResponse(r=>r.url().endsWith('/api/subscribers/reconciliation')&&r.request().method()==='POST');
 await form.getByRole('button',{name:'Approve distribution'}).click();assert.equal((await approved).status(),200);
 await page.getByText('Settlement approved and recorded.',{exact:true}).waitFor();
 assert.equal(await form.count(),0);
 assert.equal(await page.locator('#subReviewLoad').evaluate(n=>n===document.activeElement),true,'approval lost keyboard focus');
 const snapshot=await (await context.request.get(origin+'/api/subscribers/snapshot?account='+encodeURIComponent(process.env.SUBSCRIBER_ACCOUNT_ID))).json();
 assert.equal(snapshot.lifetimeDownload,'100');assert.equal(snapshot.lifetimeSeconds,'20');
 assert.equal(snapshot.decision.limits[0].used,'40');assert.equal(snapshot.decision.limits[0].provisional,'0');
 assert.deepEqual(errors,[]);
 await context.close();
 console.log('Subscriber admin browser acceptance passed: protected SQLite approval, conservation refusal, retained evidence, account-race isolation, keyboard tabs, Unicode, 375/1280 and axe.');
} finally {await browser.close();}
