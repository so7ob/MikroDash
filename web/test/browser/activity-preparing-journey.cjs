const assert=require('node:assert/strict');
async function run(page,settled,extended=false){
 // The actual page must continue an explicit 202 preparation response itself.
 let preparingRequests=0,completePreparation=false;
 await page.route('**/api/activity/query?**',async route=>{
  const response=await route.fetch();const body=await response.json();
  if(!completePreparation){preparingRequests++;await route.fulfill({status:202,contentType:'application/json',headers:{'Retry-After':'1'},body:JSON.stringify({...body,preparing:true,preparationPending:Math.max(1,1500-preparingRequests*100),refreshAt:Date.now()+1000})});}
  else await route.fulfill({response});
 });
 const beforePreparationURL=page.url();
 await page.locator('#activityRefresh').focus();await page.keyboard.press('Enter');
 await page.waitForFunction(()=>document.querySelector('#activityPreparation')?.textContent.includes('Preparing saved activity'));
 assert.ok(await page.locator('#activityPreparation').isVisible(),'preparation hidden outside Synchronization');
 assert.equal(await page.locator('#activityMetrics').innerHTML(),'','preparation displayed zero KPIs');
 assert.equal(await page.locator('#activityCount').textContent(),'—');
 assert.equal(await page.locator('#activityCoverage').textContent(),'');
 assert.equal(await page.locator('#activityComparison').textContent(),'');
 assert.equal(await page.locator('#activityError').textContent(),'');
 assert.equal(page.url(),beforePreparationURL,'preparation lost filters');
 assert.equal(await page.evaluate(()=>document.activeElement.id),'activityRefresh');
 for(const width of extended?[375,768,1280,1920]:[375,1280]){
  for(const theme of extended?['dark','light']:['dark']){
   await page.setViewportSize({width,height:1000});await page.evaluate(theme=>{document.documentElement.dataset.theme=theme},theme);
   await page.screenshot({path:'/tmp/mikrodash-activity-preparing-'+width+'-'+theme+'.png',fullPage:true});
   assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'preparing overflow');
   if(extended){const {AxeBuilder}=require('@axe-core/playwright');const result=await new AxeBuilder({page}).include('#page-activity').withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();assert.deepEqual(result.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>({target:n.target,html:n.html}))})),[],width+' '+theme+' preparation axe');}
  }
 }
 await page.evaluate(()=>{delete document.documentElement.dataset.theme});
 await page.waitForResponse(r=>new URL(r.url()).pathname==='/api/activity/query'&&r.status()===202);
 assert.ok(preparingRequests>=2,'preparation did not retry automatically');
 completePreparation=true;
 await page.waitForResponse(r=>new URL(r.url()).pathname==='/api/activity/query'&&r.status()===200);
 await page.waitForFunction(()=>document.querySelector('#activityPreparation')?.hidden&&document.querySelector('#activityMetrics')?.childElementCount>0);
 await page.unroute('**/api/activity/query?**');await settled();

}
module.exports=run;

if(require.main===module){
 (async()=>{
  const {chromium}=require('playwright');const browser=await chromium.launch({headless:true,args:['--no-sandbox']});
  try {
   const base=process.argv[2];const context=await browser.newContext({viewport:{width:1280,height:1000}});
   await context.addCookies([{name:'mikrodash_sid',value:'activity-test-token',url:base}]);
   await context.request.post(base+'/fixture/append');const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
   await page.goto(base+'/journey?routerId=r1');
   const settled=()=>page.waitForFunction(()=>document.querySelector('#page-activity')?.getAttribute('aria-busy')==='false');await settled();
   await run(page,settled,true);
   for(const [width,theme] of [[375,'light'],[1280,'dark']]){
    await page.setViewportSize({width,height:1000});await page.evaluate(theme=>{document.documentElement.dataset.theme=theme},theme);
    await page.screenshot({path:'/tmp/mikrodash-activity-prepared-'+width+'-'+theme+'.png',fullPage:true});
   }
   assert.deepEqual(errors,[]);console.log('preparation: visible 202 progress, automatic complete retry, unchanged filters/focus, no stale metrics, four widths and both themes with zero axe violations passed');
  }finally{await browser.close()}
 })().catch(e=>{console.error(e);process.exit(1)});
}
