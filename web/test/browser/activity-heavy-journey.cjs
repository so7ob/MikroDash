const {chromium}=require('playwright');
const {AxeBuilder}=require('@axe-core/playwright');
const assert=require('node:assert/strict');

(async()=>{
 const browser=await chromium.launch({headless:true});
 try {
  const context=await browser.newContext({viewport:{width:1280,height:1000}});
  await context.addCookies([{name:'mikrodash_sid',value:'activity-test-token',url:process.argv[2]}]);
  const seed=await context.request.post(process.argv[2]+'/fixture/dense');
  assert.equal(seed.status(),200);assert.equal((await seed.json()).stored,4200);
  const page=await context.newPage();const errors=[];const responses=[];
  page.on('pageerror',e=>errors.push(e.message));
  page.on('response',async r=>{
   const u=new URL(r.url());
   if(u.pathname==='/api/activity/query'&&u.searchParams.get('parts')==='overview') {
    const data=await r.json();responses.push({status:r.status(),data});
   }
  });
  await page.goto(process.argv[2]+'/journey?routerId=r1&tab=summary&days=30');
  await page.waitForFunction(()=>document.body.textContent.includes('Preparing'),{},{timeout:30000});
  await page.screenshot({path:'/tmp/mikrodash-activity-109-preparing.png',fullPage:true});
  await new Promise((resolve,reject)=>{
   const deadline=Date.now()+120000;
   const timer=setInterval(()=>{
    if(responses.some(r=>r.status===200&&!r.data.preparing&&r.data.totals.dns===4200)){clearInterval(timer);resolve();}
    else if(Date.now()>deadline){clearInterval(timer);reject(new Error('complete heavy result did not render'));}
   },100);
  });
  assert.ok(responses.filter(r=>r.status===202).length>=7,'did not exercise all retained phases');
  for(const r of responses.filter(r=>r.status===202)) {
   assert.equal(r.data.preparing,true);assert.equal(r.data.totals.dns,0);
   assert.equal(r.data.rows.length,0);assert.equal(r.data.series.length,0);
  }
  await page.waitForFunction(()=>document.querySelector('#page-activity')?.getAttribute('aria-busy')==='false'&&!document.body.textContent.includes('Preparing'));
  assert.equal(await page.getByText('Activity query failed',{exact:false}).count(),0);
  for(const width of [375,768,1280,1920]) {
   await page.setViewportSize({width,height:1000});
   for(const theme of ['dark','light']) {
    await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);
    await page.screenshot({path:`/tmp/mikrodash-activity-109-${width}-${theme}.png`,fullPage:true});
    const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1);
    assert.equal(overflow,false,`page overflow at ${width}/${theme}`);
    // This standalone component fixture omits the application shell landmarks;
    // use the same component/WCAG scope as the full Activity browser journey.
    const axe=await new AxeBuilder({page}).include('#page-activity').withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();
    assert.deepEqual(axe.violations.map(v=>v.id),[],`axe ${width}/${theme}`);
   }
  }
  assert.deepEqual(errors,[]);
  console.log(`real heavy HTTP/UI: ${responses.filter(r=>r.status===202).length} resumable responses, complete 4200-event result, no partial charts, 375/768/1280/1920 dark/light, eight zero-violation axe checks`);
 } finally {await browser.close();}
})().catch(e=>{console.error(e);process.exit(1)});
