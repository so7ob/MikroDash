// Synthetic HTTP/browser regression: actual admin page/module and local assets.
// No router or real subscriber database is used.
import {createRequire} from 'node:module';
import {readFileSync,writeFileSync,mkdirSync} from 'node:fs';
import {createServer} from 'node:http';
import {resolve} from 'node:path';
import assert from 'node:assert/strict';
const require=createRequire(process.env.SUBSCRIBER_BROWSER_MODULE+'/package.json');
const {chromium}=require('playwright');
const {default:AxeBuilder}=require('@axe-core/playwright');
const buildRequire=createRequire(resolve('web/package.json'));
const {build}=buildRequire('esbuild');
const out=process.env.SUBSCRIBER_EVIDENCE_DIR;mkdirSync(out,{recursive:true});
const bundle=await build({stdin:{contents:`import {initSubscribersPage} from './web/src/pages/subscribers';initSubscribersPage();document.dispatchEvent(new CustomEvent('mikrodash:pagechange',{detail:'subscribers'}));`,resolveDir:process.cwd()},bundle:true,write:false,format:'iife'});
const pageHTML=readFileSync('web/src/ui/page-subscribers.html','utf8').replace('class="page-view"','class="page-view" style="display:block"');
const html=`<!doctype html><html lang="en" dir="ltr" data-theme="dark" data-bs-theme="dark"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Synthetic subscriber history</title><link rel="stylesheet" href="/vendor/tabler.min.css"><link rel="stylesheet" href="/vendor/fonts/fonts.css"><link rel="stylesheet" href="/css/app-fonts.css"><link rel="stylesheet" href="/app.css"></head><body><main><h1>Subscriber administration</h1>${pageHTML}</main><script src="/test.js"></script></body></html>`;
const server=createServer((req,res)=>{const url=new URL(req.url,'http://localhost');if(url.pathname==='/'){res.setHeader('Content-Type','text/html');res.end(html);}else if(url.pathname==='/test.js'){res.setHeader('Content-Type','text/javascript');res.end(bundle.outputFiles[0].text);}else {const p=resolve('web/public','.'+url.pathname);if(!p.startsWith(resolve('web/public')+'/')){res.writeHead(404).end();return;}try{res.setHeader('Content-Type',p.endsWith('.css')?'text/css':'application/octet-stream');res.end(readFileSync(p));}catch{res.writeHead(404).end();}}});
await new Promise(r=>server.listen(0,'127.0.0.1',r));
const browser=await chromium.launch({headless:true,executablePath:process.env.SUBSCRIBER_CHROMIUM||'/usr/bin/chromium'});
try{
 const context=await browser.newContext();const page=await context.newPage();page.setDefaultTimeout(10000);const calls=[];let slowResolve,slow=true,failOlder=true,inspectResolve,inspectFail=false,deny=0;
 const rows=Array.from({length:50},(_,i)=>({id:String(9007199254741099n-BigInt(i)),actor:'Synthetic admin',action:'account.suspended',reason:'<img src=x onerror=alert(1)> Synthetic reason',createdAt:100}));
 await page.route('**/api/**',async route=>{
  const request=route.request(),url=new URL(request.url());calls.push({method:request.method(),path:url.pathname+url.search});assert.equal(request.method(),'GET');
  let data;
  if(url.pathname==='/api/subscribers')data={accounts:['A','B'].map(id=>({id,username:'Account '+id,name:'Synthetic '+id,status:'active',statusRevision:'0'})),plans:[],mode:'disabled'};
  else if(url.pathname==='/api/subscribers/snapshot'){await new Promise(r=>inspectResolve=r);if(inspectFail){await route.fulfill({status:503,json:{error:'Stale inspect failure'}});return;}data={decision:{allowed:true,reasons:[],limits:[]},pendingAccounting:0};}
  else if(url.pathname==='/api/subscribers/status-history'){
   if(deny){await route.fulfill({status:deny,json:{error:deny===401?'Authentication required':'Subscriber administration permission required'}});return;}
   if(url.searchParams.get('account')==='B')data={accountId:'B',events:[]};
   else if(url.searchParams.has('before')){
    assert.equal(url.searchParams.get('before'),'9007199254741050');
    if(failOlder){failOlder=false;await route.fulfill({status:200,contentType:'application/json',body:'{"interrupted":'});return;}
    data={accountId:'A',events:[{...rows[0],id:'9007199254741000'}]};
   }else{if(slow)await new Promise(r=>slowResolve=r);data={accountId:'A',events:rows,nextBefore:rows[49].id};}
  }else throw new Error('Unexpected API '+url.pathname);
  await route.fulfill({json:data});
 });
 await page.goto(`http://127.0.0.1:${server.address().port}`);
 const tab=name=>page.getByRole('tab',{name,exact:true});
 await page.locator('[data-sub-inspect="A"]').click();
 await page.locator('[data-sub-history="B"]').click();await page.getByText('No status changes recorded.',{exact:true}).waitFor();
 const inspected=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/subscribers/snapshot');inspectResolve();await inspected;
 await page.waitForTimeout(100);
 assert.equal(await tab('Status history').getAttribute('aria-selected'),'true','late Inspect response replaced newer history selection');
 await tab('Subscribers').click();
 // A stale rejection must not overwrite a newer tab's notice.
 inspectFail=true;await page.locator('[data-sub-inspect="A"]').click();await tab('Plans and policies').click();
 const priorNotice=await page.locator('#subscriberAdminNotice').innerText();
 const rejectedInspect=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/subscribers/snapshot');inspectResolve();await rejectedInspect;await page.waitForTimeout(100);
 assert.equal(await page.locator('#subscriberAdminNotice').innerText(),priorNotice);
 // Leaving the Subscribers page also invalidates a pending Inspect result.
 await tab('Subscribers').click();inspectFail=false;await page.locator('[data-sub-inspect="A"]').click();
 await page.evaluate(()=>document.dispatchEvent(new CustomEvent('mikrodash:pagechange',{detail:'dashboard'})));
 const awayInspect=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/subscribers/snapshot');inspectResolve();await awayInspect;await page.waitForTimeout(100);
 assert.equal(await tab('Subscribers').getAttribute('aria-selected'),'true','Inspect changed tab after leaving Subscribers');
 await page.evaluate(()=>document.dispatchEvent(new CustomEvent('mikrodash:pagechange',{detail:'subscribers'})));
 // Actual fetch helper must reject authorization failures, clear prior rows,
 // and allow first-page retry after synthetic authorization is restored.
 for(const status of [401,403]){
  deny=status;await page.locator('[data-sub-history="B"]').click();
  await page.getByText(status===401?'Authentication required':'Subscriber administration permission required',{exact:true}).waitFor();
  assert.equal(await page.locator('#subscriberStatusHistory li').count(),0);
  deny=0;await page.getByRole('button',{name:'Retry page'}).click();await page.getByText('No status changes recorded.',{exact:true}).waitFor();
  await tab('Subscribers').click();
 }
 await page.locator('[data-sub-history="A"]').click();await page.getByText('Loading status history…',{exact:true}).waitFor();
 await tab('Plans and policies').click();
 const completedHistory=page.waitForResponse(r=>r.url().includes('status-history?account=A'));
 slow=false;slowResolve();await completedHistory;await page.waitForTimeout(100);
 assert.equal(await tab('Plans and policies').getAttribute('aria-selected'),'true','history completion changed selected tab');
 assert.equal(await tab('Plans and policies').evaluate(n=>n===document.activeElement),true,'history completion stole focus');
 // Start a fresh delayed A read for the existing account-switch regression.
 await tab('Subscribers').click();slow=true;await page.locator('[data-sub-history="A"]').click();await page.getByText('Loading status history…',{exact:true}).waitFor();
 await tab('Subscribers').click();await page.locator('[data-sub-history="B"]').click();await page.getByText('No status changes recorded.',{exact:true}).waitFor();
 slow=false;slowResolve();await page.waitForTimeout(100);assert.equal(await page.locator('#subscriberStatusHistory li').count(),0);assert.match(await page.locator('#subscriberStatusHistory').innerText(),/Account B/);
 await tab('Subscribers').click();await page.locator('[data-sub-history="A"]').click();await page.getByText('50 events loaded.',{exact:true}).waitFor();
 await page.getByRole('button',{name:'Load older changes'}).click();await page.getByRole('button',{name:'Retry page'}).waitFor();assert.equal(await page.locator('#subscriberStatusHistory li').count(),50);
 await page.getByRole('button',{name:'Retry page'}).click();await page.getByText('51 events loaded.',{exact:true}).waitFor();assert.equal(await page.locator('#subscriberStatusHistory li').count(),51);assert.equal(await page.locator('#subscriberStatusHistory img').count(),0);
 assert.equal(await page.locator('[data-history-heading]').evaluate(n=>n===document.activeElement),true,'focus after final retry');
 for(const theme of ['dark','light'])for(const width of [375,1280]){
  await page.setViewportSize({width,height:900});await page.evaluate(theme=>{document.documentElement.dataset.theme=theme;document.documentElement.dataset.bsTheme=theme;},theme);
  const result=await new AxeBuilder({page}).include('#subAdmin-history').withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(result.violations,[]);
  assert.equal(await page.locator('#subAdmin-history').evaluate(n=>n.scrollWidth>n.clientWidth+1),false,'history overflow');
  await page.screenshot({path:`${out}/history-${theme}-${width}.png`,fullPage:false});
 }
 await page.getByRole('button',{name:'Refresh history',exact:true}).click();await page.getByText('50 events loaded.',{exact:true}).waitFor();
 assert.equal(calls.filter(c=>c.path.includes('before=')).length,2);
 writeFileSync(out+'/result.json',JSON.stringify({passed:true,scenarios:['stale Inspect rejection','Inspect completion after page departure','late Inspect response isolation','401/403 first-page error and retry','navigation away while history pending','late account response isolation','empty','loading','partial JSON interrupted page and retry','exact cursor','escaped source content','final-page focus','refresh','four accessibility/theme/viewport checks'],calls},null,2));
 console.log('PASS synthetic history browser flows and four axe/viewport checks');
}finally{await browser.close();server.closeAllConnections();await new Promise(r=>server.close(r));}
