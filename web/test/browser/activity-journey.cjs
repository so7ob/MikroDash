const {chromium}=require('playwright');
const {AxeBuilder}=require('@axe-core/playwright');
const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true});
 const context=await browser.newContext({viewport:{width:1280,height:1000},acceptDownloads:true,hasTouch:true,timezoneId:'UTC'});
 await context.addCookies([{name:'mikrodash_sid',value:'activity-test-token',url:process.argv[2]}]);
 const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(process.argv[2]+'/journey');await page.waitForFunction(()=>document.querySelector('#activityState')?.textContent?.includes('disabled'));
 await page.locator('#activityConfig').evaluate(e=>e.open=true);
 await page.locator('#activityEnabled').check();await page.locator('#activityTimezone').fill('UTC');await page.locator('#activitySaveSettings').click();
 await page.waitForTimeout(1200);await page.evaluate(()=>window.dispatchEvent(new Event('fixture-invalidate')));
 await page.waitForFunction(()=>document.querySelector('#activityCount')?.textContent==='130');
 await page.locator('#activityConfig').evaluate(e=>e.open=false);
 await page.screenshot({path:'/tmp/mikrodash-activity-desktop.png',fullPage:true});
 // Every overview chart/ranking filters in place; only Ctrl opens Events.
 const settled=()=>page.waitForFunction(()=>document.querySelector('#page-activity')?.getAttribute('aria-busy')==='false');
 const resetOverview=async()=>{
  await page.locator('[data-view="summary"]').click();
  if(await page.locator('[data-clear="all"]').count())await page.locator('[data-clear="all"]').click();
  if(await page.locator('#activityKind').inputValue())await page.locator('#activityKind').selectOption('');
  await page.waitForFunction(()=>document.querySelector('#activityCount')?.textContent==='130'&&document.querySelector('#page-activity')?.getAttribute('aria-busy')==='false');
 };
 for(const selector of ['#activityCategories','#activityServiceDonut','#activityUserDonut','#activityCountryDonut','#activityServices','#activityClients']){
  await resetOverview();const button=page.locator(selector+' button[data-filter]').first();
  const filter=await button.getAttribute('data-filter'),value=await button.getAttribute('data-value');
  await button.click();await settled();
  assert.equal(await page.locator('[data-view="summary"]').getAttribute('aria-selected'),'true',selector+' ordinary click left Overview');
  assert.equal(new URL(page.url()).searchParams.get(filter),value);
  assert.equal(await page.locator('#activityOverview').isVisible(),true);
  if(filter==='country')assert.equal(await page.locator('#activityKind').inputValue(),'firewall');
  assert.equal(await button.getAttribute('aria-pressed'),'true',selector+' has no selected state');
  await button.click();await settled();
  assert.equal(new URL(page.url()).searchParams.has(filter),false,selector+' repeated label did not clear');
  assert.equal(await page.locator('[data-view="summary"]').getAttribute('aria-selected'),'true');
  assert.equal(await page.locator('#activityCount').textContent(),'130',selector+' did not restore all DNS events');
  if(filter==='country')assert.equal(await page.locator('#activityKind').inputValue(),'');
  await button.click();await settled();
  await button.click({modifiers:['Control']});await settled();
  assert.equal(await page.locator('[data-view="events"]').getAttribute('aria-selected'),'true',selector+' Ctrl did not open Events');
  assert.equal(new URL(page.url()).searchParams.get(filter),value);assert.equal(context.pages().length,1);
 }
 // Click the actual painted midpoint, rather than a path's bounding box.
 const clickSlice=async(selector,key,modifiers=[],touch=false)=>{
  const point=await page.locator(selector+' svg').evaluate((svg,key)=>{
   const path=Array.from(svg.querySelectorAll('path')).find(p=>p.dataset.value===key);
   if(!path)throw new Error('slice missing: '+key);
   const p=path.getPointAtLength(path.getTotalLength()/2).matrixTransform(path.getScreenCTM());
   const hit=document.elementFromPoint(p.x,p.y);
   if(hit!==path)throw new Error('overlapping or unreachable slice: '+key);
   return {x:p.x,y:p.y};
  },key);
  if(modifiers.includes('Control'))await page.keyboard.down('Control');
  if(touch)await page.touchscreen.tap(point.x,point.y);else await page.mouse.click(point.x,point.y);
  if(modifiers.includes('Control'))await page.keyboard.up('Control');
  await settled();
 };
 let slices=0;
 for(const selector of ['#activityCategories','#activityServiceDonut','#activityUserDonut','#activityCountryDonut']){
  await resetOverview();
  const keys=await page.locator(selector+' path[data-filter]').evaluateAll(paths=>paths.map(p=>p.dataset.value));
  assert.ok(keys.length>0,selector+' measured no slices');
  if(selector==='#activityUserDonut'){
   const shares=(await page.locator(selector+' button[data-filter] strong').allTextContents()).map(parseFloat);
   assert.ok(Math.min(...shares)<1,'fixture did not measure a sub-1% selectable slice');
  }
  for(const key of keys){
   await resetOverview();await page.locator(selector).scrollIntoViewIfNeeded();
   const filter=await page.locator(selector+' path[data-filter]').first().getAttribute('data-filter');
   await clickSlice(selector,key);
   assert.equal(new URL(page.url()).searchParams.get(filter),key,'slice selected the wrong value');
   assert.equal(await page.locator('[data-view="summary"]').getAttribute('aria-selected'),'true');
   assert.equal(await page.locator(selector+' button[aria-pressed="true"]').getAttribute('data-value'),key);
   // The same slice is now often a full circle; it must still toggle off.
   await clickSlice(selector,key);
   assert.equal(new URL(page.url()).searchParams.has(filter),false,'second slice click did not clear');
   assert.equal(await page.locator('#activityCount').textContent(),'130');
   slices++;
  }
 }
 await resetOverview();await page.locator('#activityServiceDonut').scrollIntoViewIfNeeded();
 const serviceKey=await page.locator('#activityServiceDonut button[data-filter]').first().getAttribute('data-value');
 await page.locator('#activityServiceDonut button[data-filter]').first().click();await settled();
 await clickSlice('#activityServiceDonut',serviceKey,['Control']);
 assert.equal(new URL(page.url()).searchParams.get('tab'),'events');
 assert.equal(new URL(page.url()).searchParams.get('service'),serviceKey,'Ctrl slice removed selected evidence');
 await resetOverview();await page.locator('#activityServiceDonut').scrollIntoViewIfNeeded();
 const noSelectionURL=page.url();
 const other=page.locator('#activityServiceDonut path:not([data-filter])');
 assert.equal(await other.count(),1,'fixture did not measure Other');
 {
  const p=await other.evaluate(path=>{const p=path.getPointAtLength(path.getTotalLength()/2).matrixTransform(path.getScreenCTM());return {x:p.x,y:p.y};});
  await page.mouse.click(p.x,p.y);assert.equal(page.url(),noSelectionURL,'Other is an aggregate');
 }
 const box=await page.locator('#activityServiceDonut svg').boundingBox();
 await page.mouse.click(box.x+box.width/2,box.y+box.height/2);assert.equal(page.url(),noSelectionURL,'donut hole filtered');
 // Combined chart filters can be removed independently.
 await resetOverview();
 await page.locator('#activityCategories button[data-filter]').first().click();await settled();
 const category=new URL(page.url()).searchParams.get('category'),categoryCount=await page.locator('#activityCount').textContent();
 await page.locator('#activityServiceDonut button[data-filter]').first().click();await settled();
 await page.locator('#activityServiceDonut button[data-filter]').first().click();await settled();
 assert.equal(new URL(page.url()).searchParams.get('category'),category);
 assert.equal(await page.locator('#activityCount').textContent(),categoryCount);
 // A country-imposed source remembers an explicitly chosen source across reload.
 await resetOverview();await page.locator('#activityKind').selectOption('firewall');await settled();
 await page.locator('#activityCountryDonut button[data-filter]').first().click();await settled();
 await page.reload();await settled();
 await page.locator('#activityCountryDonut button[data-filter]').first().click();await settled();
 assert.equal(new URL(page.url()).searchParams.has('country'),false);
 assert.equal(await page.locator('#activityKind').inputValue(),'firewall');
 await resetOverview();
 console.log('slice selection: '+slices+' painted targets including small/full circles; label/slice toggle, Ctrl evidence, Other and hole passed');
 await resetOverview();
 const rangeState=()=>{
  const p=new URL(page.url()).searchParams;
  return Object.fromEntries(['days','from','to'].map(key=>[key,p.get(key)]));
 };
 const selectTime=async()=>{
  // Pick an occupied bucket so filtering has visible evidence.
  const index=await page.locator('#activityTimeline button').evaluateAll(nodes=>{const heights=nodes.map(node=>parseFloat(node.style.getPropertyValue('--activity-height')));return heights.indexOf(Math.max(...heights));});
  assert.ok(index>=0,'no time buckets were measured');
  await page.locator('#activityTimeline button').nth(index).click();await settled();
  assert.equal(await page.locator('[data-view="summary"]').getAttribute('aria-selected'),'true','time bar left Overview');
  const p=new URL(page.url()).searchParams;assert.ok(p.has('from')&&p.has('to')&&p.has('timeRange'));
  assert.equal(await page.locator('#activityTimeline button').count(),1,'selected bucket split into new targets');
  assert.equal(await page.locator('#activityTimeline button').getAttribute('aria-pressed'),'true');
 };
 // Rolling windows restore their original choice, including 6h buckets at 30d.
 for(const days of ['1','7','30']){
  await page.locator('#activityDays').selectOption(days);await settled();
  const before=rangeState();
  await selectTime();
  await page.locator('#activityTimeline button').click();await settled();
  assert.deepEqual(rangeState(),before,'time toggle did not restore '+days+' day period');
  assert.equal(new URL(page.url()).searchParams.has('timeRange'),false);
 }
 // Ctrl and keyboard Ctrl keep the selected range when opening Events.
 const beforeCtrl=rangeState();await selectTime();const range=rangeState();
 await page.locator('#activityTimeline button').click({modifiers:['Control']});await settled();
 assert.equal(await page.locator('[data-view="events"]').getAttribute('aria-selected'),'true','Ctrl time bar did not open Events');
 assert.deepEqual(rangeState(),range,'Ctrl time bar toggled away its evidence');
 await page.locator('[data-view="summary"]').click();await settled();
 await page.locator('#activityTimeline button').focus();await page.keyboard.press('Control+Enter');await settled();
 assert.equal(await page.locator('[data-view="events"]').getAttribute('aria-selected'),'true','Ctrl+Enter time bar did not open Events');
 assert.deepEqual(rangeState(),range);
 await page.locator('[data-view="summary"]').click();await settled();
 await page.locator('#activityTimeline button').focus();await page.keyboard.press('Space');await settled();
 assert.deepEqual(rangeState(),beforeCtrl,'keyboard time toggle did not restore period');
 // Preserve partial custom endpoints and unrelated service filters across reload.
 await page.locator('#activityDays').selectOption('custom');
 const customFrom=new Date(Date.now()-150*60000).toISOString().slice(0,16),customTo=new Date(Date.now()-15*60000).toISOString().slice(0,16);
 await page.locator('#activityFrom').fill(customFrom);await page.locator('#activityTo').fill(customTo);
 await page.locator('#activityApplyRange').click();await settled();
 await page.locator('#activityServiceDonut button[data-filter]').first().click();await settled();
 const custom=rangeState(),customService=new URL(page.url()).searchParams.get('service');
 await selectTime();await page.reload();await settled();
 await page.locator('#activityTimeline button').click();await settled();
 assert.deepEqual(rangeState(),custom,'custom time toggle lost its original endpoints after reload');
 assert.equal(await page.locator('#activityFrom').inputValue(),customFrom);
 assert.equal(await page.locator('#activityTo').inputValue(),customTo);
 assert.equal(new URL(page.url()).searchParams.get('service'),customService,'time toggle cleared another filter');
 await selectTime();await page.locator('#activityDays').selectOption('7');await settled();
 assert.equal(new URL(page.url()).searchParams.has('timeRange'),false,'manual period retained a stale selection');
 const manual=rangeState();await selectTime();await page.locator('#activityTimeline button').click();await settled();
 assert.deepEqual(rangeState(),manual,'manual period did not become the new origin');
 await page.locator('#activityDays').selectOption('1');await resetOverview();
 console.log('time toggle: 1/7/30 days, six-hour bucket, partial custom endpoints, reload, other filters, manual period, Space and Ctrl click/Enter passed');
 // Native keyboard activation has the same default and modifier behavior.
 await page.locator('#activityServiceDonut button[data-filter]').first().focus();await page.keyboard.press('Enter');await settled();
 assert.equal(await page.locator('[data-view="summary"]').getAttribute('aria-selected'),'true','Enter left Overview');
 await page.locator('#activityServiceDonut button[data-filter]').first().focus();await page.keyboard.press('Space');await settled();
 assert.equal(new URL(page.url()).searchParams.has('service'),false,'Space did not toggle the selected legend off');
 await page.locator('#activityServiceDonut button[data-filter]').first().focus();await page.keyboard.press('Enter');await settled();
 await page.locator('#activityServiceDonut button[data-filter]').first().focus();await page.keyboard.press('Control+Enter');await settled();
 assert.equal(await page.locator('[data-view="events"]').getAttribute('aria-selected'),'true','Ctrl+Enter did not open Events');
 await resetOverview();
 console.log('chart selection: six legends/rankings, time range, country evidence, in-place filtering, Ctrl drilldown and Enter/Ctrl+Enter passed');
 await page.locator('[data-view="clients"]').click();await page.waitForSelector('#activityTableBody [data-filter="client"]');
 assert.ok((await page.locator('#activityTableBody').textContent()).includes('مستخدم تجريبي'));
 await page.locator('#activityTableBody [data-filter="client"]').first().click();await page.waitForSelector('#activityFilters [data-clear="client"]');
 await page.waitForFunction(()=>document.querySelector('#activityTableBody')?.textContent?.includes('DNSUSER'));
 const link=await page.locator('#activityExport').getAttribute('href');const csv=await context.request.get(process.argv[2]+link);assert.equal(csv.status(),200);assert.ok((await csv.text()).includes('domain,service,category'));
 await page.locator('[data-clear="all"]').click();await page.locator('[data-view="rules"]').click();
 await page.locator('#activityRuleDomain').fill('chatgpt.com');await page.locator('#activityRuleService').fill('ChatGPT Custom');await page.locator('#activityRuleCategory').fill('Research');await page.locator('#activitySaveRule').click();
 await page.waitForFunction(()=>document.querySelector('#activityRulesBody')?.textContent?.includes('ChatGPT Custom'));
 await page.locator('[data-view="domains"]').click();await page.waitForFunction(()=>document.querySelector('#activityTableBody')?.textContent?.includes('ChatGPT Custom'));
 const url=page.url();await page.reload();await page.waitForFunction(()=>document.querySelector('#activityTableBody')?.textContent?.includes('ChatGPT Custom'));assert.equal(page.url(),url);
 await page.locator('#activityGroup').selectOption('day');await page.waitForFunction(()=>/^\d{4}-/.test(document.querySelector('#activityTableBody button')?.textContent||''));
 await page.locator('#activityGroup').selectOption('hour');await page.waitForFunction(()=>document.querySelector('#activityTableBody')?.textContent?.includes(':00'));
 await page.locator('#activityGroup').selectOption('country');await page.waitForFunction(()=>document.querySelector('#activityTableBody')?.textContent?.includes('Unknown'));
 await page.locator('#activityTableBody [data-filter="country"]').click();await page.waitForFunction(()=>document.querySelector('#activityTableBody')?.textContent?.includes('Firewall'));
 assert.ok(!(await page.locator('#activityTableBody').textContent()).includes('DNSUSER'));
 await page.locator('[data-clear="all"]').click();await page.locator('#activityKind').selectOption('');
 await page.locator('[data-view="summary"]').click();await page.waitForSelector('.activity-rank');
 assert.equal(await page.locator('.activity-donut svg').count(),4);

 const ax=await new AxeBuilder({page}).include('#page-activity').withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(ax.violations.map(v=>v.id),[],'desktop accessibility');console.log('axe: desktop zero violations');
 await page.setViewportSize({width:375,height:900});
 await resetOverview();await page.locator('#activityServiceDonut').scrollIntoViewIfNeeded();
 const touchKey=await page.locator('#activityServiceDonut button[data-filter]').first().getAttribute('data-value');
 await clickSlice('#activityServiceDonut',touchKey,[],true);
 assert.equal(new URL(page.url()).searchParams.get('service'),touchKey,'touch did not select the slice');
 await clickSlice('#activityServiceDonut',touchKey,[],true);
 assert.equal(new URL(page.url()).searchParams.has('service'),false,'second touch did not clear');
 await page.screenshot({path:'/tmp/mikrodash-activity-mobile.png',fullPage:true});
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth),'mobile horizontal overflow');
 await page.evaluate(()=>document.documentElement.dataset.theme='light');await page.screenshot({path:'/tmp/mikrodash-activity-light.png',fullPage:true});
 const lightAxe=await new AxeBuilder({page}).include('#page-activity').withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(lightAxe.violations.map(v=>v.id),[],'mobile light accessibility');
 await page.keyboard.press('Tab');assert.ok(await page.evaluate(()=>document.activeElement!==document.body));
 assert.deepEqual(errors,[]);console.log('journey: source enable, archive import, user drilldown, CSV, rules, reclassification, URL reload, country/day/hour groups, four doughnuts, 375/1280, dark/light and keyboard passed');
 await browser.close();
})().catch(e=>{console.error(e);process.exit(1)});
