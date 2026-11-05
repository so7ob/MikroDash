import { el, esc, pill } from '../dom';
import { fieldHtml } from '../form';
import { quantity } from '../subscriber-view';

type API = <T>(path: string, body?: unknown) => Promise<T>;
type Gap = {id:string;leaseId:string;limitId:string;from:string;until:string;amount:string;periods:string[];resolvedAt:string};
type Legacy = {id:string;limitId:string;periodStart:string;amount:string;resolvedAt:string;settledAmount:string};
type Review = {leaseId:string;network:string;device:string;seconds:string;download:string;upload:string;unresolvedLimits:number};
type Receipt = {id:string;network:string;device:string;sessionId:string;status:string;state:string;reason:string;seconds:string;download:string;upload:string;timeKnown:boolean;downloadKnown:boolean;uploadKnown:boolean;receivedAt:string;fields:Record<string,string>};
type View = {gaps:Gap[];legacy:Legacy[];sessions:Review[];receipts:Receipt[]};
function receiptDetails(r:Receipt):string {
 const reading=(raw:string,known:boolean,kind:string):string=>known&&/^\d+$/.test(raw)?quantity(raw,kind):'Not supplied or malformed';
 const reasons:Record<string,string>={'statistics-incomplete':'Some accounting statistics are missing, malformed or outside the supported range.','prior-reading-not-covered':'Earlier recorded counters are not covered by this final reading.','apply-failed':'The reliable reading could not be applied. Its original receipt is retained for retry.','covered-by-committed-final-stop':'A committed complete Stop covers this late packet.'};
 return `<details class="mb-2"><summary>Accounting ${esc(r.status)} ${pill(r.state,r.state==='pending'?'warn':'ok')}</summary><p class="text-break">${esc(r.device)} · ${esc(r.network)} · Session ${esc(r.sessionId)} · Received ${esc(r.receivedAt)}</p><p>${esc(reasons[r.reason]||r.reason)}</p><p>Time: ${esc(reading(r.seconds,r.timeKnown,'seconds'))} · Download: ${esc(reading(r.download,r.downloadKnown,'download'))} · Upload: ${esc(reading(r.upload,r.uploadKnown,'upload'))}</p><details><summary>Original accounting fields</summary><pre class="text-wrap text-break">${esc(JSON.stringify(r.fields,null,2))}</pre></details></details>`;
}
const path='/api/subscribers/reconciliation';
const unresolved=(raw:string):boolean=>raw.startsWith('0001-');
function field(id:string,label:string,value='',help=''):string {
 return fieldHtml({id,label,help},`<input id="${esc(id)}" name="${esc(id)}" class="sform-input" value="${esc(value)}" required>`);
}
function common(id:string):string {
 return field(id+'-reason','Reason','','Required for the audit trail.')+field(id+'-evidence','Evidence reference','','Record the reading or explicit approval basis. This does not turn an estimate into measured traffic.');
}
export function initSubscriberReconciliation(api:API):(accounts:{id:string;username:string}[])=>void {
 const panel=el('subscriberReconciliation');if(!panel)return()=>{};
 panel.innerHTML=`<div class="ff-grid">${fieldHtml({id:'subReviewAccount',label:'Subscriber',help:'Review one account at a time.'},'<select id="subReviewAccount" class="sform-input"></select>')}</div><button id="subReviewLoad" class="sbtn sbtn-outline" type="button">Load reconciliation</button><p id="subReviewNotice" role="status" aria-live="polite"></p><div id="subReviewRows"></div>`;
 const say=(message:string):void=>{const n=el('subReviewNotice');if(n)n.textContent=message;};
 const account=():string=>el<HTMLSelectElement>('subReviewAccount')?.value||'';
 async function load():Promise<void>{
  if(!account()){say('Choose a subscriber.');return;}
  const requestedAccount=account();
  say('Loading reconciliation records…');
  try{
   const data=await api<View>(path+'?account='+encodeURIComponent(requestedAccount));if(account()!==requestedAccount)return;const rows=el('subReviewRows');if(!rows)return;rows.dataset.account=requestedAccount;
   rows.innerHTML=data.receipts.map(receiptDetails).join('')+data.sessions.map(s=>{const id='session-'+s.leaseId;return `<section><h4>Final accounting ${pill('review required','warn')}</h4><p>${esc(s.device)} · ${esc(s.network)} · ${s.unresolvedLimits} unresolved limit holds</p><form data-review="missing-stop" data-target="${esc(s.leaseId)}" data-fields="${id}"><div class="ff-grid">${field(id+'-seconds','Final cumulative seconds',s.seconds)}${field(id+'-download','Final cumulative download bytes',s.download)}${field(id+'-upload','Final cumulative upload bytes',s.upload)}${common(id)}</div><button class="sbtn sbtn-warn" type="submit">Approve final reading</button></form></section>`;}).join('')+
    data.gaps.map(g=>{const id='gap-'+g.id;return `<section><h4>Byte distribution ${pill(unresolved(g.resolvedAt)?'unresolved':'settled',unresolved(g.resolvedAt)?'warn':'ok')}</h4><p>${esc(g.limitId)} · ${esc(g.amount)} bytes · ${esc(g.from)} to ${esc(g.until)}</p>${unresolved(g.resolvedAt)?`<form data-review="byte-gap" data-target="${esc(g.id)}" data-fields="${id}"><p>The distribution was not measured. Allocate exactly ${esc(g.amount)} bytes across the eligible periods using a documented approval basis.</p><div class="ff-grid">${g.periods.map((p,i)=>fieldHtml({id:id+'-period-'+i,label:'Period '+p,help:'Decimal bytes. Allocations must add up to the original delta.'},`<input id="${id}-period-${i}" class="sform-input" data-reconcile-period="${esc(p)}" inputmode="numeric" pattern="[0-9]+" required>`)).join('')}${common(id)}</div><button class="sbtn sbtn-warn" type="submit">Approve distribution</button></form>`:''}</section>`;}).join('')+
    data.legacy.map(g=>{const id='legacy-'+g.id;return `<section><h4>Legacy conservative balance ${pill(unresolved(g.resolvedAt)?'review required':'settled',unresolved(g.resolvedAt)?'warn':'ok')}</h4><p>${esc(g.limitId)} · ${esc(g.periodStart)} · Original upper bound ${esc(g.amount)}</p>${unresolved(g.resolvedAt)?`<form data-review="legacy" data-target="${esc(g.id)}" data-fields="${esc(id)}"><div class="ff-grid">${field(id+'-amount','Approved period usage','','Enter a decimal quantity within the original conservative bound.')}${common(id)}</div><button class="sbtn sbtn-warn" type="submit">Approve period usage</button></form>`:`<p>Approved usage: ${esc(g.settledAmount)}</p>`}</section>`;}).join('');
   if(!rows.innerHTML)rows.textContent='No reconciliation records for this subscriber.';
   say('Loaded up to 1,000 records per category, with unresolved records first. Each approval preserves its original evidence.');
  }catch(error){say(error instanceof Error?error.message:'Unable to load reconciliation');}
 }
 el('subReviewAccount')?.addEventListener('change',()=>{const rows=el('subReviewRows');if(rows){rows.innerHTML='';delete rows.dataset.account;}say('Load this subscriber before approving a settlement.');});
 el('subReviewLoad')?.addEventListener('click',()=>void load());
 panel.addEventListener('submit',async event=>{
  const form=event.target;if(!(form instanceof HTMLFormElement)||!form.dataset.review)return;event.preventDefault();
  if(el('subReviewRows')?.dataset.account!==account()){say('Reload this subscriber before approving a settlement.');return;}
  const id=form.dataset.fields||'';const value=(suffix:string):string=>el<HTMLInputElement>(id+'-'+suffix)?.value||'';
  if(!confirm('Approve this accounting settlement? The original evidence and this approval will be retained.'))return;
  const body:{action:string;id:string;reason:string;evidence:string;amount?:string;allocations?:{periodStart:string;amount:string}[];reading?:{leaseId:string;seconds:string;download:string;upload:string}}={action:form.dataset.review,id:form.dataset.target||'',reason:value('reason'),evidence:value('evidence')};
  if(body.action==='byte-gap')body.allocations=Array.from(form.querySelectorAll<HTMLInputElement>('[data-reconcile-period]')).map(n=>({periodStart:n.dataset.reconcilePeriod||'',amount:n.value}));
  if(body.action==='legacy')body.amount=value('amount');
  if(body.action==='missing-stop')body.reading={leaseId:body.id,seconds:value('seconds'),download:value('download'),upload:value('upload')};
  const button=form.querySelector('button');if(button)button.disabled=true;
  say('Recording settlement…');
  try{await api(path,body);await load();say('Settlement approved and recorded.');el('subReviewLoad')?.focus();}catch(error){say(error instanceof Error?error.message:'Settlement failed');}finally{if(button)button.disabled=false;}
 });
 return accounts=>{const picker=el<HTMLSelectElement>('subReviewAccount');if(!picker)return;const previous=picker.value;picker.innerHTML=accounts.map(a=>`<option value="${esc(a.id)}">${esc(a.username)}</option>`).join('');if(accounts.some(a=>a.id===previous))picker.value=previous;};
}
