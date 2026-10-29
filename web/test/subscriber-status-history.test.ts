import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createStatusHistory, historyMarkup, type HistoryState } from '../src/pages/subscriber-status-history';
const tick=()=>new Promise(resolve=>setImmediate(resolve));
const event=(id:string)=>({id,actor:'<script>admin</script>',action:'account.suspended',reason:'<img src=x onerror=alert(1)>',createdAt:100});
test('history isolates late account results and suppresses repeated pending requests',async()=>{
 const requests:{path:string;resolve:(v:unknown)=>void}[]=[];let state!:HistoryState;
 const c=createStatusHistory(<T>(path:string)=>new Promise<T>(resolve=>requests.push({path,resolve:resolve as (v:unknown)=>void})),s=>state=s);
 c.open('A','Account A');c.refresh();c.retry();assert.equal(requests.length,1);
 c.open('B','Account B');requests[1].resolve({accountId:'B',events:[]});await tick();
 requests[0].resolve({accountId:'A',events:[event('99')]});await tick();
 assert.equal(state.accountId,'B');assert.equal(state.events.length,0);assert.equal(state.busy,false);
 assert.match(historyMarkup(state),/No status changes recorded/);
});
test('history preserves exact cursor and retained rows through error/retry',async()=>{
 const calls:string[]=[];let state!:HistoryState;let fail=true;
 const rows=Array.from({length:50},(_,i)=>event(String(9007199254741099n-BigInt(i))));
 const c=createStatusHistory(async<T>(path:string)=>{calls.push(path);if(calls.length===1)return {accountId:'A',events:rows,nextBefore:rows[49].id} as T;if(fail)throw new Error('Connection interrupted');return {accountId:'A',events:[event('9007199254741000')]} as T;},s=>state=s);
 c.open('A','<Account>');await tick();c.more();c.more();await tick();
 assert.equal(calls.length,2);assert.match(calls[1],/before=9007199254741050$/);assert.equal(state.events.length,50);assert.match(state.error,/interrupted/);
 fail=false;c.retry();await tick();assert.equal(calls[2],calls[1]);assert.equal(state.events.length,51);assert.equal(state.next,undefined);
 const html=historyMarkup(state);assert.ok(!html.includes('<script>'));assert.ok(!html.includes('<img'));assert.match(html,/&lt;Account&gt;/);assert.match(html,/9007199254741000/);
});
test('history rejects wrong account, duplicate ordering and malformed cursors without leaking records',async()=>{
 for(const page of [{accountId:'B',events:[event('9')]},{accountId:'A',events:[event('9'),event('9')]},{accountId:'A',events:[event('9')],nextBefore:'9'},{accountId:'A',events:[event('01')]}]){
  let state!:HistoryState;const c=createStatusHistory(async<T>()=>page as T,s=>state=s);c.open('A','A');await tick();assert.ok(state.error);assert.equal(state.events.length,0);
 }
});
test('stale rejection cannot replace the new account loading or result state',async()=>{
 const requests:{resolve:(v:unknown)=>void;reject:(e:Error)=>void}[]=[];let state!:HistoryState;
 const c=createStatusHistory(<T>()=>new Promise<T>((resolve,reject)=>requests.push({resolve:resolve as (v:unknown)=>void,reject})),s=>state=s);
 c.open('A','Account A');c.open('B','Account B');
 requests[0].reject(new Error('Old account denied'));await tick();
 assert.equal(state.accountId,'B');assert.equal(state.busy,true);assert.equal(state.error,'');assert.equal(state.events.length,0);
 requests[1].resolve({accountId:'B',events:[event('99')]});await tick();
 assert.equal(state.busy,false);assert.equal(state.events[0].id,'99');assert.equal(state.error,'');
});
test('initial-page failure clears prior account rows and retry requests the same first page',async()=>{
 const calls:string[]=[];let state!:HistoryState;let fail=true;
 const c=createStatusHistory(async<T>(path:string)=>{calls.push(path);if(path.endsWith('A'))return {accountId:'A',events:[event('99')]} as T;if(fail)throw new Error('Subscriber administration permission required');return {accountId:'B',events:[]} as T;},s=>state=s);
 c.open('A','Account A');await tick();c.open('B','Account B');await tick();
 assert.equal(state.events.length,0);assert.equal(state.loaded,false);assert.equal(state.busy,false);assert.match(historyMarkup(state),/Subscriber administration permission required/);assert.match(historyMarkup(state),/Retry page/);
 fail=false;c.retry();await tick();assert.equal(calls[1],calls[2]);assert.equal(state.loaded,true);assert.equal(state.error,'');assert.equal(state.events.length,0);
});
