// Real protected HTTP/SQLite; every identity, credential and audit fixture is synthetic.
import assert from 'node:assert/strict';
import {createRequire} from 'node:module';
import {mkdirSync,readFileSync,writeFileSync} from 'node:fs';
const require=createRequire(process.env.SUBSCRIBER_BROWSER_MODULE+'/package.json');
const {chromium}=require('playwright'),{default:AxeBuilder}=require('@axe-core/playwright');
const out=process.env.SUBSCRIBER_EVIDENCE_DIR;mkdirSync(out,{recursive:true});
const geometrySource=readFileSync(new URL('./form-geometry.js',import.meta.url),'utf8');
const browser=await chromium.launch({headless:true,...(process.env.SUBSCRIBER_CHROMIUM?{executablePath:process.env.SUBSCRIBER_CHROMIUM}:{})});
let page;let stage='setup';
try{
 const context=await browser.newContext({ignoreHTTPSErrors:true});const origin=process.env.SUBSCRIBER_TEST_URL,id=process.env.SUBSCRIBER_ACCOUNT_ID,other=process.env.SUBSCRIBER_OTHER_ACCOUNT_ID;
 const cookie=async value=>context.addCookies([{name:'mikrodash_sid',value,url:origin,httpOnly:true,secure:true,sameSite:'Lax'}]);await cookie('synthetic-token');
 page=await context.newPage();const errors=[];let auditRequests=0;page.on('pageerror',error=>errors.push(error.message));page.on('request',r=>{if(new URL(r.url()).pathname==='/api/subscribers/audit'){assert.equal(r.method(),'GET');auditRequests++;}});
 await page.setViewportSize({width:1280,height:1000});await page.goto(origin+'/subscribers');
 const open=id=>page.locator(`[data-sub-audit="${id}"]`).click();const modal=page.locator('#subscriberAuditModal'),results=modal.locator('[data-audit-results]'),rows=results.locator('[data-audit-event]');
 const category=modal.getByLabel('Change category',{exact:true}),from=modal.getByLabel('From (UTC)',{exact:true}),until=modal.getByLabel('Until (UTC)',{exact:true});
 const showFilters=async()=>{const d=modal.locator('[data-audit-filters]');if(!await d.evaluate(n=>n.open))await d.locator('summary').click();};
 const apply=()=>modal.getByRole('button',{name:'Apply filters',exact:true}).click();const refresh=()=>modal.getByRole('button',{name:'Refresh audit',exact:true}).click();
 const loaded=count=>page.waitForFunction(({count})=>{const r=document.querySelector('#subscriberAuditModal [data-audit-results]');return r?.getAttribute('aria-busy')==='false'&&r.querySelectorAll('[data-audit-event]').length===count;},{count});
 stage='list and pagination';await open(id);await loaded(50);assert.match(await results.innerText(),/9007199254741049/);assert.ok(!(await results.innerText()).includes('Synthetic foreign private reason'));
 await modal.getByRole('button',{name:'Load older audit events',exact:true}).click();await loaded(59);assert.equal(await modal.getByRole('button',{name:'Load older audit events',exact:true}).count(),0);
 assert.equal(await modal.locator('[data-audit-heading]').evaluate(n=>n===document.activeElement),true,'terminal page lost keyboard focus');
 assert.match(await results.innerText(),/Account created/);assert.equal(await rows.count(),59);
 stage='historical evidence';const nameRow=rows.filter({hasText:'Display name changed'});await nameRow.getByText('Evidence & change details',{exact:true}).click();assert.match(await nameRow.innerText(),/Before: Synthetic fixture/);assert.match(await nameRow.innerText(),/After: Synthetic اسم <script>/);
 const credential=rows.filter({hasText:'Password reset'});await credential.getByText('Evidence & change details',{exact:true}).click();assert.match(await credential.innerText(),/Credential revision: 0 → 1/);assert.ok(!(await results.innerText()).includes('synthetic-audit-browser-password'));
 assert.equal(await results.locator('script').count(),0);assert.equal(await page.evaluate(()=>window.syntheticAuditExecuted),undefined);
 stage='server date and category filters';await showFilters();const beforeInvalid=auditRequests;await from.fill('2026-11-15');await until.fill('2026-11-14');await apply();assert.equal(auditRequests,beforeInvalid);assert.equal(await until.evaluate(n=>n.validity.customError),true);
 await from.fill('1999-01-01');await until.fill('1999-01-02');await apply();await loaded(0);await results.getByText('No audit events match these filters.',{exact:true}).first().waitFor();
 await showFilters();await from.fill('');await until.fill('');await category.selectOption('credentials');await apply();await loaded(1);assert.match(await rows.innerText(),/Password reset/);
 // Read-only filters close directly even after changes; edit dialogs retain their dirty guard.
 await page.keyboard.press('Escape');await modal.waitFor({state:'hidden'});assert.equal(await page.locator(`[data-sub-audit="${id}"]`).evaluate(n=>n===document.activeElement),true);assert.equal(await page.getByRole('button',{name:'Discard changes',exact:true}).isVisible(),false);
 stage='account switch races';
 for(const failure of [false,true]){
  let release,ready;const gate=new Promise(resolve=>{release=resolve;}),fetched=new Promise(resolve=>{ready=resolve;});
  await page.route('**/api/subscribers/audit?*',async route=>{const response=await route.fetch();assert.equal(response.status(),200);ready();await gate;if(failure)await route.abort('connectionreset');else await route.fulfill({response});},{times:1});
  await open(id);await fetched;
  try{await page.keyboard.press('Escape');await modal.waitFor({state:'hidden'});await open(other);await loaded(1);assert.match(await modal.innerText(),/synthetic-audit-other/);assert.equal(await rows.locator('td').filter({hasText:'Password reset'}).count(),0);}finally{release();}
  await page.waitForTimeout(100);assert.match(await modal.innerText(),/synthetic-audit-other/);assert.ok(!(await results.innerText()).includes('Retry page'));await page.keyboard.press('Escape');await modal.waitFor({state:'hidden'});
 }
 stage='filter race';await open(id);await loaded(50);
 let release,ready;const gate=new Promise(resolve=>{release=resolve;}),fetched=new Promise(resolve=>{ready=resolve;});
 await page.route('**/api/subscribers/audit?*',async route=>{const response=await route.fetch();ready();await gate;await route.fulfill({response});},{times:1});
 await refresh();await fetched;
 try{await showFilters();await category.selectOption('credentials');await apply();await loaded(1);}finally{release();}
 await page.waitForTimeout(100);assert.equal(await rows.count(),1);assert.match(await results.innerText(),/Applied filter: Credentials/);
 stage='real authority errors and retry';
 for(const [value,code]of [['synthetic-viewer-token',403],['synthetic-expired-token',401]]){
  await cookie(value);const denied=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/subscribers/audit');await refresh();assert.equal((await denied).status(),code);await loaded(0);await modal.getByRole('button',{name:'Retry page',exact:true}).waitFor();
  await cookie('synthetic-token');await modal.getByRole('button',{name:'Retry page',exact:true}).click();await loaded(1);
 }
 stage='real validation error and interrupted older page';await showFilters();await category.selectOption('all');await apply();await loaded(50);
 await page.route('**/api/subscribers/audit?*',async route=>{const url=new URL(route.request().url());url.searchParams.set('category','synthetic-invalid');const response=await route.fetch({url:url.toString()});assert.equal(response.status(),400);await route.fulfill({response});},{times:1});
 await refresh();await loaded(0);await modal.getByRole('button',{name:'Retry page',exact:true}).click();await loaded(50);
 const tableScroll=results.locator('.data-scroll');await tableScroll.evaluate(n=>n.scrollTop=200);const oldTop=await tableScroll.evaluate(n=>n.scrollTop);assert.ok(oldTop>0);
 await page.route('**/api/subscribers/audit?*',route=>route.abort('connectionreset'),{times:1});await modal.getByRole('button',{name:'Load older audit events',exact:true}).click();await modal.getByRole('button',{name:'Retry page',exact:true}).waitFor();assert.equal(await rows.count(),50);assert.equal(await tableScroll.evaluate(n=>n.scrollTop),oldTop,'failed older page lost reading position');
 await modal.getByRole('button',{name:'Retry page',exact:true}).click();await loaded(59);
 stage='visual, keyboard and accessibility';await refresh();await loaded(50);await rows.filter({hasText:'Display name changed'}).getByText('Evidence & change details',{exact:true}).click();
 await modal.getByRole('button',{name:'Close',exact:true}).focus();await page.keyboard.press('Shift+Tab');assert.equal(await modal.getByRole('button',{name:'Cancel',exact:true}).evaluate(n=>n===document.activeElement),true);await page.keyboard.press('Tab');assert.equal(await modal.getByRole('button',{name:'Close',exact:true}).evaluate(n=>n===document.activeElement),true);
 const evidence=[];
 for(const appearance of [{theme:'dark',palette:''},{theme:'light',palette:''},{theme:'dark',palette:'nord'}]){
  await page.evaluate(({theme,palette})=>{document.documentElement.dataset.theme=theme;document.documentElement.dataset.bsTheme=theme;if(palette)document.documentElement.dataset.palette=palette;else delete document.documentElement.dataset.palette;},appearance);
  for(const width of [375,768,1280,1920]){
   await page.setViewportSize({width,height:1000});assert.equal(await page.locator('html').getAttribute('dir'),'ltr');assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1),false,'page overflow');assert.equal(await modal.locator('[role=dialog]').evaluate(n=>n.scrollWidth>n.clientWidth+1),false,'modal overflow');
   await showFilters();const geometry=await page.evaluate(geometrySource);assert.ok(geometry.fields>=3);assert.equal(geometry.ok,true,JSON.stringify(geometry.issues));
   if(width===375&&appearance.theme==='dark'&&!appearance.palette){await category.evaluate(n=>n.style.height='50px');assert.equal((await page.evaluate(geometrySource)).ok,false,'geometry accepted broken input');await category.evaluate(n=>n.style.removeProperty('height'));assert.equal((await page.evaluate(geometrySource)).ok,true);}
   const audit=await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(audit.violations,[]);
   await modal.locator('[data-audit-filters]').evaluate(n=>n.open=false);await modal.locator('.rtr-modal-body').evaluate(n=>n.scrollTop=0);
   if(width===375){assert.equal(await results.locator('[data-audit-card]').first().isVisible(),true);assert.equal(await rows.first().isVisible(),false);}else{assert.equal(await rows.first().isVisible(),true);}
   const file=`subscriber-audit-synthetic-${appearance.theme}-${appearance.palette||'default'}-${width}.png`;await page.screenshot({path:out+'/'+file,fullPage:true});evidence.push({...appearance,width,file,axeViolations:0,geometry:true});
  }
 }
 // Native disclosure is operable without adding a second dialog or focus model.
 await page.setViewportSize({width:1280,height:1000});const disclosure=rows.filter({hasText:'Display name changed'}).getByText('Evidence & change details',{exact:true});await disclosure.focus();await page.keyboard.press('Enter');assert.equal(await disclosure.evaluate(n=>n.parentElement.open),false);await page.keyboard.press('Enter');assert.equal(await disclosure.evaluate(n=>n.parentElement.open),true);
 await page.keyboard.press('Escape');await modal.waitFor({state:'hidden'});await page.locator(`[data-sub-view="${id}"]`).first().click();await page.getByRole('button',{name:'Open account audit',exact:true}).click();await loaded(50);assert.equal(await page.locator('#subscriberDetailsModal').isVisible(),false);
 assert.equal(await page.evaluate(()=>window.syntheticAuditExecuted),undefined);assert.deepEqual(errors,[]);
 writeFileSync(out+'/audit-browser-results.json',JSON.stringify({status:'passed',source:'real protected HTTP/SQLite; synthetic identities and history',scenarios:['exact keyset and terminal focus','owned historical subscriptions','escaped original evidence','server filters and invalid range','readonly Escape/focus','late account success and failure','filter race','real 401/403/400 and retry','interrupted older page retains rows','native disclosure','shared detail link'],evidence},null,2));
 console.log(JSON.stringify({status:'passed',screenshots:evidence.length,axeCases:evidence.length,rows:59,source:'real protected HTTP/SQLite; synthetic identities/history'}));
}catch(error){if(page){await page.screenshot({path:out+'/audit-failure.png',fullPage:true}).catch(()=>{});writeFileSync(out+'/audit-failure.json',JSON.stringify({stage,message:error.message},null,2));}throw error;}finally{await browser.close();}
