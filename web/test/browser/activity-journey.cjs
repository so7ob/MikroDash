const {chromium}=require('playwright');
const {AxeBuilder}=require('@axe-core/playwright');
const assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true});
 const context=await browser.newContext({viewport:{width:1280,height:1000},acceptDownloads:true});
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
  await resetOverview();const button=page.locator(selector+' [data-filter]').first();
  const filter=await button.getAttribute('data-filter'),value=await button.getAttribute('data-value');
  await button.click();await settled();
  assert.equal(await page.locator('[data-view="summary"]').getAttribute('aria-selected'),'true',selector+' ordinary click left Overview');
  assert.equal(new URL(page.url()).searchParams.get(filter),value);
  assert.equal(await page.locator('#activityOverview').isVisible(),true);
  if(filter==='country')assert.equal(await page.locator('#activityKind').inputValue(),'firewall');
  await button.click({modifiers:['Control']});await settled();
  assert.equal(await page.locator('[data-view="events"]').getAttribute('aria-selected'),'true',selector+' Ctrl did not open Events');
  assert.equal(new URL(page.url()).searchParams.get(filter),value);assert.equal(context.pages().length,1);
 }
 await resetOverview();
 const timebar=page.locator('#activityTimeline button').filter({has:page.locator('span')});
 // Pick an occupied bucket so filtering has visible evidence.
 const index=await page.locator('#activityTimeline button').evaluateAll(nodes=>{const heights=nodes.map(node=>parseFloat(node.style.getPropertyValue('--activity-height')));return heights.indexOf(Math.max(...heights));});
 assert.ok(index>=0,'no time buckets were measured');
 await timebar.nth(index).click();await settled();
 assert.equal(await page.locator('[data-view="summary"]').getAttribute('aria-selected'),'true','time bar left Overview');
 const range=new URL(page.url()).searchParams;assert.ok(range.has('from')&&range.has('to'));
 await page.locator('#activityTimeline button').first().click({modifiers:['Control']});await settled();
 assert.equal(await page.locator('[data-view="events"]').getAttribute('aria-selected'),'true','Ctrl time bar did not open Events');
 assert.equal(new URL(page.url()).searchParams.get('from'),range.get('from'));
 assert.equal(new URL(page.url()).searchParams.get('to'),range.get('to'));
 await page.locator('#activityDays').selectOption('1');await resetOverview();
 // Native keyboard activation has the same default and modifier behavior.
 await page.locator('#activityServiceDonut [data-filter]').first().focus();await page.keyboard.press('Enter');await settled();
 assert.equal(await page.locator('[data-view="summary"]').getAttribute('aria-selected'),'true','Enter left Overview');
 await page.locator('#activityServiceDonut [data-filter]').first().focus();await page.keyboard.press('Control+Enter');await settled();
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
 await page.setViewportSize({width:375,height:900});await page.screenshot({path:'/tmp/mikrodash-activity-mobile.png',fullPage:true});
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth),'mobile horizontal overflow');
 await page.evaluate(()=>document.documentElement.dataset.theme='light');await page.screenshot({path:'/tmp/mikrodash-activity-light.png',fullPage:true});
 const lightAxe=await new AxeBuilder({page}).include('#page-activity').withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(lightAxe.violations.map(v=>v.id),[],'mobile light accessibility');
 await page.keyboard.press('Tab');assert.ok(await page.evaluate(()=>document.activeElement!==document.body));
 assert.deepEqual(errors,[]);console.log('journey: source enable, archive import, user drilldown, CSV, rules, reclassification, URL reload, country/day/hour groups, four doughnuts, 375/1280, dark/light and keyboard passed');
 await browser.close();
})().catch(e=>{console.error(e);process.exit(1)});
