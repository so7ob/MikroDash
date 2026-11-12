// Protected HTTP/SQLite browser acceptance on an isolated synthetic TLS server.
// No router or operator account is used. QR images are decoded by the shipped jsQR.
import { createRequire } from 'node:module';
import { mkdirSync } from 'node:fs';
import assert from 'node:assert/strict';
const require = createRequire(process.env.SUBSCRIBER_BROWSER_MODULE + '/package.json');
const { chromium } = require('playwright');
const { default: AxeBuilder } = require('@axe-core/playwright');
const QRCode = require('qrcode');
const out = process.env.SUBSCRIBER_EVIDENCE_DIR, origin = process.env.SUBSCRIBER_TEST_URL;
mkdirSync(out, { recursive: true });
const browser = await chromium.launch({ headless: true, ...(process.env.SUBSCRIBER_CHROMIUM ? { executablePath: process.env.SUBSCRIBER_CHROMIUM } : {}) });
try {
 const context = await browser.newContext({ ignoreHTTPSErrors: true }); // httptest certificate only.
 await context.addInitScript(() => { Object.defineProperty(window, 'sessionStorage', { get() { throw new DOMException('Synthetic storage restriction', 'SecurityError'); } }); });
 const page = await context.newPage(); page.setDefaultTimeout(12000);
 let connectRequests=0, loginRequests=0, speedRequests=0;
 page.on('request',r=>{if(r.method()==='POST'){const p=new URL(r.url()).pathname;if(p==='/api/connect')connectRequests++;if(p==='/api/login')loginRequests++;if(p==='/api/speed')speedRequests++;}});
 const errors = [], csp=[]; page.on('pageerror', err => errors.push(err.message));
 await page.addInitScript(()=>document.addEventListener('securitypolicyviolation', e=>console.error('CSP:'+e.violatedDirective)));
 page.on('console',message=>{if(message.text().startsWith('CSP:'))csp.push(message.text());});
 async function audit(name) {
  assert.equal(await page.locator('html').getAttribute('lang'),'ar');
  assert.equal(await page.locator('html').getAttribute('dir'),'rtl');
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1),false,`overflow ${name}`);
  const result=await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();
  assert.deepEqual(result.violations.map(v=>({id:v.id,impact:v.impact,nodes:v.nodes.map(n=>n.target)})),[],name);
  await page.screenshot({path:`${out}/${name}.png`,fullPage:true});
 }
 await page.goto(origin+'/?connect=C');
 await page.evaluate(()=>document.fonts.ready);
 assert.ok(await page.evaluate(()=>document.fonts.check('16px Tajawal')),'Tajawal did not load');
 for (const width of [375,1280]) { await page.setViewportSize({width,height:900}); await audit(`login-${width}`); }
 assert.equal(await page.locator('#subscriberPassword').isDisabled(),true);
 await page.locator('#portalQR').focus(); await page.keyboard.press('Enter');
 await page.locator('#portalQRDialog').waitFor({state:'visible'});
 for(let i=0;i<8;i++) { await page.keyboard.press('Tab'); assert.ok(await page.evaluate(()=>document.querySelector('#portalQRDialog').contains(document.activeElement)),'QR focus escaped modal'); }
 await audit('qr-dialog'); await page.keyboard.press('Escape');
 assert.equal(await page.evaluate(()=>document.activeElement?.id),'portalQR');
 // Synthetic permission race: a stream arriving after close must be stopped.
 await page.evaluate(() => {
  window.__cameraStops=0;
  Object.defineProperty(navigator.mediaDevices,'getUserMedia',{configurable:true,value:()=>new Promise(resolve=>{
   window.__cameraResolve=()=>{const canvas=document.createElement('canvas');canvas.width=10;canvas.height=10;const stream=canvas.captureStream(1);for(const track of stream.getTracks()){const stop=track.stop.bind(track);track.stop=()=>{window.__cameraStops++;stop();};}resolve(stream);};
  })});
 });
 await page.locator('#portalQR').click();await page.locator('#portalQRCamera').click();await page.locator('#portalQRClose').click();
 await page.evaluate(()=>window.__cameraResolve());await page.waitForFunction(()=>window.__cameraStops===1);
 await page.locator('#portalQR').click();await page.locator('#portalQRCamera').click();await page.evaluate(()=>window.__cameraResolve());
 await page.locator('#portalQRVideo').waitFor({state:'visible'});await page.keyboard.press('Escape');
 await page.waitForFunction(()=>window.__cameraStops===2);assert.equal(await page.locator('#portalQRVideo').isHidden(),true);
 // Image path: actual local QR decoding fills a voucher and never submits it.
 await page.locator('#portalQR').click();
 await page.locator('#portalQRFile').setInputFiles({name:'voucher.png',mimeType:'image/png',buffer:await QRCode.toBuffer(process.env.SUBSCRIBER_TEST_VOUCHER)});
 await page.locator('#portalQRDialog').waitFor({state:'hidden'});
 assert.equal(await page.locator('#subscriberUsername').inputValue(),process.env.SUBSCRIBER_TEST_VOUCHER);
 assert.equal(loginRequests,0); assert.equal(connectRequests,0);
 const voucherReply=page.waitForResponse(r=>r.url().endsWith('/api/login'));
 await page.locator('#subscriberUsername').press('Enter'); assert.equal((await voucherReply).status(),200);
 await page.locator('#subscriptionTitle').filter({hasText:'Synthetic test'}).waitFor();
 assert.equal(await page.locator('#subscriberUsername').inputValue(),'');
 assert.equal(await page.locator('#subscriberPassword').inputValue(),'');
 assert.equal(connectRequests,0,'Shadow voucher entry submitted a network request');
 await page.locator('#portalLogout').click(); await page.locator('#portalLogin').waitFor({state:'visible'});
 // Account path still supports password managers and keyboard submission.
 await page.goto(origin+'/?connect=C');
 await page.getByRole('button',{name:'حساب مستخدم',exact:true}).click();
 await page.locator('#subscriberUsername').fill('synthetic-account');
 await page.locator('#subscriberPassword').fill('wrong-password');
 await page.locator('#subscriberPassword').press('Enter');
 await page.getByText('بيانات الدخول غير صحيحة.',{exact:true}).waitFor();
 assert.equal(await page.locator('#portalLogin').isVisible(),true);
 await page.locator('#subscriberPassword').fill('synthetic-password');
 assert.equal(await page.getByRole('button', { name: 'تسجيل الدخول والاتصال', exact: true }).count(),1);
 await page.locator('#subscriberPassword').press('Enter');
 await page.locator('#portalAccount').waitFor({ state: 'visible' });
 await page.getByRole('heading', { name: 'Synthetic test', exact: true }).waitFor();
 await page.waitForFunction(()=>document.activeElement?.id==='subscriptionTitle');
 assert.equal(connectRequests,0,'Shadow sign-in submitted a network request');
 assert.equal(await page.locator('#internetState').innerText(),'لم يتم التحقق من الاتصال');
 await page.goto(origin+'/?connect=C');
 await page.locator('#portalAccount').waitFor({state:'visible'});
 await page.locator('#connectionState').filter({hasText:'تحديث مباشر'}).waitFor();
 assert.equal(connectRequests,0,'A GET landing hint and existing cookie opened Internet');
 await page.goto(origin+'/?connection='+'f'.repeat(64));
 await page.locator('#portalAccount').waitFor({state:'visible'});
 await page.locator('#connectionState').filter({hasText:'تحديث مباشر'}).waitFor();
 assert.equal(await page.locator('#internetState').innerText(),'لم يتم التحقق من الاتصال');
 assert.equal(new URL(page.url()).search,'','Connection reference remained in the visible URL');
 assert.equal(connectRequests,0,'An unowned return reference submitted a network request');
 if (process.env.SUBSCRIBER_PENDING_RECEIPT === 'true') {
  await page.locator('#portalNotice').filter({hasText:'يلزم تسوية الاستهلاك (1 قيد المراجعة)'}).waitFor();
  const snapshot=await (await context.request.get(origin+'/api/account')).json();
  assert.equal(snapshot.pendingAccounting,1); assert.equal(snapshot.quality,'reconciling'); assert.equal(snapshot.decision.allowed,false); assert.equal(snapshot.lifetimeSeconds,'20');
  await page.locator('#subscriberLimits').getByText('00:00:20',{exact:true}).waitFor();
  await page.locator('#subscriberLimits').getByText('00:59:40',{exact:true}).first().waitFor();
  assert.doesNotMatch(await page.locator('#subscriberLimits').innerText(),/Unknown/);
 }
 // A real request is held while live snapshots arrive: one speed write, focus kept.
 let releaseSpeed; const hold=new Promise(resolve=>releaseSpeed=resolve);
 await page.route('**/api/speed',async route=>{await hold;await route.continue();});
 await page.getByRole('button', { name: 'Fast', exact: true }).focus(); await page.keyboard.press('Enter');
 await page.waitForFunction(()=>document.querySelector('#speedButtons').getAttribute('aria-busy')==='true');
 await page.waitForTimeout(2200); assert.equal(await page.locator('#speedButtons button:disabled').count(),2);
 releaseSpeed(); await page.getByText('تم اختيار السرعة. أعد اتصال الجهاز').waitFor();
 await page.unroute('**/api/speed');
 assert.equal(speedRequests,1);
 assert.match(await page.locator('#speedState').innerText(), /بانتظار إعادة الاتصال/);
 await page.waitForFunction(()=>document.activeElement?.getAttribute('data-speed')==='fast');
 await page.waitForTimeout(2200);
 assert.equal(await page.evaluate(()=>document.activeElement?.getAttribute('data-speed')),'fast');
 await page.getByRole('button', { name: 'الاتصال بالإنترنت',exact:true }).focus(); await page.keyboard.press('Enter');
 await page.getByText('الاتصال بالإنترنت غير مفعّل من هذه البوابة حالياً.',{exact:true}).waitFor();
 for (const width of [375,1280]) { await page.setViewportSize({width,height:900}); await audit(`account-${width}`); }
 assert.deepEqual(errors,[]); assert.deepEqual(csp,[]);
 await page.getByRole('button',{name:'الخروج من البوابة'}).click();
 await page.locator('#portalLogin').waitFor({state:'visible'});
 assert.equal(await page.evaluate(()=>document.activeElement?.id),'subscriberUsername');
 assert.equal((await context.request.get(origin+'/api/account')).status(),401);
 for(const path of ['/api/routers','/api/users','/app.js','/subscriber/','/vendor/jsqr/','/subscriber/private.json']) assert.equal((await context.request.get(origin+path)).status(),404,path);
 await context.close();
 console.log('So7ob portal passed: real account/voucher HTTP authentication, local QR, speed/WS focus, shadow refusal, logout, RTL 375/1280, keyboard and axe.');
} finally { await browser.close(); }
