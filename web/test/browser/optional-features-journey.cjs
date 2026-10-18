// Run with NODE_PATH=/usr/lib/node_modules node web/test/browser/optional-features-journey.cjs.
// Every HTTP response and socket event is synthetic. No external router connection exists.
const {chromium}=require('playwright');
const {AxeBuilder}=require('@axe-core/playwright');
const assert=require('node:assert/strict');
const fs=require('node:fs');const path=require('node:path');const http=require('node:http');
const {execFileSync}=require('node:child_process');
const root=path.resolve(__dirname,'../../..');
const dir=fs.mkdtempSync('/tmp/mikrodash-optional-');
execFileSync(path.join(root,'web/node_modules/.bin/esbuild'),[path.join(__dirname,'optional-features-entry.ts'),'--bundle','--format=esm','--outfile='+dir+'/entry.js','--log-level=error']);
const now=Date.now();let recording=false;let lists=[];let watches=[];const requests=[];
const routes=(p,method,b)=>{
 if(p==='/api/routers')return {routers:[{id:'synthetic',label:'Synthetic router',host:'198.51.100.1'}]};
 if(p==='/api/dns/lists'){if(method==='POST')lists=[{...b,id:1,routerIds:b.routerIds||[],lastResult:'',lastSyncAt:0,dropped:[]}];return {lists,ok:true};}
 if(p==='/api/dns/cache')return {total:1,cached:1,rows:[{name:'synthetic.example',type:'A',data:'198.51.100.2',ttl:'1m'}]};
 if(p==='/api/dns/trace')return {name:'synthetic.example',matches:[],answering:[],steps:[{step:'Route',state:'ok',text:'Synthetic route is active'}]};
 if(p==='/api/dns/bulk')return {id:'job1',state:'stopped',total:2,done:1,added:1,changed:0,removed:0,failed:1,unconfirmed:1,waitingUntil:0,errors:['stopped: permission no longer granted']};
 if(p==='/api/watch'){if(method==='POST')watches=[{...b,id:1,known:true,present:true}];if(method==='DELETE')watches=[];return {devices:watches,ok:true};}
 if(p==='/api/wifi/history/record'){recording=b.on;return {recording};}
 if(p==='/api/wifi/history/status')return {recording};
 if(p==='/api/wifi/history')return {from:now-120000,to:now,samples:[{ts:now-60000,mac:'02:00:00:00:00:01',iface:'wifi1',ap:'Synthetic AP',ssid:'Synthetic',signal:-55,txRate:'866Mbps'},{ts:now,mac:'02:00:00:00:00:01',iface:'wifi2',ap:'Synthetic AP two',ssid:'Synthetic',signal:-60,txRate:'866Mbps'}]};
 if(p==='/api/capsman/version')return {version:'7.24'};
 if(p==='/api/capsman/upgrade')return {ok:true};
 throw Error('Unexpected API '+method+' '+p);
};
const server=http.createServer(async(req,res)=>{
 try{
 const u=new URL(req.url,'http://localhost');
 if(u.pathname.startsWith('/api/')){let body='';for await(const chunk of req)body+=chunk;const b=body?JSON.parse(body):{};requests.push({path:u.pathname,method:req.method,body:b});res.setHeader('Content-Type','application/json');return res.end(JSON.stringify(routes(u.pathname,req.method,b)));}
 if(u.pathname==='/entry.js'){res.setHeader('Content-Type','text/javascript');return res.end(fs.readFileSync(dir+'/entry.js'));}
 if(u.pathname==='/journey'){
 const p=u.searchParams.get('page');let markup=fs.readFileSync(path.join(root,'web/src/ui/page-'+p+'.html'),'utf8');
 if(p==='dashboard')markup='<div id="page-dashboard"><div id="dash-grid-root">'+markup.slice(markup.indexOf('<div id="dc-card-domainroute"'),markup.indexOf('<!-- dc-card-secscore:')).replace('id="dc-card-domainroute"','id="dc-card-domainroute" style="grid-column:1 / span 24;grid-row:1 / span 8"')+'</div></div>';
 res.setHeader('Content-Type','text/html');return res.end('<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Optional features verification</title><link rel="stylesheet" href="/vendor/fonts/fonts.css"><link rel="stylesheet" href="/css/app-fonts.css"><link rel="stylesheet" href="/vendor/tabler.min.css"><link rel="stylesheet" href="/app.css"><link rel="stylesheet" href="/css/dashboard-grid.css"></head><body>'+markup+'<script type="module" src="/entry.js"></script></body></html>');}
 const file=path.join(root,'web/public',u.pathname);if(!file.startsWith(root+'/web/public/'))throw Error('invalid path');
 res.setHeader('Content-Type',u.pathname.endsWith('.css')?'text/css':u.pathname.endsWith('.woff2')?'font/woff2':'application/octet-stream');res.end(fs.readFileSync(file));
 }catch(e){res.statusCode=500;res.end(String(e));}
});
const dns={ts:now,pollMs:10000,available:true,settings:{servers:['198.51.100.53'],dynamicServers:[],dohEnabled:false,dohUrl:'',dohVerifyCert:false,dohMaxServerConnections:5,dohMaxConcurrentQueries:50,dohTimeout:'5s',allowRemoteRequests:true,cacheSize:8192,cacheUsed:100,cacheMaxTtl:'1w',maxUdpPacketSize:4096,maxConcurrentQueries:100,queryServerTimeout:'2s',queryTotalTimeout:'10s',mdnsRepeatIfaces:[],vrf:'main',addressListExtraTime:'4h'},staticEntries:[{id:'*1',name:'synthetic.example',regexp:'',address:'198.51.100.2',type:'A',ttl:'1d',disabled:false,comment:'Synthetic fixture',addressList:'',matchSubdomain:false,dynamic:false}],forwarders:[],forwardersAvailable:true,adlists:[],adlistsAvailable:true};
(async()=>{await new Promise(r=>server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+server.address().port;const browser=await chromium.launch({headless:true});
try{
 const context=await browser.newContext({viewport:{width:1280,height:1000}});const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
 const send=(e,d)=>page.evaluate(([e,d])=>fixture.send(e,d),[e,d]);
 const open=async(p)=>{await page.goto(base+'/journey?page='+p);await page.waitForFunction(()=>globalThis.fixture);};
 const audit=async(p,scope)=>{
 assert.equal(await page.locator(scope||'#page-'+p).isVisible(),true,p+' audit scope is hidden');
 for(const width of [375,1280])for(const theme of ['dark','light','nord']){
 await page.setViewportSize({width,height:1000});await page.evaluate(t=>fixture.theme(t),theme);
 const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1);assert.equal(overflow,false,p+' overflow '+width+' '+theme);
 const a=await new AxeBuilder({page}).include(scope||'#page-'+p).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze();fs.writeFileSync(dir+'/'+p+'-'+width+'-'+theme+'-axe.json',JSON.stringify(a.violations,null,2));assert.deepEqual(a.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)})),[],p+' '+width+' '+theme+' '+dir);
 await page.screenshot({path:dir+'/'+p+'-'+width+'-'+theme+'.png',fullPage:true});
 }console.log(p+': 375/1280 dark/light/Nord, axe and overflow passed');};
 await open('dns');assert.deepEqual(requests.filter(r=>r.path!=='/api/routers'),[],'disabled features made requests');assert.equal(await page.locator('#dnsCacheCard').isVisible(),false);
 await page.evaluate(()=>fixture.enable(true));await send('dns:update',dns);
 await page.locator('#dnsCacheSearch').fill('synthetic');await page.keyboard.press('Enter');await page.waitForFunction(()=>document.querySelector('#dnsCacheTable').textContent.includes('198.51.100.2'));
 await page.locator('#dnsTraceName').fill('synthetic.example');await page.keyboard.press('Enter');await page.waitForFunction(()=>document.querySelector('#dnsTraceOut').textContent.includes('Synthetic route'));
 await page.locator('#dnsListAdd').click();await page.locator('#dnsListName').fill('Synthetic list');await page.locator('#dnsListDomains').fill('synthetic.example');await page.locator('#dnsListAddr').fill('synthetic-vpn');await audit('dns');
 await page.locator('#dnsListSave').focus();await page.keyboard.press('Enter');await page.waitForFunction(()=>document.querySelector('#dnsListsTable').textContent.includes('Synthetic list'));
 await page.locator('.dns-pick').check();await page.locator('#dnsBulkDisable').click();await page.waitForFunction(()=>document.querySelector('#dnsBulkNote').textContent.includes('stopped'));
 await page.evaluate(()=>fixture.enable(false));assert.equal(await page.locator('#dnsListsCard').isVisible(),false);assert.equal(await page.locator('#dnsCacheCard').isVisible(),false);
 await open('wifi-clients');await page.evaluate(()=>fixture.enable(true));
 await send('wireless:update',{ts:now,pollMs:30000,mode:'wifi',capsmanAvailable:false,clients:[{mac:'02:00:00:00:00:01',signal:-55,iface:'wifi1',txRate:'866Mbps',rxRate:'866Mbps',band:'5GHz',standard:'Wi-Fi 6',ip:'198.51.100.2',uptime:'1h',ssid:'Synthetic',name:'Synthetic client'}],ssids:[],ssidsManagedElsewhere:0});
 await page.locator('[data-watch]').focus();await page.keyboard.press('Enter');await page.waitForFunction(()=>document.querySelector('#wlWatchBadge').textContent==='1');
 await page.locator('#wlRecToggle').click();await page.waitForFunction(()=>document.querySelector('#wlRecToggle').textContent==='Stop recording');
 await page.locator('[data-wlhist]').focus();await page.keyboard.press('Enter');await page.waitForSelector('#wlHistBody svg');await audit('wifi-clients');
 await open('capsman');await page.evaluate(()=>fixture.enable(true));
 await send('capsman:update',{ts:now,pollMs:10000,available:true,caps:[{identity:'Synthetic CAP',version:'7.23',boardName:'Synthetic',state:'Run',radios:[],clients:[],clientCount:0,legacy:false}],role:'manager',manager:{enabled:true},cap:{},totals:{caps:1,radios:0,clients:0},localRadios:[],profiles:{configuration:[],security:[],channel:[],datapath:[]},provisioning:[]});
 await page.waitForSelector('[data-capupgrade]');await audit('capsman');page.once('dialog',d=>d.accept());await page.locator('[data-capupgrade]').focus();await page.keyboard.press('Enter');await page.waitForFunction(()=>document.querySelector('#capsmanTable').textContent.includes('upgrading'));
 await open('settings');await page.locator('[data-tab=features]').click();assert.equal(await page.locator('#stab-features input[type=checkbox]:checked').count(),0);assert.equal(await page.locator('#stab-features input[type=checkbox]').count(),10);await audit('settings','#stab-features');await page.locator('#s_dnsBulkEnabled').focus();await page.keyboard.press('Space');assert.equal(await page.locator('#s_dnsBulkEnabled').isChecked(),true);
 await open('dashboard');await send('dnsrouting:state',{routerId:'synthetic',list:'synthetic-vpn',feeding:2,entries:3,ok:true,withheld:[],rules:[{chain:'prerouting',mark:'synthetic-vpn',packets:'12'}],routes:[{table:'synthetic-vpn',dst:'0.0.0.0/0',gateway:'198.51.100.1',active:true}]});await audit('dashboard','#dc-card-domainroute');
 assert.deepEqual(errors,[]);console.log('Keyboard: DNS cache/trace/list save, watch, recording, client history, CAP upgrade and feature checkbox passed. Screenshots: '+dir);
}finally{await browser.close();server.close();}})().catch(e=>{console.error(e);process.exitCode=1;server.close();});
