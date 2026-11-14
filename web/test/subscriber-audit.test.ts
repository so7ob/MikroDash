import assert from 'node:assert/strict';
import {test} from 'node:test';
import {createAccountAudit,accountAuditMarkup,type AuditEvent,type AuditFilters,type AuditState} from '../src/pages/subscriber-audit';
const tick=()=>new Promise(resolve=>setImmediate(resolve));
const filters:AuditFilters={category:'all',from:'',until:''};
const event=(id:string):AuditEvent=>({id,actor:'Synthetic <script>actor</script>',action:'account.suspended',target:'A',reason:'سبب <img src=x onerror=alert(1)>',createdAt:100,evidenceState:'complete'});
const page=(accountId:string,events:AuditEvent[]=[],rest:Record<string,unknown>={})=>({accountId,category:'all',from:'',until:'',events,...rest});
const settled=(events:AuditEvent[]):AuditState=>({accountId:'A',label:'Synthetic اسم <script>',filters,events,busy:false,loaded:true,error:''});

test('account audit retains exact keyset rows through interruption and same-page retry',async()=>{
 let state!:AuditState;const paths:string[]=[];let fail=true;
 const first=Array.from({length:50},(_,i)=>event(String(9007199254741099n-BigInt(i))));
 const c=createAccountAudit(async<T>(path:string)=>{paths.push(path);if(paths.length===1)return page('A',first,{nextBefore:first[49].id}) as T;if(fail)throw new Error('Synthetic connection interrupted');return page('A',[event('9007199254741000')]) as T;},s=>state=s);
 c.open('A','Synthetic A');c.refresh();c.retry();await tick();assert.equal(paths.length,1);
 c.more();c.more();await tick();assert.equal(paths.length,2);assert.match(paths[1],/before=9007199254741050$/);assert.equal(state.events.length,50);assert.match(state.error,/interrupted/);
 fail=false;c.retry();await tick();assert.equal(paths[1],paths[2]);assert.equal(state.events.length,51);assert.equal(state.next,undefined);assert.equal(state.events[50].id,'9007199254741000');
});

test('account and closed-dialog generations fence late success and rejection',async()=>{
 for(const rejectOld of [false,true]){
  const requests:{resolve:(v:unknown)=>void;reject:(e:Error)=>void}[]=[];let state!:AuditState;
  const c=createAccountAudit(<T>()=>new Promise<T>((resolve,reject)=>requests.push({resolve:resolve as(v:unknown)=>void,reject})),s=>state=s);
  c.open('A','A');c.close();c.open('B','B');
  if(rejectOld)requests[0].reject(new Error('Synthetic old private error'));else requests[0].resolve(page('A',[event('99')]));await tick();assert.equal(state.accountId,'B');assert.equal(state.busy,true);assert.equal(state.error,'');assert.equal(state.events.length,0);
  requests[1].resolve(page('B'));await tick();assert.equal(state.loaded,true);assert.equal(state.busy,false);assert.equal(state.events.length,0);
  c.refresh();c.close();requests[2].resolve(page('B'));await tick();assert.equal(state.accountId,'');assert.equal(state.loaded,false);
 }
});

test('server filters form a detached exact review and supersede in-flight pages',async()=>{
 const requests:{path:string;resolve:(v:unknown)=>void}[]=[];let state!:AuditState;
 const c=createAccountAudit(<T>(path:string)=>new Promise<T>(resolve=>requests.push({path,resolve:resolve as(v:unknown)=>void})),s=>state=s);
 c.open('A','A');const requested={category:'credentials',from:'2026-11-14',until:'2026-11-14'} as const;c.apply(requested);
 assert.equal(requests.length,2);assert.match(requests[1].path,/category=credentials&from=2026-11-14&until=2026-11-14/);
 requests[0].resolve(page('A',[event('99')]));await tick();assert.equal(state.busy,true);assert.equal(state.events.length,0);
 const credential={...event('101'),action:'account.credential.reset',reason:'{"reason":"Synthetic reset","beforeRevision":"0","afterRevision":"1"}'} as AuditEvent;
 requests[1].resolve(page('A',[credential],requested));await tick();assert.equal(state.error,'');assert.deepEqual(state.filters,requested);assert.equal(state.events[0].id,'101');
 c.apply({category:'profile',from:'',until:''});assert.equal(state.events.length,0);assert.equal(state.loaded,false);requests[2].resolve(page('A',[],{category:'profile'}));await tick();assert.equal(state.events.length,0);assert.equal(state.loaded,true);
});

test('malformed, mismatched, unordered and unbounded audit pages fail without exposing rows',async()=>{
 const base=event('9');
 for(const bad of [page('B',[base]),page('A',[{...base,target:'B'}]),page('A',[base,base]),page('A',[event('01')]),page('A',[{...base,id:9}]),page('A',[{...base,id:'9223372036854775808'}]),page('A',[base],{category:'credentials'}),page('A',[base],{from:'2026-11-14'}),page('A',[base],{nextBefore:'9'}),page('A',[{...base,createdAt:1e18}]),page('A',[{...base,evidenceState:'unknown'}]),page('A',[{...base,evidenceState:'unavailable',reason:'Synthetic hidden'}]),page('A',[{...base,action:'subscription.assign',subscriptionId:'S'}]),page('A',[{...base,planId:'synthetic-invalid-owner'}]),page('A',[{...base,reason:'x'.repeat(8193)}]),page('A',Array.from({length:51},(_,i)=>event(String(100-i))))]){
  let state!:AuditState;const c=createAccountAudit(async<T>()=>bad as T,s=>state=s);c.open('A','A');await tick();assert.ok(state.error);assert.equal(state.loaded,false);assert.deepEqual(state.events,[]);
 }
});

