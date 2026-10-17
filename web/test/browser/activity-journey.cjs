const {chromium}=require('playwright');
const {AxeBuilder}=require('@axe-core/playwright');
const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true});
 const context=await browser.newContext({viewport:{width:1280,height:1000},acceptDownloads:true,hasTouch:true,timezoneId:'UTC'});
 await context.addCookies([{name:'mikrodash_sid',value:'activity-test-token',url:process.argv[2]}]);
 const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(process.argv[2]+'/journey');await page.waitForFunction(()=>document.querySelector('#activityState')?.textContent?.includes('disabled'));
 const settled=()=>page.waitForFunction(()=>document.querySelector('#page-activity')?.getAttribute('aria-busy')==='false');
 for(const tab of ['summary','domains','clients','events','rules']){
  await page.locator(`[data-view="${tab}"]`).click();await settled();
  assert.equal(await page.locator('#activityState').isVisible(),false,`${tab} exposes synchronization status`);
  assert.equal(await page.locator('#activityConfig').isVisible(),false,`${tab} exposes synchronization settings`);
 }
 await page.locator('#activityDays').selectOption('7');await settled();
 await page.locator('[data-view="sync"]').click();await settled();
 assert.equal(await page.locator('#activitySyncPane').isVisible(),true);
 assert.equal(await page.locator('#activityFolder').isVisible(),true,'settings require a second disclosure');
 for(const id of ['activityOverview','activityTablePane','activityRulesPane'])assert.equal(await page.locator('#'+id).isVisible(),false);
 await page.reload();await settled();
 assert.equal(new URL(page.url()).searchParams.get('tab'),'sync');
 assert.equal(new URL(page.url()).searchParams.get('routerId'),'r1');
 assert.equal(await page.locator('#activityDays').inputValue(),'7','tab reload lost the period');
 assert.equal(await page.locator('[data-view="sync"]').getAttribute('aria-selected'),'true');
 await page.locator('[data-view="sync"]').focus();await page.keyboard.press('Home');await settled();
 assert.equal(await page.locator('[data-view="summary"]').evaluate(e=>e===document.activeElement),true);
 await page.keyboard.press('End');await settled();
 assert.equal(await page.locator('[data-view="sync"]').evaluate(e=>e===document.activeElement),true);
 assert.equal(await page.locator('#activityState').isVisible(),true);
 await page.route('**/api/activity/query?**',async route=>{
  const response=await route.fetch();const data=await response.json();data.canWrite=false;await route.fulfill({response,json:data});
 });
 await page.locator('#activityRefresh').click();await settled();
 for(const id of ['activityEnabled','activityFolder','activityTimezone','activitySaveSettings'])assert.equal(await page.locator('#'+id).isDisabled(),true,'read-only source controls enabled');
 await page.unroute('**/api/activity/query?**');await page.locator('#activityRefresh').click();await settled();
 await page.locator('#activityEnabled').check();await page.locator('#activityTimezone').fill('UTC');await page.locator('#activitySaveSettings').click();
 await page.waitForTimeout(1200);await page.evaluate(()=>window.dispatchEvent(new Event('fixture-invalidate')));
 await page.waitForFunction(()=>document.querySelector('#activityCount')?.textContent==='130');
 await page.locator('#activityDays').selectOption('1');await settled();
 await page.locator('[data-view="summary"]').click();await settled();
 assert.equal(await page.locator('#activityState').isVisible(),false,'source save exposed status outside sync tab');
 await page.screenshot({path:'/tmp/mikrodash-activity-desktop.png',fullPage:true});
 // Every overview chart/ranking filters in place; only Ctrl opens Events.
 const resetOverview=async()=>{
  await page.locator('[data-view="summary"]').click();
  if(await page.locator('[data-clear="all"]').count())await page.locator('[data-clear="all"]').click();
  if(await page.locator('#activityKind').inputValue())await page.locator('#activityKind').selectOption('');
  await page.waitForFunction(()=>document.querySelector('#activityCount')?.textContent==='130'&&document.querySelector('#page-activity')?.getAttribute('aria-busy')==='false');
 };
 // Shared colored filter chips stay stable across tabs; Clear removes all of them.
 await page.locator('#activityServices [data-filter="service"]').first().click();await settled();
 await page.locator('#activityClients [data-filter="client"]').first().click();await settled();
 await page.locator('#activityCategory').selectOption('AI');await settled();
 const chipColors=async()=>page.locator('#activityFilters .chip').evaluateAll(es=>es.map(e=>getComputedStyle(e).color));
 assert.equal(await page.locator('#activityFilters .chip.is-on').count(),3);
 const colors=await chipColors();assert.equal(new Set(colors).size,3,'selected filters share one color');
 assert.equal(await page.locator('[data-clear="all"]').textContent(),'Clear');
 assert.ok(!colors.includes(await page.locator('[data-clear="all"]').evaluate(e=>getComputedStyle(e).color)),'Clear is not distinct');
 await page.locator('[data-view="sync"]').click();await settled();
 assert.deepEqual(await chipColors(),colors,'tab switch changed filter colors');
 assert.equal(await page.locator('#activityState').isVisible(),true);
 const syncAxe=await new AxeBuilder({page}).include('#page-activity').withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(syncAxe.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)})),[],'synchronization/filter accessibility');
 await page.screenshot({path:'/tmp/mikrodash-activity-sync-desktop.png',fullPage:true});
 await page.locator('[data-clear="service"]').focus();await page.keyboard.press('Enter');await settled();
 assert.equal(new URL(page.url()).searchParams.has('service'),false);
 assert.equal(await page.locator('#activityFilters .chip').count(),2);
 await page.locator('[data-clear="all"]').click();await settled();
 assert.equal(await page.locator('#activityFilters button').count(),0);
 await page.locator('[data-view="summary"]').click();await settled();
 assert.equal(await page.locator('#activityCount').textContent(),'130');
 console.log('synchronization tab: visibility, save/import, URL/period/router persistence, keyboard, read-only controls and colored removable filters passed');
 // Missing GeoIP keeps the answer evidence visible and labels its units honestly.
 assert.equal(await page.locator('#activityCountryBasis').inputValue(),'dns');
 assert.match(await page.locator('#activityGeoNote').textContent(),/database unavailable/);
 assert.equal(await page.locator('#activityGeoSetup').isVisible(),true);
 assert.equal(await page.locator('#activityCountryDonut .activity-donut-total').textContent(),'258');
 assert.equal(await page.locator('#activityCountryDonut .activity-donut-caption').textContent(),'answers');
 await page.locator('#activityCountry').selectOption('Unknown');await settled();
 assert.equal(await page.locator('#activityCount').textContent(),'86','DNS country did not filter answered events');
 assert.equal(await page.locator('#activityKind').inputValue(),'');
 await page.locator('#activityCountryDonut button[data-value="Unknown"]').click();await settled();
 assert.equal(await page.locator('#activityCount').textContent(),'130');
 await page.locator('#activityCountryBasis').selectOption('firewall');await settled();
 assert.equal(await page.locator('#activityCountryDonut .activity-donut-total').textContent(),'1');
 assert.equal(await page.locator('#activityCountryDonut .activity-donut-caption').textContent(),'events');
 await page.locator('#activityCountryDonut button[data-value="Unknown"]').click();await settled();
 assert.equal(await page.locator('#activityCount').textContent(),'0');
 await page.reload();await settled();
 assert.equal(await page.locator('#activityCountryBasis').inputValue(),'firewall','country source not persisted in URL');
 await page.locator('#activityCountryDonut button[data-value="Unknown"]').click();await settled();
 assert.equal(await page.locator('#activityCount').textContent(),'130');
 await page.locator('#activityCountryBasis').selectOption('dns');await settled();
 console.log('country sources: missing GeoIP/Unknown, DNS answer units and event filter, Firewall units and filter, source URL restoration passed');
 // Enriched DNS evidence is paginated/sorted through the real API.
 assert.deepEqual(await page.locator('#activityDNSMetrics strong').allTextContents(),['86','81','9','2']);
 await page.locator('.activity-dns-panel').screenshot({path:'/tmp/mikrodash-dns-evidence-panel.png'});
 assert.equal(await page.locator('#activityEvidenceBody tr').count(),10);
 await page.locator('#activityEvidenceNext').click();await settled();
 assert.equal(await page.locator('#activityEvidenceBody tr').count(),10);
 assert.match(await page.locator('#activityEvidencePage').textContent(),/^2 \/ 9/);
 await page.locator('#activityEvidencePrev').click();await settled();
 await page.locator('#activityEvidenceHead th').first().click();await settled();
 const sorted=await page.locator('#activityEvidenceBody button[data-filter]').allTextContents();
 assert.deepEqual(sorted,[...sorted].sort(),'evidence table did not sort by value');
 for(const group of ['dnsIP','deviceName','canonical','domain','server']){
  await resetOverview();await page.locator('#activityEvidenceGroup').selectOption(group);await settled();
  const button=group==='domain' ? page.locator('#activityEvidenceBody button[data-value="youtubei.googleapis.com"]') : page.locator('#activityEvidenceBody button[data-filter]').first();
  const value=await button.getAttribute('data-value');assert.ok(value,group+' has no grouping rows');
  await button.click();await settled();
  assert.equal(new URL(page.url()).searchParams.get(group),value);
  assert.equal(await page.locator('[data-view="summary"]').getAttribute('aria-selected'),'true');
  await button.click();await settled();
  assert.equal(new URL(page.url()).searchParams.has(group),false,group+' evidence did not toggle off');
  assert.equal(await page.locator('#activityCount').textContent(),'130');
  await button.click();await settled();await button.click({modifiers:['Control']});await settled();
  assert.equal(await page.locator('[data-view="events"]').getAttribute('aria-selected'),'true');
  assert.ok(await page.locator('#activityTableBody .activity-dns-details').count(),'event details omitted');
  for(const key of ['canonical','deviceName','ipBasis','dnsIP','dnsCountry'])assert.ok(await page.locator('#activityTableBody [data-filter="'+key+'"]').count(),group+' events omitted '+key);
  assert.match(await page.locator('#activityTableBody').textContent(),/2001:db8::/);
  assert.match(await page.locator('#activityTableBody').textContent(),/198\.51\.100\./);
  if(group==='dnsIP'){
   await page.locator('#activityTableBody tr').first().screenshot({path:'/tmp/mikrodash-dns-evidence-event.png'});
   const eventAxe=await new AxeBuilder({page}).include('#page-activity').withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();
   assert.deepEqual(eventAxe.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)})),[],'DNS event accessibility');
  }
  const link=await page.locator('#activityExport').getAttribute('href');const csv=await context.request.get(process.argv[2]+link);
  assert.equal(csv.status(),200);assert.match(await csv.text(),/canonical,dns_ips,ip_basis,device_name/);
 }
 await resetOverview();
 await page.locator('#activitySearch').fill('2001:db8::');await page.waitForFunction(()=>document.querySelector('#activityCount')?.textContent==='86');await settled();
 await page.locator('#activitySearch').fill('');await page.waitForFunction(()=>document.querySelector('#activityCount')?.textContent==='130');await settled();
 // The new fields do not make legacy messages appear enriched.
 await page.locator('#activityIPBases button[data-value="-"]').click();await settled();
 assert.equal(await page.locator('#activityCount').textContent(),'44');
 await page.locator('#activityEvidenceGroup').selectOption('canonical');await settled();
 assert.match(await page.locator('#activityEvidenceBody').textContent(),/Older DNSUSER/);
 const legacyAxe=await new AxeBuilder({page}).include('#page-activity').withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();
 assert.deepEqual(legacyAxe.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)})),[],'legacy evidence empty-state accessibility');
 await page.locator('#activityIPBases button[data-value="-"]').click({modifiers:['Control']});await settled();
 assert.equal(await page.locator('#activityTableBody [data-filter="dnsIP"]').count(),0);
 assert.match(await page.locator('#activityTableBody').textContent(),/Not recorded/);
 await resetOverview();await page.locator('#activityEvidenceGroup').selectOption('dnsIP');await settled();
 console.log('DNS evidence: event fields/IPv4/IPv6, five overview groups, pagination, sorting, toggles/Ctrl, search, CSV, Arabic and missing legacy values passed');
 for(const selector of ['#activityCategories','#activityServiceDonut','#activityUserDonut','#activityCountryDonut','#activityServices','#activityClients','#activityQueryTypes','#activityMatchMethods','#activityIPBases']){
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
  await page.locator(selector+' svg').scrollIntoViewIfNeeded();
  const point=await page.locator(selector+' svg').evaluate((svg,key)=>{
   const path=Array.from(svg.querySelectorAll('path')).find(p=>p.dataset.value===key);
   if(!path)throw new Error('slice missing: '+key);
   const p=path.getPointAtLength(path.getTotalLength()/2).matrixTransform(path.getScreenCTM());
   const hit=document.elementFromPoint(p.x,p.y);
   if(hit!==path)throw new Error('overlapping or unreachable slice: '+key+' at '+p.x+','+p.y+'; hit '+(hit?.outerHTML.slice(0,300)||'outside viewport'));
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
 await resetOverview();await page.locator('#activityCountryBasis').selectOption('firewall');await settled();await page.locator('#activityKind').selectOption('firewall');await settled();
 await page.locator('#activityCountryDonut button[data-filter]').first().click();await settled();
 await page.reload();await settled();
 await page.locator('#activityCountryDonut button[data-filter]').first().click();await settled();
 assert.equal(new URL(page.url()).searchParams.has('country'),false);
 assert.equal(await page.locator('#activityKind').inputValue(),'firewall');
 await resetOverview();await page.locator('#activityCountryBasis').selectOption('dns');await settled();
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
 console.log('chart selection: nine legends/rankings, time range, country evidence, in-place filtering, Ctrl drilldown and Enter/Ctrl+Enter passed');
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
 await page.locator('#activityGroup').selectOption('dnsCountry');await settled();
 assert.match(await page.locator('#activityTableHead').textContent(),/DNS answers/);
 assert.match(await page.locator('#activityTableBody').textContent(),/258/);
 await page.locator('#activityTableBody [data-filter="dnsCountry"]').click();await settled();
 assert.equal(await page.locator('#activityCount').textContent(),'86');
 assert.doesNotMatch(await page.locator('#activityTableBody').textContent(),/Firewall/);
 await page.locator('[data-clear="all"]').click();await settled();await page.locator('[data-view="domains"]').click();await settled();
 await page.locator('#activityGroup').selectOption('country');await page.waitForFunction(()=>document.querySelector('#activityTableBody')?.textContent?.includes('Unknown'));
 await page.locator('#activityTableBody [data-filter="country"]').click();await page.waitForFunction(()=>document.querySelector('#activityTableBody')?.textContent?.includes('Firewall'));
 assert.ok(!(await page.locator('#activityTableBody').textContent()).includes('DNSUSER'));
 await page.locator('[data-clear="all"]').click();await page.locator('#activityKind').selectOption('');
 await page.locator('[data-view="summary"]').click();await page.waitForSelector('.activity-rank');
 assert.equal(await page.locator('.activity-donut svg').count(),7);

 const ax=await new AxeBuilder({page}).include('#page-activity').withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(ax.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)})),[],'desktop accessibility');console.log('axe: desktop zero violations');
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
 await page.locator('#activityServices [data-filter="service"]').first().click();await settled();
 await page.locator('#activityClients [data-filter="client"]').first().click();await settled();
 await page.locator('#activityCategory').selectOption('AI');await settled();
 await page.locator('[data-view="sync"]').click();await settled();
 assert.equal(await page.locator('#activityState').isVisible(),true);
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth),'mobile synchronization/filter overflow');
 const syncLightAxe=await new AxeBuilder({page}).include('#page-activity').withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(syncLightAxe.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)})),[],'mobile light synchronization/filter accessibility');
 await page.screenshot({path:'/tmp/mikrodash-activity-sync-mobile-light.png',fullPage:true});
 await page.evaluate(()=>{delete document.documentElement.dataset.theme;document.documentElement.dataset.palette='nord'});
 const syncNordAxe=await new AxeBuilder({page}).include('#page-activity').withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(syncNordAxe.violations.map(v=>v.id),[],'Nord synchronization/filter accessibility');
 await page.locator('[data-view="sync"]').focus();await page.keyboard.press('ArrowRight');await settled();
 assert.equal(await page.locator('[data-view="summary"]').getAttribute('aria-selected'),'true');
 assert.equal(await page.locator('#activityState').isVisible(),false);
 await page.keyboard.press('Tab');assert.ok(await page.evaluate(()=>document.activeElement!==document.body));
 assert.deepEqual(errors,[]);console.log('journey: source enable, archive import, user drilldown, CSV, rules, reclassification, URL reload, country/day/hour groups, seven doughnuts, 375/1280, dark/light and keyboard passed');
 await browser.close();
})().catch(e=>{console.error(e);process.exit(1)});
