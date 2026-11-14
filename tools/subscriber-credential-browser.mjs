// Protected HTTP/SQLite acceptance. All accounts/secrets/NAS observations are synthetic.
import assert from 'node:assert/strict';
import {createRequire} from 'node:module';
import {mkdirSync,readFileSync,writeFileSync} from 'node:fs';
const require=createRequire(process.env.SUBSCRIBER_BROWSER_MODULE+'/package.json');
const {chromium}=require('playwright');
const {default:AxeBuilder}=require('@axe-core/playwright');
const out=process.env.SUBSCRIBER_EVIDENCE_DIR;mkdirSync(out,{recursive:true});
const geometrySource=readFileSync(new URL('./form-geometry.js',import.meta.url),'utf8');
const browser=await chromium.launch({headless:true,...(process.env.SUBSCRIBER_CHROMIUM?{executablePath:process.env.SUBSCRIBER_CHROMIUM}:{})});
let diagnosticPage;let stage='setup';
try {
 const context=await browser.newContext({ignoreHTTPSErrors:true});
 const portalContext=await browser.newContext({ignoreHTTPSErrors:true});
 const origin=process.env.SUBSCRIBER_TEST_URL,portal=process.env.SUBSCRIBER_PORTAL_TEST_URL,id=process.env.SUBSCRIBER_ACCOUNT_ID;
 await context.addCookies([{name:'mikrodash_sid',value:'synthetic-token',url:origin,httpOnly:true,secure:true,sameSite:'Lax'}]);
 const headers={Origin:origin,'X-MikroDash-Request':'subscriber-admin'};
 const account=async()=>((await (await context.request.get(origin+'/api/subscribers')).json()).accounts.find(a=>a.id===id));
 const login=(ctx,password,username='synthetic-account')=>ctx.request.post(portal+'/api/login',{headers:{Origin:portal},data:{username,password}});
 assert.equal((await login(portalContext,'synthetic-password')).status(),200);
 assert.equal((await portalContext.request.get(portal+'/api/session')).status(),200);
 const page=await context.newPage();diagnosticPage=page;const errors=[];let submissions=0;
 page.on('pageerror',error=>errors.push(error.message));
 page.on('request',request=>{if(request.method()==='POST'&&new URL(request.url()).pathname==='/api/subscribers'&&request.postDataJSON()?.action==='credential')submissions++;});
 await page.setViewportSize({width:1280,height:1000});await page.goto(origin+'/subscribers');
 const opener=()=>page.locator(`[data-sub-credential="${id}"]`);
 const dialog=page.locator('#subscriberCredentialModal');const form=page.locator('#subscriberCredentialForm');
 const secret=form.getByLabel('New password',{exact:true}),repeat=form.getByLabel('Confirm new password',{exact:true}),reason=form.getByLabel('Reset reason',{exact:true});
 const confirm=form.getByRole('checkbox'),save=()=>form.getByRole('button',{name:'Reset password',exact:true});
 const fill=async(password,why)=>{await secret.fill(password);await repeat.fill(password);await reason.fill(why);await confirm.check();};
 const discard=async()=>{await dialog.getByRole('button',{name:'Cancel',exact:true}).click();await dialog.getByRole('button',{name:'Discard changes',exact:true}).click();await dialog.waitFor({state:'hidden'});};
 const refresh=async()=>{const response=page.waitForResponse(r=>r.url().endsWith('/api/subscribers')&&r.request().method()==='GET');await page.getByRole('button',{name:'Refresh',exact:true}).click();assert.equal((await response).status(),200);await page.waitForFunction(()=>document.querySelector('#subscriberAdminNotice').textContent.startsWith('Mode:'));};
 stage='cancel and validation';await opener().click();
 assert.match(await page.locator('#subscriberCredentialAccount').innerText(),/اسم <script>/);
 assert.equal(await dialog.locator('script').count(),0);assert.equal(await page.evaluate(()=>window.credentialNameExecuted),undefined);
 await fill('synthetic-browser-new-password','Synthetic reviewed rotation');await repeat.fill('different-synthetic-value');await save().click();
 assert.equal(await repeat.evaluate(n=>n.validity.customError),true);assert.equal(submissions,0);
 await secret.fill('short');await repeat.fill('short');await save().click();assert.equal(await secret.evaluate(n=>n.validity.customError),true);assert.equal(submissions,0);
 await page.keyboard.press('Escape');await dialog.getByRole('button',{name:'Keep editing',exact:true}).click();assert.equal(await secret.inputValue(),'short');
 await discard();assert.equal(await secret.inputValue(),'');assert.equal(await opener().evaluate(n=>n===document.activeElement),true);
 stage='stale review';await opener().click();await fill('synthetic-browser-new-password','Synthetic stale reviewed rotation');
 assert.equal(await page.locator('#subscriberCredentialRevision').innerText(),'0');
 const concurrent=await context.request.post(origin+'/api/subscribers',{headers,data:{action:'credential',accountId:id,password:'synthetic-concurrent-password',reason:'Synthetic concurrent rotation',expectedCredentialRevision:'0',confirmCredentialReset:true}});assert.equal(concurrent.status(),200);
 assert.equal((await portalContext.request.get(portal+'/api/session')).status(),401);
 const stale=page.waitForResponse(r=>r.url().endsWith('/api/subscribers')&&r.request().method()==='POST');await save().click();assert.equal((await stale).status(),409);
 await page.waitForFunction(()=>document.querySelector('#subscriberCredentialSave').disabled);
 assert.equal(await secret.inputValue(),'synthetic-browser-new-password');await discard();await opener().click();assert.equal(await dialog.isVisible(),false);
 await page.getByText('Refresh the subscriber list before reviewing another credential reset.',{exact:true}).waitFor();await refresh();await opener().click();assert.equal(await page.locator('#subscriberCredentialRevision').innerText(),'1');
 stage='correctable server refusal';await fill('synthetic-browser-new-password','Synthetic corrected reviewed rotation');
 await page.route('**/api/subscribers',async route=>{const body=route.request().postDataJSON();const response=await route.fetch({postData:JSON.stringify({...body,password:'short'})});await route.fulfill({response});},{times:1});
 const refused=page.waitForResponse(r=>r.url().endsWith('/api/subscribers')&&r.request().method()==='POST');await save().click();assert.equal((await refused).status(),400);
 await page.waitForFunction(()=>!document.querySelector('#subscriberCredentialSave').disabled);assert.equal(await secret.inputValue(),'synthetic-browser-new-password');assert.equal((await account()).credentialRevision,'1');
 stage='commit with lost response';let release,ready;const gate=new Promise(resolve=>{release=resolve;}),fetched=new Promise(resolve=>{ready=resolve;});
 await page.route('**/api/subscribers',async route=>{const response=await route.fetch();assert.equal(response.status(),200);ready();await gate;await route.abort('connectionreset');},{times:1});
 const beforeLost=submissions;await save().click();await fetched;
 assert.equal(await secret.isDisabled(),true);assert.equal(await dialog.getByRole('button',{name:'Cancel',exact:true}).isDisabled(),true);
 await page.keyboard.press('Escape');assert.equal(await dialog.isVisible(),true);
 await form.evaluate(n=>n.dispatchEvent(new Event('submit',{bubbles:true,cancelable:true})));assert.equal(submissions,beforeLost+1);
 release();await page.waitForFunction(()=>document.querySelector('#subscriberCredentialModal [data-managed-error]').textContent.includes('refresh the subscriber list'));
 assert.equal(await save().isDisabled(),true);assert.equal(await secret.inputValue(),'synthetic-browser-new-password');assert.equal((await account()).credentialRevision,'2');
 assert.equal((await login(portalContext,'synthetic-concurrent-password')).status(),401);assert.equal((await login(portalContext,'synthetic-browser-new-password')).status(),200);
 await discard();await refresh();await opener().click();assert.equal(await page.locator('#subscriberCredentialRevision').innerText(),'2');
 stage='visual and accessibility';await fill('synthetic-browser-final-password','Synthetic final reviewed rotation');
 await dialog.getByRole('button',{name:'Close',exact:true}).focus();await page.keyboard.press('Shift+Tab');assert.equal(await save().evaluate(n=>n===document.activeElement),true);await page.keyboard.press('Tab');assert.equal(await dialog.getByRole('button',{name:'Close',exact:true}).evaluate(n=>n===document.activeElement),true);
 const evidence=[];
 for(const appearance of [{theme:'dark',palette:''},{theme:'light',palette:''},{theme:'dark',palette:'nord'}]){
  await page.evaluate(({theme,palette})=>{document.documentElement.dataset.theme=theme;document.documentElement.dataset.bsTheme=theme;if(palette)document.documentElement.dataset.palette=palette;else delete document.documentElement.dataset.palette;},appearance);
  for(const width of [375,768,1280,1920]){
   await page.setViewportSize({width,height:1000});assert.equal(await page.locator('html').getAttribute('dir'),'ltr');
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1),false,'page overflow');
   assert.equal(await dialog.locator('[role=dialog]').evaluate(n=>n.scrollWidth>n.clientWidth+1),false,'dialog overflow');
   const geometry=await page.evaluate(geometrySource);assert.ok(geometry.fields>=4);assert.equal(geometry.ok,true,JSON.stringify(geometry.issues));
   if(width===375&&appearance.theme==='dark'&&!appearance.palette){await secret.evaluate(n=>n.style.height='50px');assert.equal((await page.evaluate(geometrySource)).ok,false,'geometry accepted deliberate broken input');await secret.evaluate(n=>n.style.removeProperty('height'));assert.equal((await page.evaluate(geometrySource)).ok,true);}
   const audit=await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(audit.violations,[]);
   const file=`subscriber-credential-synthetic-${appearance.theme}-${appearance.palette||'default'}-${width}.png`;await page.screenshot({path:out+'/'+file,fullPage:true});evidence.push({...appearance,width,file,axeViolations:audit.violations.length,geometry:true});
  }
 }
 await page.evaluate(()=>{document.documentElement.dataset.theme='dark';document.documentElement.dataset.bsTheme='dark';delete document.documentElement.dataset.palette;});
 stage='successful final reset';await page.setViewportSize({width:1280,height:1000});const final=page.waitForResponse(r=>r.url().endsWith('/api/subscribers')&&r.request().method()==='POST');await save().click();assert.equal((await final).status(),200);await dialog.waitFor({state:'hidden'});
 await page.getByText('Password reset. Old credentials and portal sessions are revoked. Subscription balances and usage are preserved.',{exact:true}).waitFor();
 assert.equal(await secret.inputValue(),'');assert.equal(await repeat.inputValue(),'');assert.equal(await opener().evaluate(n=>n===document.activeElement),true);assert.equal((await account()).credentialRevision,'3');
 assert.equal((await portalContext.request.get(portal+'/api/session')).status(),401);assert.equal((await login(portalContext,'synthetic-browser-new-password')).status(),401);assert.equal((await login(portalContext,'synthetic-browser-final-password')).status(),200);assert.equal((await portalContext.request.get(portal+'/api/session')).status(),200);
 const otherContext=await browser.newContext({ignoreHTTPSErrors:true});assert.equal((await login(otherContext,'synthetic-other-password','synthetic-other-credential')).status(),200);
 assert.equal(await page.evaluate(()=>window.credentialNameExecuted),undefined);assert.deepEqual(errors,[]);
 writeFileSync(out+'/credential-browser-results.json',JSON.stringify({source:'real protected HTTP and SQLite; synthetic identities and NAS',status:'passed',scenarios:['validation','dirty cancel','focus trap/return','stale revision','server refusal correction','committed lost response fencing','duplicate submit prevention','old portal revocation','fresh login','unrelated account preserved','Unicode and script escaping'],evidence},null,2));
 console.log(JSON.stringify({status:'passed',screenshots:evidence.length,axeCases:evidence.length,finalRevision:'3',source:'real protected HTTP/SQLite; synthetic identities/NAS'}));
} catch(error){
 if(diagnosticPage){await diagnosticPage.screenshot({path:out+'/credential-failure.png',fullPage:true}).catch(()=>{});writeFileSync(out+'/credential-failure.json',JSON.stringify({stage,message:error.message},null,2));}
 throw error;
} finally {await browser.close();}
