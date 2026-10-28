// Frontend-only browser acceptance: real shipped UI/assets, synthetic HTTP API.
// This does not prove SQLite commits, RouterOS behavior or hardware acceptance.
import assert from 'node:assert/strict';
import {createRequire} from 'node:module';
import {readFileSync,mkdirSync,writeFileSync} from 'node:fs';
import {createServer} from 'node:http';
import {fileURLToPath} from 'node:url';
import path from 'node:path';
const root=fileURLToPath(new URL('../',import.meta.url));
const require=createRequire(process.env.SUBSCRIBER_BROWSER_MODULE+'/package.json');
const {chromium}=require('playwright');
const {default:AxeBuilder}=require('@axe-core/playwright');
const {buildSync}=createRequire(path.join(root,'web/package.json'))('esbuild');
const out=process.env.SUBSCRIBER_EVIDENCE_DIR;
assert.ok(out,'Set SUBSCRIBER_EVIDENCE_DIR');mkdirSync(out,{recursive:true});
const bundle=buildSync({entryPoints:[path.join(root,'web/test/browser/subscriber-admin-entry.ts')],bundle:true,write:false,format:'iife'}).outputFiles[0].contents;
const markup=readFileSync(path.join(root,'web/src/ui/page-subscribers.html'),'utf8').replace('class="page-view"','class="page-view active"');
let pending, posts=0;const submitted=[],plans=[];
const server=createServer(async(req,res)=>{
 try {
  const url=new URL(req.url,'http://localhost');
  if(url.pathname==='/api/subscribers') {
   res.setHeader('Content-Type','application/json');
   if(req.method==='GET'){res.end(JSON.stringify({accounts:[],plans,mode:'disabled'}));return;}
   let raw='';for await(const chunk of req)raw+=chunk;
   const body=JSON.parse(raw);assert.equal(body.action,'plan');submitted.push(body);posts++;
   pending=mode=>{pending=undefined;if(mode==='disconnect'){res.write('{');setTimeout(()=>res.destroy(),10);return;}if(mode==='fail'){res.statusCode=400;res.end(JSON.stringify({error:'Synthetic validation failure'}));return;}plans.push(body.plan);res.end('{}');};return;
  }
  if(url.pathname==='/admin-test.js'){res.setHeader('Content-Type','text/javascript');res.end(bundle);return;}
  if(url.pathname==='/'){res.setHeader('Content-Type','text/html');res.end(`<!doctype html><html lang="en" dir="ltr" data-bs-theme="dark" data-theme="dark"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Synthetic plan editor</title><link rel="stylesheet" href="/vendor/tabler.min.css"><link rel="stylesheet" href="/vendor/fonts/fonts.css"><link rel="stylesheet" href="/css/app-fonts.css"><link rel="stylesheet" href="/app.css"></head><body><main id="main"><h1>Synthetic administration</h1>${markup}</main><script src="/admin-test.js"></script></body></html>`);return;}
  const base=path.join(root,'web/public'),file=path.resolve(base,'.'+decodeURIComponent(url.pathname));
  if(!file.startsWith(base+path.sep)){res.statusCode=404;res.end();return;}
  const ext=path.extname(file);res.setHeader('Content-Type',ext==='.css'?'text/css':ext==='.woff2'?'font/woff2':'application/octet-stream');res.end(readFileSync(file));
 }catch{res.statusCode=404;res.end();}
});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const browser=await chromium.launch({headless:true,...(process.env.SUBSCRIBER_CHROMIUM?{executablePath:process.env.SUBSCRIBER_CHROMIUM}:{})});
const context=await browser.newContext();const page=await context.newPage();const errors=[];page.on('pageerror',error=>errors.push(error.message));
const waitPending=async()=>{for(let n=0;!pending&&n<100;n++)await new Promise(resolve=>setTimeout(resolve,10));assert.ok(pending,'plan request missing');};
try {
 await page.goto(`http://127.0.0.1:${server.address().port}`);
 await page.locator('#subTab-plans').click();
 for(const [id,value] of Object.entries({subPlanID:'synthetic-plan',subPlanName:'Synthetic خطة <script>',subPlanReason:'Synthetic editor verification'}))await page.locator('#'+id).fill(value);
 await page.locator('#subscriberAddLimit').click();
 await page.getByRole('button',{name:'Edit limit-1',exact:true}).click();
 await page.locator('#subLimitAmount').fill('7200');
 await page.locator('#subscriberCancelLimit').click();
 assert.match(await page.locator('#subscriberDraftLimits').textContent(),/3600/);
 await page.getByRole('button',{name:'Edit limit-1',exact:true}).click();
 await page.locator('#subLimitAmount').fill('9223372036854775808');
 await page.locator('#subscriberAddLimit').click();
 assert.match(await page.locator('#subscriberAdminNotice').textContent(),/range/);
 await page.locator('#subscriberSavePlan').dispatchEvent('submit');
 assert.match(await page.locator('#subscriberAdminNotice').textContent(),/Save or cancel/);assert.equal(posts,0);
 await page.locator('#subLimitAmount').fill('007200');await page.locator('#subscriberAddLimit').click();
 assert.match(await page.locator('#subscriberDraftLimits').textContent(),/7200/);
 await page.locator('#subLimitKind').selectOption('total');await page.locator('#subLimitAmount').fill('09007199254740993');
 await page.locator('#subLimitPeriod').selectOption('monthly');await page.locator('#subLimitZone').fill('UTC');
 await page.locator('#subLimitAction').selectOption('throttle');
 await page.locator('#subLimitDown').fill('00800000');await page.locator('#subLimitUp').fill('00400000');
 await page.locator('#subscriberAddLimit').click();
 await page.getByRole('button',{name:'Remove limit-1',exact:true}).click();
 await page.locator('#subLimitKind').selectOption('seconds');await page.locator('#subLimitAmount').fill('3600');await page.locator('#subLimitAction').selectOption('disconnect');
 await page.locator('#subscriberAddLimit').click();
 assert.equal(await page.getByRole('button',{name:'Edit limit-3',exact:true}).count(),1);
 // A dropped response retains the draft, unlocks the form and never claims success.
 await page.locator('#subscriberSavePlan').dispatchEvent('submit');await waitPending();
 assert.equal(posts,1);assert.equal(await page.locator('#subPlanID').isDisabled(),true);
 await page.locator('#subscriberSavePlan').dispatchEvent('submit');assert.equal(posts,1);
 pending('disconnect');await page.waitForFunction(()=>!document.querySelector('#subPlanID').disabled);
 assert.doesNotMatch(await page.locator('#subscriberAdminNotice').textContent(),/Plan version saved/);
 assert.equal(await page.getByRole('button',{name:'Edit limit-2',exact:true}).count(),1);
 // A server refusal also preserves the draft and permits a deliberate retry.
 await page.locator('#subscriberSavePlan').dispatchEvent('submit');await waitPending();pending('fail');
 await page.getByText('Plan save unconfirmed: Synthetic validation failure Refresh the plan list before retrying.',{exact:true}).waitFor();
 await page.getByRole('button',{name:'Edit limit-2',exact:true}).click();
 await page.locator('#subLimitDown').fill('600000');await page.locator('#subscriberAddLimit').click();
 await page.locator('#subscriberSavePlan').dispatchEvent('submit');await waitPending();
 assert.equal(posts,3);assert.equal(submitted[0].plan.policies[0].limits[0].down,'800000');assert.equal(submitted[2].plan.policies[0].limits[0].down,'600000');
 assert.equal(submitted[2].plan.policies[0].limits[0].amount,'9007199254740993');
 assert.equal(submitted[2].plan.policies[0].limits[0].period.zone,'UTC');
 assert.deepEqual(submitted[2].plan.policies[0].limits.map(limit=>limit.id),['limit-2','limit-3']);
 pending('success');await page.getByText('Plan version saved. Existing subscriptions keep their assigned version.',{exact:true}).waitFor();
 assert.match(await page.locator('#subscriberPlanList').textContent(),/Synthetic خطة <script>/);
 // Escape data, keyboard focus and the existing three palettes at phone/desktop widths.
 await page.getByRole('button',{name:'Edit limit-2',exact:true}).click();
 assert.equal(await page.locator('#subLimitAmount').evaluate(node=>document.activeElement===node),true);
 const checks=[];
 for(const appearance of [{theme:'dark',palette:''},{theme:'light',palette:''},{theme:'dark',palette:'nord'}])for(const width of [375,1280]) {
  await page.setViewportSize({width,height:1000});
  await page.evaluate(({theme,palette})=>{document.documentElement.dataset.theme=theme;document.documentElement.dataset.bsTheme=theme;if(palette)document.documentElement.dataset.palette=palette;else delete document.documentElement.dataset.palette;},appearance);
  const result=await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();
  assert.deepEqual(result.violations,[]);
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'horizontal overflow');
  const name=`plan-editor-${appearance.theme}-${appearance.palette||'default'}-${width}`;
  await page.screenshot({path:path.join(out,name+'.png'),fullPage:true});
  await page.locator('#subscriberAddLimit').scrollIntoViewIfNeeded();
  await page.screenshot({path:path.join(out,name+'-controls.png')});checks.push({name,violations:result.violations.length});
 }
 assert.deepEqual(errors,[]);writeFileSync(path.join(out,'result.json'),JSON.stringify({syntheticAPI:true,posts,checks},null,2));
 console.log('PASS: plan draft add/edit/cancel/remove, exact quantities, pending/interrupted/repeated saves, 6 theme/viewport axe checks');
}finally {await browser.close();await new Promise(resolve=>server.close(resolve));}
