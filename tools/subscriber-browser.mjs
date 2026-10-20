// Browser acceptance uses an isolated synthetic TLS server, never a router.
import { createRequire } from 'node:module';
import { mkdirSync } from 'node:fs';
import assert from 'node:assert/strict';
const require = createRequire(process.env.SUBSCRIBER_BROWSER_MODULE + '/package.json');
const { chromium } = require('playwright');
const { default: AxeBuilder } = require('@axe-core/playwright');
const out = process.env.SUBSCRIBER_EVIDENCE_DIR;
mkdirSync(out, { recursive: true });
const browser = await chromium.launch({ headless: true, ...(process.env.SUBSCRIBER_CHROMIUM ? { executablePath: process.env.SUBSCRIBER_CHROMIUM } : {}) });
try {
 const context = await browser.newContext({ ignoreHTTPSErrors: true }); // Synthetic httptest certificate only.
 await context.addInitScript(() => { Object.defineProperty(window, 'sessionStorage', { get() { throw new DOMException('Synthetic storage restriction', 'SecurityError'); } }); });
 const page = await context.newPage();
 let connectRequests=0;
 page.on('request',r=>{if(r.method()==='POST'&&new URL(r.url()).pathname==='/api/connect')connectRequests++;});
 const errors = []; page.on('pageerror', err => errors.push(err.message));
 await page.goto(process.env.SUBSCRIBER_TEST_URL+'/?connect=C');
 await page.locator('#subscriberUsername').fill('synthetic-account');
 await page.locator('#subscriberPassword').fill('synthetic-password');
 assert.equal(await page.getByRole('button', { name: 'Sign in and connect', exact: true }).count(),1);
 await page.locator('#subscriberPassword').press('Enter');
 await page.locator('#portalAccount').waitFor({ state: 'visible' });
 await page.getByRole('heading', { name: 'Synthetic test', exact: true }).waitFor();
 assert.equal(connectRequests,0,'Shadow sign-in submitted a network request');
 assert.equal(await page.locator('#internetState').innerText(),'Internet connection not checked');
 await page.goto(process.env.SUBSCRIBER_TEST_URL+'/?connect=C');
 await page.locator('#portalAccount').waitFor({state:'visible'});
 await page.locator('#connectionState').filter({hasText:'Live updates connected'}).waitFor();
 assert.equal(connectRequests,0,'A GET landing hint and existing cookie opened Internet');
 await page.goto(process.env.SUBSCRIBER_TEST_URL+'/?connection='+'f'.repeat(64));
 await page.locator('#portalAccount').waitFor({state:'visible'});
 await page.locator('#connectionState').filter({hasText:'Live updates connected'}).waitFor();
 assert.equal(await page.locator('#internetState').innerText(),'Internet connection not checked');
 assert.equal(new URL(page.url()).search,'','Connection reference remained in the visible URL');
 assert.equal(connectRequests,0,'An unowned return reference submitted a network request');
 if (process.env.SUBSCRIBER_PENDING_RECEIPT === 'true') {
  await page.locator('#portalNotice').filter({hasText:'Accounting reconciliation required (1 pending)'}).waitFor();
  const snapshot=await (await context.request.get(process.env.SUBSCRIBER_TEST_URL+'/api/account')).json();
  assert.equal(snapshot.pendingAccounting,1);
  assert.equal(snapshot.quality,'reconciling');
  assert.equal(snapshot.decision.allowed,false);
  assert.equal(snapshot.lifetimeSeconds,'20');
  await page.locator('#subscriberLimits').getByText('00:00:20',{exact:true}).waitFor();
  await page.locator('#subscriberLimits').getByText('00:59:40',{exact:true}).first().waitFor();
  assert.doesNotMatch(await page.locator('#subscriberLimits').innerText(),/Unknown/);
 }
 await page.getByRole('button', { name: 'Fast', exact: true }).click();
 await page.getByText('Speed selected. Reconnect a device').waitFor();
 assert.match(await page.locator('#speedState').innerText(), /pending-reconnect/);
 await page.getByRole('button', { name: 'Connect to internet' }).focus();
 await page.keyboard.press('Enter');
 await page.getByText('Internet connection is not enabled in shadow mode').waitFor();
 for (const width of [375, 1280]) {
  await page.setViewportSize({width, height:900});
  for (const theme of ['blue','green','purple']) {
   await page.locator('#portalTheme').selectOption(theme);
   assert.equal(await page.locator('html').getAttribute('dir'),'ltr');
   const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1);
   assert.equal(overflow,false,`overflow at ${width}`);
   await page.screenshot({path:`${out}/subscriber-synthetic-${width}-${theme}.png`,fullPage:true});
  }
  const result=await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();
  assert.deepEqual(result.violations.map(v=>({id:v.id, impact:v.impact, nodes:v.nodes.map(n=>n.target)})),[]);
 }
 assert.deepEqual(errors,[]);
 await page.getByRole('button',{name:'Sign out of portal'}).click();
 await page.locator('#portalLogin').waitFor({state:'visible'});
 assert.equal(await page.evaluate(()=>document.activeElement?.id),'subscriberUsername');
 assert.equal((await context.request.get(process.env.SUBSCRIBER_TEST_URL+'/api/account')).status(),401);
 await context.close();
 console.log('Subscriber browser acceptance passed: login, speed persistence, shadow refusal, logout, LTR, 375/1280, 3 themes and axe.');
} finally { await browser.close(); }
