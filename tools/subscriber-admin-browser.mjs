// Real protected HTTP/SQLite acceptance on a synthetic local TLS fixture.
import {createRequire} from 'node:module';
import {mkdirSync,readFileSync} from 'node:fs';
import assert from 'node:assert/strict';
const require=createRequire(process.env.SUBSCRIBER_BROWSER_MODULE+'/package.json');
const {chromium}=require('playwright');
const {default:AxeBuilder}=require('@axe-core/playwright');
const out=process.env.SUBSCRIBER_EVIDENCE_DIR;
const geometrySource=readFileSync(new URL('./form-geometry.js',import.meta.url),'utf8');
mkdirSync(out,{recursive:true});
const browser=await chromium.launch({headless:true,...(process.env.SUBSCRIBER_CHROMIUM?{executablePath:process.env.SUBSCRIBER_CHROMIUM}:{})});
try {
 const context=await browser.newContext({ignoreHTTPSErrors:true}); // Synthetic httptest identity only.
 const origin=process.env.SUBSCRIBER_TEST_URL;
 await context.addCookies([{name:'mikrodash_sid',value:'synthetic-token',url:origin,httpOnly:true,secure:true,sameSite:'Lax'}]);
 const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(origin);
 await page.getByText('Unicode fixture: اختبار',{exact:true}).waitFor();
 await page.locator('#subscriberRuntimeStatus').getByText('Disabled',{exact:true}).waitFor();
 await page.getByLabel('Username',{exact:true}).fill('synthetic-offline-browser');
 await page.getByLabel('Display name',{exact:true}).fill('Synthetic offline: تجربة');
 await page.getByLabel('Password',{exact:true}).fill('synthetic-password');
 await page.getByLabel('Change reason',{exact:true}).first().fill('Synthetic offline browser setup');
 const created=page.waitForResponse(r=>r.url().endsWith('/api/subscribers')&&r.request().method()==='POST');
 await page.getByRole('button',{name:'Create subscriber',exact:true}).click();
 const createdResponse=await created;assert.equal(createdResponse.status(),200);
 const createdAccount=(await createdResponse.json()).account;
 await page.getByRole('cell',{name:'Synthetic offline: تجربة',exact:true}).waitFor();
 await page.getByText('Subscriber created.',{exact:true}).waitFor();
 assert.match(await page.locator('#subscriberRuntimeStatus').innerText(),/portal and RADIUS are disabled/);
 for(const width of [375,1280]) {
  await page.setViewportSize({width,height:1000});
  assert.equal(await page.locator('html').getAttribute('dir'),'ltr');
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1),false,'offline setup overflow');
  const accessibility=await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();
  assert.deepEqual(accessibility.violations,[]);
  await page.screenshot({path:`${out}/subscriber-admin-offline-synthetic-${width}.png`,fullPage:true});
 }

 // Exercise a real concurrent administrator edit before saving this form.
 const editButton=()=>page.locator(`[data-sub-edit="${createdAccount.id}"]`);
 const nameEditor=page.locator('#subscriberNameForm');
 await editButton().click();
 assert.equal(await page.locator('#subscriberCreateForm').isVisible(),false);
 await nameEditor.getByLabel('New display name',{exact:true}).fill('Discarded synthetic edit');
 await nameEditor.getByRole('button',{name:'Cancel',exact:true}).click();
 assert.equal(await nameEditor.isVisible(),false);
 assert.equal(await page.locator('#subscriberCreateForm').isVisible(),true);
 assert.equal(await editButton().evaluate(n=>n===document.activeElement),true,'cancel lost account focus');
 await editButton().click();
 await nameEditor.getByLabel('New display name',{exact:true}).fill('x'.repeat(257));
 await nameEditor.getByLabel('Change reason',{exact:true}).fill('Synthetic invalid name');
 const invalidName=page.waitForResponse(r=>r.url().endsWith('/api/subscribers')&&r.request().method()==='POST');
 await nameEditor.getByRole('button',{name:'Save display name',exact:true}).click();
 assert.equal((await invalidName).status(),400);
 await page.getByText('display name must be valid Unicode and at most 256 UTF-8 bytes',{exact:true}).waitFor();
 assert.equal(await nameEditor.getByLabel('New display name',{exact:true}).inputValue(),'x'.repeat(257),'validation cleared input');
 const concurrentName='Concurrent synthetic name';
 const concurrent=await context.request.post(origin+'/api/subscribers',{headers:{Origin:origin,'X-MikroDash-Request':'subscriber-admin'},data:{action:'name',accountId:createdAccount.id,expectedName:createdAccount.name,name:concurrentName,reason:'Synthetic concurrent administrator'}});
 assert.equal(concurrent.status(),200);
 const editedName='Synthetic edited: اسم <script>window.syntheticNameExecuted=true</script>';
 await nameEditor.getByLabel('New display name',{exact:true}).fill(editedName);
 await nameEditor.getByLabel('Change reason',{exact:true}).fill('Synthetic name correction');
 const staleName=page.waitForResponse(r=>r.url().endsWith('/api/subscribers')&&r.request().method()==='POST');
 await nameEditor.getByRole('button',{name:'Save display name',exact:true}).click();
 assert.equal((await staleName).status(),409);
 await page.getByText('Subscriber details changed. Refresh the list and open Edit name again before saving.',{exact:true}).waitFor();
 assert.equal(await nameEditor.getByLabel('New display name',{exact:true}).inputValue(),editedName,'stale edit cleared input');
 assert.equal(await nameEditor.getByLabel('Change reason',{exact:true}).inputValue(),'Synthetic name correction');
 const refreshed=page.waitForResponse(r=>r.url().endsWith('/api/subscribers')&&r.request().method()==='GET');
 await page.getByRole('button',{name:'Refresh',exact:true}).click();await refreshed;
 await page.getByRole('cell',{name:concurrentName,exact:true}).waitFor();
 await nameEditor.getByRole('button',{name:'Cancel',exact:true}).click();await editButton().click();
 assert.equal(await nameEditor.getByLabel('New display name',{exact:true}).inputValue(),concurrentName);
 await nameEditor.getByLabel('New display name',{exact:true}).fill(editedName);
 await nameEditor.getByLabel('Change reason',{exact:true}).fill('Synthetic reviewed name correction');
 for(const appearance of [{theme:'dark',palette:''},{theme:'light',palette:''},{theme:'dark',palette:'nord'}]) {
  await page.evaluate(({theme,palette})=>{document.documentElement.dataset.theme=theme;document.documentElement.dataset.bsTheme=theme;if(palette)document.documentElement.dataset.palette=palette;else delete document.documentElement.dataset.palette;},appearance);
 for(const width of [375,1280]) {
  await page.setViewportSize({width,height:1000});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1),false,'name editor overflow');
  const result=await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();
  assert.deepEqual(result.violations,[]);
  await page.screenshot({path:`${out}/subscriber-name-editor-synthetic-${appearance.theme}-${appearance.palette||'default'}-${width}.png`,fullPage:true});
 }
 }
 await page.evaluate(()=>{document.documentElement.dataset.theme='dark';document.documentElement.dataset.bsTheme='dark';delete document.documentElement.dataset.palette;});
 let releaseName;const nameGate=new Promise(resolve=>{releaseName=resolve;});
 let nameFetched;const nameReady=new Promise(resolve=>{nameFetched=resolve;});
 await page.route('**/api/subscribers',async route=>{const response=await route.fetch();nameFetched();await nameGate;await route.fulfill({response});},{times:1});
 await nameEditor.getByRole('button',{name:'Save display name',exact:true}).click();await nameReady;
 assert.equal(await nameEditor.getByRole('button',{name:'Saving…',exact:true}).isDisabled(),true);
 assert.equal(await nameEditor.getByRole('button',{name:'Cancel',exact:true}).isDisabled(),true);
 assert.equal(await editButton().isDisabled(),true);releaseName();
 await page.getByText('Display name updated.',{exact:true}).waitFor();
 await page.getByRole('cell',{name:editedName,exact:true}).waitFor();
 assert.equal(await page.locator('#subscriberAccountRows script').count(),0);
 assert.equal(await page.evaluate(()=>window.syntheticNameExecuted),undefined);
 assert.equal(await editButton().evaluate(n=>n===document.activeElement),true,'save lost selected account focus');
 const named=(await (await context.request.get(origin+'/api/subscribers')).json()).accounts.find(a=>a.id===createdAccount.id);
 assert.equal(named.name,editedName);assert.equal(named.username,createdAccount.username);

 // Account selection is explicit; changing filters cancels any old review.
 const apiHeaders={Origin:origin,'X-MikroDash-Request':'subscriber-admin'};
 const currentAccounts=()=>context.request.get(origin+'/api/subscribers').then(response=>response.json());
 const otherAccount=(await currentAccounts()).accounts.find(account=>account.username==='synthetic-account-two');
 assert.ok(otherAccount);
 const choose=id=>page.locator(`[data-sub-select="${id}"]`);
 const reason=page.getByLabel('Batch change reason',{exact:true});
 const prepare=page.getByRole('button',{name:'Review selected subscribers',exact:true});
 const apply=page.getByRole('button',{name:'Apply reviewed status change',exact:true});
 const confirm=page.getByLabel('I reviewed these subscribers and their new status.',{exact:true});
 const search=page.getByLabel('Search subscribers',{exact:true});
 const accountStatus=page.getByLabel('Account status',{exact:true});
 await choose(createdAccount.id).check();
 assert.equal(await choose(createdAccount.id).evaluate(node=>node===document.activeElement),true,'selection redraw lost keyboard focus');
 await reason.fill('Synthetic cancelled review');await prepare.click();
 assert.equal(await confirm.evaluate(node=>node===document.activeElement),true);
 assert.equal(await apply.isDisabled(),true,'review sent a batch without confirmation');
 await page.getByRole('button',{name:'Back to selection',exact:true}).click();
 assert.equal(await reason.evaluate(node=>node===document.activeElement),true);
 await prepare.click();await search.fill('مشترك-no-match');
 assert.equal(await page.locator('#subscriberBatchReview').isVisible(),false,'filter kept a hidden reviewed selection');
 assert.match(await page.locator('#subscriberSelectionCount').innerText(),/^0 selected/);
 await page.getByText('No subscribers match the current filters.',{exact:true}).waitFor();
 await search.fill('');
 await choose(createdAccount.id).check();await choose(otherAccount.id).check();
 await reason.fill('Synthetic stale selection');await prepare.click();
 assert.equal((await context.request.post(origin+'/api/subscribers',{headers:apiHeaders,data:{action:'status',accountId:otherAccount.id,status:'suspended',reason:'Synthetic concurrent administrator'}})).status(),200);
 assert.equal((await context.request.post(origin+'/api/subscribers',{headers:apiHeaders,data:{action:'status',accountId:otherAccount.id,status:'active',reason:'Synthetic status restored before stale browser submission'}})).status(),200);
 await confirm.check();
 const staleBatch=page.waitForResponse(response=>response.url().endsWith('/api/subscribers')&&response.request().method()==='POST');
 await apply.click();assert.equal((await staleBatch).status(),409);
 await page.waitForFunction(()=>document.querySelector('#subscriberAdminNotice').textContent.includes('review the entire selection again'));
 assert.equal(await apply.isDisabled(),true,'stale review could be replayed');
 assert.equal(await reason.inputValue(),'Synthetic stale selection','conflict erased the reason');
 assert.equal(await page.locator('#subscriberBatchResults li').count(),2);
 for(const text of await page.locator('#subscriberBatchResults li').allTextContents())assert.ok(text.includes('Not confirmed. Refresh the list.'));
 let stored=await currentAccounts();assert.equal(stored.accounts.find(account=>account.id===createdAccount.id).status,'active','stale batch partially updated its first account');
 assert.equal(stored.accounts.find(account=>account.id===otherAccount.id).status,'active');
 assert.notEqual(stored.accounts.find(account=>account.id===otherAccount.id).statusRevision,otherAccount.statusRevision,'ABA fixture did not advance the stored review identity');
 const reload=page.waitForResponse(response=>response.url().endsWith('/api/subscribers')&&response.request().method()==='GET');
 await page.getByRole('button',{name:'Refresh',exact:true}).click();await reload;
 await page.waitForFunction(()=>document.querySelector('#subscriberBatchReview').hidden);
 await choose(createdAccount.id).check();await choose(otherAccount.id).check();
 const batchReason='Synthetic reviewed <script>window.syntheticBatchExecuted=true</script>';
 await reason.fill(batchReason);await prepare.click();
 assert.match(await page.locator('#subscriberBatchReviewedReason').innerText(),/Synthetic reviewed <script>/);
 assert.equal(await page.locator('#subscriberBatchReview script').count(),0);
 assert.equal(await choose(createdAccount.id).isDisabled(),true,'review selection was mutable');
 for(const appearance of [{theme:'dark',palette:''},{theme:'light',palette:''},{theme:'dark',palette:'nord'}]) {
  await page.evaluate(({theme,palette})=>{document.documentElement.dataset.theme=theme;document.documentElement.dataset.bsTheme=theme;if(palette)document.documentElement.dataset.palette=palette;else delete document.documentElement.dataset.palette;},appearance);
  for(const width of [375,1280]) {
   await page.setViewportSize({width,height:1000});
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1),false,'status review overflow');
   const geometry=await page.evaluate(geometrySource);assert.ok(geometry.fields>0,'form geometry measured no fields');assert.equal(geometry.ok,true,JSON.stringify(geometry.issues));
   if(appearance.theme==='dark'&&!appearance.palette&&width===375){await search.evaluate(node=>{node.style.height='50px';});const broken=await page.evaluate(geometrySource);assert.equal(broken.ok,false,'geometry gate accepted a deliberately broken new control');await search.evaluate(node=>{node.style.removeProperty('height');});const restored=await page.evaluate(geometrySource);assert.equal(restored.ok,true,JSON.stringify(restored.issues));}
   const audit=await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(audit.violations,[]);
   await page.screenshot({path:`${out}/subscriber-status-review-synthetic-${appearance.theme}-${appearance.palette||'default'}-${width}.png`,fullPage:true});
   await page.locator('#subscriberBatchReview').screenshot({path:`${out}/subscriber-status-review-controls-synthetic-${appearance.theme}-${appearance.palette||'default'}-${width}.png`});
  }
 }
 await page.evaluate(()=>{document.documentElement.dataset.theme='dark';document.documentElement.dataset.bsTheme='dark';delete document.documentElement.dataset.palette;});
 let releaseBatch,batchFetched;const batchGate=new Promise(resolve=>{releaseBatch=resolve;}),batchReady=new Promise(resolve=>{batchFetched=resolve;});
 await page.route('**/api/subscribers',async route=>{const response=await route.fetch();batchFetched();await batchGate;await route.fulfill({response});},{times:1});
 await confirm.check();const appliedBatch=page.waitForResponse(response=>response.url().endsWith('/api/subscribers')&&response.request().method()==='POST');
 await apply.click();await batchReady;
 assert.equal(await page.getByRole('button',{name:'Applying…',exact:true}).isDisabled(),true);
 assert.equal(await page.getByRole('button',{name:'Refresh',exact:true}).isDisabled(),true);
 assert.equal(await page.getByRole('button',{name:'Back to selection',exact:true}).isDisabled(),true);
 assert.ok(!(await page.locator('#subscriberAdminNotice').innerText()).includes('Status updated for'),'UI claimed commit success before receiving a response');
 releaseBatch();const applied=await appliedBatch;assert.equal(applied.status(),200);const appliedResult=await applied.json();assert.equal(appliedResult.changed.length,2);assert.ok(appliedResult.batchId);
 await page.getByText('Status updated for 2 subscribers. 0 already had that status.',{exact:true}).waitFor();
 assert.deepEqual(appliedResult.results.map(item=>[item.account.id,item.outcome]),[[createdAccount.id,'changed'],[otherAccount.id,'changed']]);
 assert.equal(await page.locator('#subscriberBatchResults li').count(),2);
 for(const text of await page.locator('#subscriberBatchResults li').allTextContents())assert.ok(text.includes('Updated: suspended'));
 assert.equal(await search.evaluate(node=>node===document.activeElement),true,'committed batch lost keyboard focus');
 stored=await currentAccounts();assert.equal(stored.mode,'disabled');
 for(const id of [createdAccount.id,otherAccount.id])assert.equal(stored.accounts.find(account=>account.id===id).status,'suspended');
 assert.equal(stored.accounts.find(account=>account.id===createdAccount.id).name,editedName);
 assert.equal(stored.accounts.find(account=>account.id===process.env.SUBSCRIBER_ACCOUNT_ID).status,'active','batch changed an unselected account');
 assert.equal(await page.evaluate(()=>window.syntheticBatchExecuted),undefined);
 await search.fill('اسم');assert.equal(await page.locator('#subscriberAccountRows tr').count(),1);assert.match(await page.locator('#subscriberAccountFilterCount').innerText(),/^1 of 3 subscribers shown/);
 await accountStatus.selectOption('active');await page.getByText('No subscribers match the current filters.',{exact:true}).waitFor();
 await accountStatus.selectOption('suspended');await page.getByRole('cell',{name:editedName,exact:true}).waitFor();
 await search.fill('');await accountStatus.selectOption('all');
 await choose(createdAccount.id).check();await choose(otherAccount.id).check();
 await page.getByLabel('New account status',{exact:true}).selectOption('active');
 await choose(process.env.SUBSCRIBER_ACCOUNT_ID).check();
 await reason.fill('Synthetic reviewed reactivation');await prepare.click();await confirm.check();
 let releaseSecond,secondFetched;const secondGate=new Promise(resolve=>{releaseSecond=resolve;}),secondReady=new Promise(resolve=>{secondFetched=resolve;});
 await page.route('**/api/subscribers',async route=>{const response=await route.fetch();secondFetched();await secondGate;await route.fulfill({response});},{times:1});
 const reactivated=page.waitForResponse(response=>response.url().endsWith('/api/subscribers')&&response.request().method()==='POST');
 await apply.click();await secondReady;
 try {
  assert.equal(await page.locator('#subscriberBatchResults').isVisible(),false,'previous batch results remained visible during the next request');
  assert.equal(await page.locator('#subscriberAdminNotice').innerText(),'Applying reviewed status change…','prior success remained visible during the next request');
 } finally {
  // Drain the genuine response even when an assertion fails, preserving its cause.
  releaseSecond();await reactivated;
 }
 const secondResponse=await reactivated;assert.equal(secondResponse.status(),200);
 const secondResult=await secondResponse.json();assert.equal(secondResult.changed.length,2);assert.equal(secondResult.skipped,1);
 assert.deepEqual(secondResult.results.map(item=>[item.account.id,item.outcome]),[[createdAccount.id,'changed'],[otherAccount.id,'changed'],[process.env.SUBSCRIBER_ACCOUNT_ID,'unchanged']]);
 await page.getByText('Status updated for 2 subscribers. 1 already had that status.',{exact:true}).waitFor();
 assert.equal(await page.locator('#subscriberBatchResults li').count(),3);
 assert.ok((await page.locator('#subscriberBatchResults li').last().innerText()).includes('Already had this status: active'));
 await page.waitForFunction(()=>document.querySelector('#subscriberBatchReview').hidden);
 stored=await currentAccounts();for(const id of [createdAccount.id,otherAccount.id])assert.equal(stored.accounts.find(account=>account.id===id).status,'active');
 for(const width of [375,1280]){await page.setViewportSize({width,height:1000});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1),false,'batch results overflow');const audit=await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(audit.violations,[]);await page.screenshot({path:`${out}/subscriber-status-results-synthetic-${width}.png`,fullPage:true});}

 await page.locator(`[data-sub-inspect="${process.env.SUBSCRIBER_ACCOUNT_ID}"]`).click();
 await page.locator('#subscriberInspect').getByText('Accounting reconciliation required (1 pending). Last confirmed balances are shown. New Internet connections are blocked until the counters are settled.',{exact:true}).waitFor();
 assert.ok(!(await page.locator('#subscriberInspect').innerText()).includes('Unknown'),'pending accounting hid trusted limit cards');
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
 const receipt=page.locator('#subReviewRows > details').first();
 await receipt.locator('summary').first().click();
 await receipt.getByText('Not supplied or malformed',{exact:false}).waitFor();
 await receipt.getByText('Original accounting fields',{exact:true}).click();
 assert.match(await receipt.locator('pre').innerText(),/"Acct-Output-Octets": "101"/);
 const periods=form.locator('[data-reconcile-period]');assert.equal(await periods.count(),2);
 await periods.nth(0).fill('39');await periods.nth(1).fill('60');
 await form.getByLabel('Reason',{exact:true}).fill('Synthetic approved distribution');
 await form.getByLabel('Evidence reference',{exact:true}).fill('Synthetic browser fixture approval; distribution is not measured');
 page.once('dialog',d=>d.accept());
 const rejected=page.waitForResponse(r=>r.url().endsWith('/api/subscribers/reconciliation')&&r.request().method()==='POST');
 await form.getByRole('button',{name:'Approve distribution'}).click();assert.equal((await rejected).status(),400);
 await page.waitForFunction(()=>document.querySelector('#subReviewNotice').textContent!=='Loading reconciliation records…');
 assert.equal(await periods.nth(0).inputValue(),'39','failed approval cleared evidence');
 for(const appearance of [{theme:'dark',palette:''},{theme:'light',palette:''},{theme:'dark',palette:'nord'}]) {
  await page.evaluate(({theme,palette})=>{document.documentElement.dataset.theme=theme;document.documentElement.dataset.bsTheme=theme;if(palette)document.documentElement.dataset.palette=palette;else delete document.documentElement.dataset.palette;},appearance);
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
  await page.screenshot({path:`${out}/subscriber-admin-reconciliation-synthetic-${appearance.theme}-${appearance.palette||'default'}-${width}.png`,fullPage:true});
 }
 }
 await page.evaluate(()=>{document.documentElement.dataset.theme='dark';document.documentElement.dataset.bsTheme='dark';delete document.documentElement.dataset.palette;});
 await periods.nth(0).fill('40');page.once('dialog',d=>d.accept());
 const approved=page.waitForResponse(r=>r.url().endsWith('/api/subscribers/reconciliation')&&r.request().method()==='POST');
 await form.getByRole('button',{name:'Approve distribution'}).click();assert.equal((await approved).status(),200);
 await page.getByText('Settlement approved and recorded.',{exact:true}).waitFor();
 assert.equal(await form.count(),0);
 assert.equal(await page.locator('#subReviewLoad').evaluate(n=>n===document.activeElement),true,'approval lost keyboard focus');
 const snapshot=await (await context.request.get(origin+'/api/subscribers/snapshot?account='+encodeURIComponent(process.env.SUBSCRIBER_ACCOUNT_ID))).json();
 assert.equal(snapshot.lifetimeDownload,'100');assert.equal(snapshot.lifetimeSeconds,'20');
 assert.equal(snapshot.decision.limits[0].used,'40');assert.equal(snapshot.decision.limits[0].provisional,'0');
 assert.equal(snapshot.pendingAccounting,1);
 const final=page.locator('form[data-review="missing-stop"]');
 await final.getByLabel('Final cumulative seconds',{exact:true}).fill('20');
 await final.getByLabel('Final cumulative download bytes',{exact:true}).fill('101');
 await final.getByLabel('Final cumulative upload bytes',{exact:true}).fill('0');
 await final.getByLabel('Reason',{exact:true}).fill('Synthetic correction of incomplete final accounting');
 await final.getByLabel('Evidence reference',{exact:true}).fill('Synthetic independent final byte reading');
 page.once('dialog',d=>d.accept());
 let releaseFinal,finalFetched;const finalGate=new Promise(resolve=>{releaseFinal=resolve;}),finalReady=new Promise(resolve=>{finalFetched=resolve;});
 await page.route('**/api/subscribers/reconciliation',async route=>{const response=await route.fetch();finalFetched();await finalGate;await route.fulfill({response});},{times:1});
 const corrected=page.waitForResponse(r=>r.url().endsWith('/api/subscribers/reconciliation')&&r.request().method()==='POST');
 await final.getByRole('button',{name:'Approve final reading'}).click();await finalReady;
 assert.equal(await page.locator('#subReviewNotice').innerText(),'Recording settlement…','prior success was reused while a new settlement was pending');
 assert.equal(await final.getByRole('button',{name:'Approve final reading'}).isDisabled(),true);
 releaseFinal();assert.equal((await corrected).status(),200);
 await page.getByText('Settlement approved and recorded.',{exact:true}).waitFor();
 const settled=await (await context.request.get(origin+'/api/subscribers/snapshot?account='+encodeURIComponent(process.env.SUBSCRIBER_ACCOUNT_ID))).json();
 assert.equal(settled.pendingAccounting,0);assert.equal(settled.lifetimeDownload,'101');assert.equal(settled.lifetimeSeconds,'20');
 assert.equal(await final.count(),0);
 assert.deepEqual(errors,[]);
 await context.close();
 console.log('Subscriber admin browser acceptance passed: protected SQLite approval, conservation refusal, retained evidence, account-race isolation, keyboard tabs, Unicode, 375/1280 and axe.');
} finally {await browser.close();}