test('a category-labelled page containing a different action is rejected',async()=>{
 const requests:{resolve:(v:unknown)=>void}[]=[];let state!:AuditState;
 const c=createAccountAudit(<T>()=>new Promise<T>(resolve=>requests.push({resolve:resolve as(v:unknown)=>void})),s=>state=s);
 c.open('A','A');c.apply({category:'credentials',from:'',until:''});requests[1].resolve(page('A',[event('9')],{category:'credentials'}));await tick();assert.ok(state.error);assert.equal(state.events.length,0);
 requests[0].resolve(page('A'));await tick();assert.ok(state.error);
});

test('initial failure and malformed later page preserve the correct scope and retry cursor',async()=>{
 let state!:AuditState;let attempts=0;const paths:string[]=[];const rows=Array.from({length:50},(_,i)=>event(String(100-i)));
 const c=createAccountAudit(async<T>(path:string)=>{paths.push(path);attempts++;if(attempts===1)throw new Error('Synthetic permission refused');if(attempts===2)return page('A',rows,{nextBefore:'51'}) as T;if(attempts===3)throw new SyntaxError('Synthetic incomplete JSON');return page('A') as T;},s=>state=s);
 c.open('A','A');await tick();assert.match(accountAuditMarkup(state),/Synthetic permission refused/);assert.match(accountAuditMarkup(state),/Retry page/);assert.equal(state.events.length,0);
 c.retry();await tick();assert.equal(paths[0],paths[1]);c.more();await tick();assert.equal(state.events.length,50);assert.equal(state.next,'51');assert.match(state.error,/JSON/);c.retry();await tick();assert.equal(paths[2],paths[3]);assert.equal(state.events.length,50);assert.equal(state.next,undefined);
});

test('audit rendering escapes Unicode evidence and explains exact stored changes',()=>{
 const events:AuditEvent[]=[{...event('9007199254740999'),action:'account.name.edit',reason:JSON.stringify({reason:'سبب <script>x()</script>',before:'اسم قديم',after:'اسم جديد <img>'})},{...event('9007199254740998'),action:'account.status.batch',reason:JSON.stringify({reason:'Synthetic review',before:'active',after:'suspended',batchId:'synthetic-batch'})},{...event('9007199254740997'),action:'account.credential.reset',reason:JSON.stringify({reason:'Synthetic reset',beforeRevision:'9007199254740993',afterRevision:'9007199254740994'})},{...event('9007199254740996'),action:'subscription.assign',target:'S',subscriptionId:'S',planId:'Synthetic <plan>',planVersion:2}];
 const html=accountAuditMarkup(settled(events));assert.ok(!html.includes('<script>'));assert.ok(!html.includes('<img>'));assert.match(html,/اسم جديد &lt;img&gt;/);assert.match(html,/Before: active · After: suspended/);assert.match(html,/Batch ID: synthetic-batch/);assert.match(html,/9007199254740993 → 9007199254740994/);assert.match(html,/Plan: <bdi>Synthetic &lt;plan&gt;<\/bdi> · v2/);assert.match(html,/Original audit evidence/);assert.match(html,/&quot;beforeRevision&quot;:&quot;9007199254740993&quot;/);
});

test('legacy evidence and missing before-states never invent profile or network application',()=>{
 const html=accountAuditMarkup(settled([{...event('9'),action:'account.create'},{...event('8'),action:'account.name.edit',reason:'{broken'},{...event('7'),action:'speed.request',target:'S',subscriptionId:'S',planId:'P',planVersion:1}]));assert.match(html,/Historical before-state: Not recorded/);assert.ok(!html.includes('Before: <bdi>'));assert.match(html,/Live application is not confirmed/);
});

test('unsafe credential evidence has no raw fallback and truncation is explicit',()=>{
 const html=accountAuditMarkup(settled([{...event('9'),action:'account.credential.reset',evidenceState:'unavailable',reason:'synthetic-private-material'},{...event('8'),evidenceState:'truncated'}]));assert.ok(!html.includes('synthetic-private-material'));assert.match(html,/Historical evidence unavailable/);assert.match(html,/truncated to 8,192 characters/);
});

test('empty and loading cards distinguish measured zero from not loaded',()=>{
 const empty=settled([]);assert.match(accountAuditMarkup(empty),/No audit events match these filters/);assert.match(accountAuditMarkup(empty),/Counts apply to loaded events/);
 const loading=accountAuditMarkup({...empty,loaded:false,busy:true});assert.match(loading,/Loading account audit/);assert.match(loading,/fa-stat-val">—/);
});


test('original evidence preserves control characters using a labelled escaped representation',()=>{
 const html=accountAuditMarkup(settled([{...event('9'),action:'account.create',reason:'Original\u0000after'.replace('\\u0000','\u0000')} ]));
 assert.match(html,/JSON-escaped control characters/);assert.match(html,/\\u0000/);
});
