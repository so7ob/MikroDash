// Actual client acceptance for the owned CHR and normal MikroDash runtime only.
import assert from 'node:assert/strict';
import {createHash, randomBytes, X509Certificate} from 'node:crypto';
import {execFileSync, spawnSync} from 'node:child_process';
import * as fs from 'node:fs';
import path from 'node:path';
import {createRequire} from 'node:module';
import {fileURLToPath} from 'node:url';
import {readPrivateJSON, runtimeFixture, verifyApp} from './subscriber-chr-proof.mjs';

const script=fileURLToPath(import.meta.url), root=path.dirname(path.dirname(script));
const [action,directory,inside,...extra]=process.argv.slice(2);
if(!['journey','exhausted'].includes(action)||!directory||!path.isAbsolute(directory)||extra.length||(inside&&inside!=='--inside')) {
 console.error('Usage: node tools/subscriber-chr-browser.mjs journey|exhausted /absolute/private/lab');process.exit(1);
}
process.umask(0o077);
const privateJSON=name=>readPrivateJSON(directory,name);
const {state,fixture,cfg}=runtimeFixture(directory,true), app=privateJSON('app-owned.json');
if(!inside) {
 execFileSync(process.execPath,[path.join(root,'tools/subscriber-chr-lab.mjs'),'check',directory],{stdio:'pipe',timeout:10000});
 verifyApp(directory,app,fixture);
 const child=spawnSync('ip',['netns','exec','md-subscriber-lab',process.execPath,script,action,directory,'--inside'],{stdio:'inherit',timeout:180000});
 if(child.error)throw child.error;process.exit(child.status??1);
}
assert.equal(fs.statSync('/proc/self/ns/net').ino,fs.statSync('/run/netns/md-subscriber-lab').ino,'actual isolated client namespace required');
if(!process.env.SUBSCRIBER_BROWSER_MODULE)throw Error('Configure the existing SUBSCRIBER_BROWSER_MODULE browser harness');
const require=createRequire(path.join(process.env.SUBSCRIBER_BROWSER_MODULE,'package.json'));
const {chromium}=require('playwright'), {default:AxeBuilder}=require('@axe-core/playwright');
const credentials=privateJSON('subscriber-login.json');assert.equal(credentials.username,'chr-lab');
for(const [host,address,port,ca,route,status] of [
 ['login.example.test','198.51.100.1',443,'md-lab-ca.crt','/login','200'],
 ['portal.example.test','198.51.100.10',9444,'portal-ca.crt','/api/session','401'],
]) {
 const code=execFileSync('curl',['--noproxy','*','--silent','--show-error','--max-time','10','--cacert',path.join(directory,ca),'--resolve',`${host}:${port}:${address}`,`https://${host}:${port}${route}`,'-o','/dev/null','-w','%{http_code}'],{encoding:'utf8',timeout:12000});
 assert.equal(code,status,'strict CA/hostname preflight failed');
}
const pin=(name,host)=>{
 const cert=new X509Certificate(fs.readFileSync(path.join(directory,name)));assert.equal(cert.checkHost(host),host);
 return createHash('sha256').update(cert.publicKey.export({type:'spki',format:'der'})).digest('base64');
};
const pins=[pin('portal-chain.pem','portal.example.test'),pin('md-lab-server.crt','login.example.test')];
const evidence=path.join(directory,`browser-${action}-${randomBytes(6).toString('hex')}`);fs.mkdirSync(evidence,{mode:0o700});
const save=(name,value)=>fs.writeFileSync(path.join(evidence,name),JSON.stringify(value,null,2),{mode:0o600,flag:'wx'});
const browser=await chromium.launch({headless:true,...(process.env.SUBSCRIBER_CHROMIUM?{executablePath:process.env.SUBSCRIBER_CHROMIUM}:{}),args:['--no-sandbox','--no-proxy-server','--host-resolver-rules=MAP portal.example.test 198.51.100.10, MAP login.example.test 198.51.100.1',`--ignore-certificate-errors-spki-list=${pins.join(',')}`]});
const context=await browser.newContext({viewport:{width:1280,height:900}}), page=await context.newPage(), errors=[];
page.on('pageerror',e=>errors.push(e.message));
const origin='https://portal.example.test:9444';let phase='sign-in';
const snapshot=()=>page.evaluate(async()=>{const r=await fetch('/api/account');if(!r.ok)throw Error('Owned account unavailable');return r.json();});
try {
 await page.goto('https://login.example.test/login'); // Actual HotSpot document; automatic handoff is a separate gap.
 await page.goto(origin);await page.locator('#subscriberUsername').fill(credentials.username);await page.locator('#subscriberPassword').fill(credentials.password);await page.locator('#subscriberLoginForm button').click();await page.locator('#portalAccount').waitFor({state:'visible'});
 const before=await snapshot();save('before.json',before);assert.equal(before.pendingAccounting,0);assert.equal(before.sessions.filter(s=>s.state==='active').length,0,'start with no owned active session');
 assert.equal(await page.evaluate(async()=>(await fetch('/api/routers')).status),404);
 phase='actual browser HotSpot POST';const response=page.waitForResponse(r=>r.url().startsWith('https://login.example.test/login')&&r.request().method()==='POST',{timeout:15000});
 await page.locator('#internetConnect').click();assert.equal((await response).status(),200);const repliedAt=Date.now();
 if(action==='exhausted') {
  assert.equal(before.decision.allowed,false);assert.equal(before.decision.limits.find(x=>x.limit.kind==='download').remaining,'0');
  await page.waitForURL('https://login.example.test/login',{timeout:10000});const text=await page.locator('body').innerText();assert.match(text,/invalid|denied|reject|not allowed|not permitted|failed/i);save('router-refusal.json',{text});
  await page.goto(origin);await page.locator('#portalAccount').waitFor({state:'visible'});const after=await snapshot();assert.equal(after.decision.allowed,false);assert.equal(after.sessions.filter(s=>s.state==='active').length,0);assert.equal(after.lifetimeDownload,before.lifetimeDownload);
  save('result.json',{synthetic:true,result:'network-passed',scope:'actual exhausted re-entry denied; portal available; admin route 404',runtimeBinarySHA256:app.binarySHA256,automaticRecovery:false});
 } else {
  assert.equal(before.decision.allowed,true);await page.waitForURL(origin+'/',{timeout:10000});await page.waitForFunction(()=>document.querySelector('#sessionCount')?.textContent==='1',null,{timeout:10000});
  const dashboardReturnMs=Date.now()-repliedAt;assert.ok(dashboardReturnMs<=5000,'dashboard return exceeded five seconds');
  const connected=await snapshot(), live=connected.sessions.filter(s=>s.state==='active');assert.equal(live.length,1);assert.equal(live[0].connectionState,'connected');assert.ok(BigInt(connected.lifetimeSeconds)>=BigInt(before.lifetimeSeconds));save('connected.json',connected);
  const profiles=['fast','economy'];assert.ok(profiles.every(id=>connected.subscription.plan.speeds.some(p=>p.id===id)));
  if(connected.subscription.speedId==='fast')profiles.reverse();
  for(const [index,id] of profiles.entries()) {
   phase=`speed ${id}`;const reply=page.waitForResponse(r=>r.url().endsWith('/api/speed')&&r.request().method()==='POST');await page.locator(`[data-speed="${id}"]`).click();assert.equal((await reply).status(),200);
   if(index===0) {
    const cooldown=await page.evaluate(async other=>{const s=await(await fetch('/api/session')).json();const r=await fetch('/api/speed',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':s.csrf},body:JSON.stringify({profileId:other})});return {status:r.status,body:await r.json()};},profiles[1]);
    assert.equal(cooldown.status,400);assert.match(cooldown.body.error,/wait before changing speed again/i);save('cooldown.json',cooldown);
   }
   const profile=connected.subscription.plan.speeds.find(p=>p.id===id);
   await page.waitForFunction(name=>document.querySelector('#speedState')?.textContent.includes(`Selected: ${name} · Application: verified`),profile.name,{timeout:15000});
   const applied=await snapshot(), device=applied.sessions.find(s=>s.state==='active');assert.equal(applied.subscription.speedId,id);assert.equal(applied.subscription.speedState,'verified');assert.equal(device.id,live[0].id);assert.equal(device.appliedDown,profile.down);assert.equal(device.appliedUp,profile.up);
   assert.equal(applied.subscription.id,connected.subscription.id);assert.equal(applied.subscription.expiresAt,connected.subscription.expiresAt);assert.ok(BigInt(applied.lifetimeSeconds)>=BigInt(connected.lifetimeSeconds));assert.equal(applied.lifetimeDownload,connected.lifetimeDownload);assert.equal(applied.lifetimeUpload,connected.lifetimeUpload);save(`speed-${id}.json`,applied);
   if(index===0)await page.waitForTimeout(10500);
  }
  save('result.json',{synthetic:true,result:'network-passed',dashboardReturnMs,lease:live[0].id,sessionId:live[0].sessionId,runtimeBinarySHA256:app.binarySHA256,automaticLanding:false,authority:'normal MikroDash runtime; real owned CHR'});
 }
 phase='visual and accessibility';for(const width of [375,1280]) {
  await page.setViewportSize({width,height:900});assert.equal(await page.locator('html').getAttribute('lang'),'en');assert.equal(await page.locator('html').getAttribute('dir'),'ltr');assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1),false);
  for(const theme of ['blue','green','purple']) {
   await page.locator('#portalTheme').selectOption(theme);await page.screenshot({path:path.join(evidence,`${width}-${theme}.png`),fullPage:true});
   const axe=await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(axe.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)})),[]);
  }
 }
 assert.deepEqual(errors,[]);save('complete.json',{synthetic:true,action,result:'passed',allBrowsersClosedAfterRun:true});console.log(`Owned CHR ${action} browser acceptance passed; private evidence: ${evidence}`);
} catch(e) {save('failure.json',{phase,error:String(e),url:page.url(),errors});await page.screenshot({path:path.join(evidence,'failed.png'),fullPage:true}).catch(()=>{});throw e;}
finally {await browser.close();}
