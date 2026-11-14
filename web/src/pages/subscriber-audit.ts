import { el, esc, pill } from '../dom';
import { fieldHtml } from '../form';
import { managedModal } from '../modals';
import { fmtTs } from '../timefmt';
import type { SubscriberAdminAccount } from './subscriber-account-actions';

export const auditActions = ['account.create','account.name.edit','account.active','account.suspended','account.status.batch','account.credential.reset','subscription.assign','speed.request'] as const;
export const auditCategories = {all:'All changes',profile:'Profile',status:'Status',credentials:'Credentials',subscriptions:'Subscriptions',speed:'Speed choices'} as const;
export type AuditFilters = Readonly<{category:keyof typeof auditCategories;from:string;until:string}>;
export type AuditEvent = {id:string;actor:string;action:typeof auditActions[number];target:string;reason:string;evidenceState:'complete'|'truncated'|'unavailable';createdAt:number;subscriptionId?:string;planId?:string;planVersion?:number};
type AuditPage = {accountId:string;category:string;from:string;until:string;events:AuditEvent[];nextBefore?:string};
export type AuditState = {accountId:string;label:string;filters:AuditFilters;events:AuditEvent[];next?:string;busy:boolean;loaded:boolean;error:string};
type API = <T>(path:string)=>Promise<T>;
const matchesCategory=(action:AuditEvent['action'],category:AuditFilters['category']):boolean=>category==='all'||category==='profile'&&['account.create','account.name.edit'].includes(action)||category==='status'&&['account.active','account.suspended','account.status.batch'].includes(action)||category==='credentials'&&action==='account.credential.reset'||category==='subscriptions'&&action==='subscription.assign'||category==='speed'&&action==='speed.request';
const initialFilters=():AuditFilters=>Object.freeze({category:'all',from:'',until:''});
const exactID=(v:unknown):v is string=>typeof v==='string'&&/^[1-9][0-9]{0,18}$/.test(v)&&BigInt(v)<=9223372036854775807n;

export function createAccountAudit(api:API,changed:(state:AuditState)=>void){
 let generation=0;
 let state:AuditState={accountId:'',label:'',filters:initialFilters(),events:[],busy:false,loaded:false,error:''};
 const emit=():void=>changed({...state,filters:{...state.filters},events:[...state.events]});
 async function load(before?:string):Promise<void>{
  if(state.busy||!state.accountId)return;
  const request=++generation,account=state.accountId,filters={...state.filters};state={...state,busy:true,error:''};emit();
  try{
   const query=new URLSearchParams({account,category:filters.category});if(filters.from)query.set('from',filters.from);if(filters.until)query.set('until',filters.until);if(before)query.set('before',before);
   const page=await api<AuditPage>('/api/subscribers/audit?'+query.toString());if(request!==generation)return;
   const invalid=():never=>{throw new Error('Unexpected account audit response. Retry this page.');};
   if(!page||page.accountId!==account||page.category!==filters.category||page.from!==filters.from||page.until!==filters.until||!Array.isArray(page.events)||page.events.length>50)invalid();
   let previous=before;
   for(const event of page.events){
    if(!event||!exactID(event.id)||previous!==undefined&&BigInt(event.id)>=BigInt(previous)||typeof event.actor!=='string'||typeof event.target!=='string'||!event.target||typeof event.reason!=='string'||Array.from(event.reason).length>8192||!auditActions.includes(event.action)||!matchesCategory(event.action,filters.category)||!['complete','truncated','unavailable'].includes(event.evidenceState)||event.evidenceState==='unavailable'&&event.reason!==''||!Number.isSafeInteger(event.createdAt)||!Number.isFinite(new Date(event.createdAt*1000).getTime()))invalid();
    const subscription=event.action==='subscription.assign'||event.action==='speed.request';
    if(subscription?typeof event.subscriptionId!=='string'||!event.subscriptionId||event.target!==event.subscriptionId||typeof event.planId!=='string'||!event.planId||!Number.isSafeInteger(event.planVersion)||(event.planVersion||0)<1:event.target!==account||event.subscriptionId!==undefined||event.planId!==undefined||event.planVersion!==undefined)invalid();
    if(event.action==='account.credential.reset'&&event.evidenceState!=='complete'&&event.evidenceState!=='unavailable')invalid();
    previous=event.id;
   }
   if(page.nextBefore!==undefined&&(!exactID(page.nextBefore)||page.events.length!==50||page.nextBefore!==previous))invalid();
   state={...state,events:before?[...state.events,...page.events]:[...page.events],next:page.nextBefore,loaded:true};
  }catch(error){if(request!==generation)return;state={...state,error:error instanceof Error?error.message:'Unable to load account audit.'};}
  finally{if(request===generation){state={...state,busy:false};emit();}}
 }
 return {
  open(accountId:string,label:string):void{generation++;state={accountId,label,filters:initialFilters(),events:[],busy:false,loaded:false,error:''};void load();},
  close():void{generation++;state={accountId:'',label:'',filters:initialFilters(),events:[],busy:false,loaded:false,error:''};emit();},
  apply(filters:AuditFilters):void{if(state.busy&&JSON.stringify(filters)===JSON.stringify(state.filters))return;generation++;state={...state,filters:Object.freeze({...filters}),events:[],next:undefined,busy:false,loaded:false,error:''};void load();},
  refresh():void{if(state.busy)return;state={...state,events:[],next:undefined,loaded:false};void load();},
  more():void{if(state.next)void load(state.next);},
  retry():void{void load(state.loaded?state.next:undefined);},
 };
}

const actionLabel:Record<AuditEvent['action'],string>={'account.create':'Account created','account.name.edit':'Display name changed','account.active':'Activated','account.suspended':'Suspended','account.status.batch':'Reviewed status change','account.credential.reset':'Password reset','subscription.assign':'Subscription assigned','speed.request':'Speed requested'};
function auditDetails(event:AuditEvent):string{
 if(event.evidenceState==='unavailable')return '<p>Historical evidence unavailable for this format.</p>';
 const escapedControls=/[\u0000-\u001f]/.test(event.reason),originalEvidence=escapedControls?JSON.stringify(event.reason):event.reason;
 let reason=event.reason,change='Historical before-state: Not recorded.';
 if(event.evidenceState==='complete'){
  try{
   const value:unknown=JSON.parse(event.reason);
   if(value&&typeof value==='object'&&!Array.isArray(value)){
    const v=value as Record<string,unknown>;
    if(typeof v.reason==='string'){
     if(event.action==='account.name.edit'&&typeof v.before==='string'&&typeof v.after==='string'){reason=v.reason;change=`Before: <bdi>${esc(v.before)}</bdi><br>After: <bdi>${esc(v.after)}</bdi>`;}
     if(event.action==='account.status.batch'&&['active','suspended'].includes(String(v.before))&&['active','suspended'].includes(String(v.after))&&typeof v.batchId==='string'){reason=v.reason;change=`Before: ${esc(v.before)} · After: ${esc(v.after)}<br>Batch ID: ${esc(v.batchId)}`;}
     if(event.action==='account.credential.reset'&&typeof v.beforeRevision==='string'&&typeof v.afterRevision==='string'&&/^\d{1,19}$/.test(v.beforeRevision)&&/^\d{1,19}$/.test(v.afterRevision)){reason=v.reason;change=`Credential revision: ${esc(v.beforeRevision)} → ${esc(v.afterRevision)}. Secret values are not recorded.`;}
    }
   }
  }catch{/* Historical unstructured reasons remain labelled original evidence. */}
 }
 if(event.action==='account.active'||event.action==='account.suspended')change+=' After: '+(event.action==='account.active'?'active.':'suspended.');
 if(event.subscriptionId)change+=`<br>Subscription: <bdi>${esc(event.subscriptionId)}</bdi><br>Plan: <bdi>${esc(event.planId||'')}</bdi> · v${esc(event.planVersion||'')}`;
 if(event.action==='speed.request')change+='<br>This records a requested choice. Live application is not confirmed by this event.';
 return `<p class="text-break"><bdi>${esc(reason)}</bdi></p><details><summary>Evidence & change details</summary><p class="text-break">${change}</p><p class="text-break">Target: <bdi>${esc(event.target)}</bdi></p><p>${event.evidenceState==='truncated'?'Original evidence truncated to 8,192 characters.':escapedControls?'Original audit evidence (JSON-escaped control characters):':'Original audit evidence:'}</p><p class="text-break"><bdi>${esc(originalEvidence)}</bdi></p></details>`;
}
function auditSummary(state:AuditState):string{
 const value=(n:number):number|string=>state.loaded?n:'—';
 return `<p class="text-break" tabindex="-1" data-audit-heading>Subscriber: <bdi>${esc(state.label)}</bdi></p><div class="subscriber-grid mb-2">${[['Events loaded',value(state.events.length)],['Actors loaded',value(new Set(state.events.map(e=>e.actor)).size)]].map(([label,count])=>`<div class="wl-summary-card"><div class="wl-summary-title">${esc(label)}</div><div class="fa-stat-val">${esc(count)}</div></div>`).join('')}</div><details><summary>Scope & timestamps</summary><p>Administrative account and subscription changes. These events do not confirm router application. Historical before-values are shown only when recorded. Counts apply to loaded events. Filters use UTC; event dates follow the display timezone.</p></details>`;
}
function auditList(state:AuditState):string{
 const message=state.busy?'Loading account audit…':state.error|| (state.loaded?(state.events.length?`${state.events.length} audit event${state.events.length===1?'':'s'} loaded.`:'No audit events match these filters.'):'Choose Audit on a subscriber.');
 const empty=state.busy?'Loading account audit…':state.error?'Audit data unavailable. Retry this page.':'No audit events match these filters.';
 return `<p role="status" aria-live="polite">${esc(message)}</p><p class="text-secondary">Applied filter: ${esc(auditCategories[state.filters.category])} · ${esc(state.filters.from||'Any start')} to ${esc(state.filters.until||'Any end')} (UTC).</p><button class="sbtn sbtn-outline" type="button" data-audit="refresh" ${state.busy?'disabled':''}>Refresh audit</button>`+(state.error?` <button class="sbtn sbtn-outline" type="button" data-audit="retry" ${state.busy?'disabled':''}>Retry page</button>`:'')+
 `<div class="data-scroll d-none d-md-block"><table class="table table-vcenter"><thead><tr><th>Date</th><th>Actor</th><th>Change</th><th>Reason & evidence</th><th>Audit ID</th></tr></thead><tbody>${state.events.map(e=>`<tr data-audit-event="${esc(e.id)}"><td class="text-nowrap align-top">${esc(fmtTs(e.createdAt*1000,true))}</td><td class="text-break align-top"><bdi>${esc(e.actor||'Not recorded')}</bdi></td><td class="align-top">${pill(actionLabel[e.action],e.action==='account.suspended'?'warn':e.action==='account.credential.reset'?'alt':'rx')}</td><td class="align-top">${auditDetails(e)}</td><td class="text-nowrap align-top">${esc(e.id)}</td></tr>`).join('')||`<tr><td colspan="5" class="tbl-empty">${esc(empty)}</td></tr>`}</tbody></table></div>`+
 `<div class="d-md-none">${state.events.map(e=>`<article class="subscriber-section" data-audit-card="${esc(e.id)}" aria-label="${esc(actionLabel[e.action])} · ${esc(e.id)}"><p>${pill(actionLabel[e.action],e.action==='account.suspended'?'warn':e.action==='account.credential.reset'?'alt':'rx')}</p><p class="text-break">${esc(fmtTs(e.createdAt*1000,true))} · <bdi>${esc(e.actor||'Not recorded')}</bdi><br>Audit ID: ${esc(e.id)}</p>${auditDetails(e)}</article>`).join('')||`<p>${esc(empty)}</p>`}</div>`+
 (state.next?`<button class="sbtn sbtn-outline" type="button" data-audit="more" ${state.busy?'disabled':''}>Load older audit events</button>`:'');
}
export function accountAuditMarkup(state:AuditState):string{return auditSummary(state)+auditList(state);}

export function initAccountAudit(api:API){
 if(!el('subscriberAccountTools'))return {open(_account:SubscriberAdminAccount):void{}};
 const content=document.createElement('div');content.className='subscriber-admin';
 content.innerHTML=`<div data-audit-summary></div><details data-audit-filters><summary>Audit filters</summary><div class="ff-grid">${fieldHtml({id:'subscriberAuditCategory',label:'Change category',help:'Filter committed administrative events.'},`<select id="subscriberAuditCategory" class="sform-input">${Object.entries(auditCategories).map(([key,label])=>`<option value="${key}">${label}</option>`).join('')}</select>`)}${fieldHtml({id:'subscriberAuditFrom',label:'From (UTC)',help:'Inclusive UTC start date.'},'<input id="subscriberAuditFrom" type="date" class="sform-input">')}${fieldHtml({id:'subscriberAuditUntil',label:'Until (UTC)',help:'Inclusive UTC end date.'},'<input id="subscriberAuditUntil" type="date" class="sform-input">')}</div><button class="sbtn sbtn-outline" type="button" data-audit-apply>Apply filters</button></details><div data-audit-results></div>`;
 const modal=managedModal('subscriberAuditModal','Account Audit',content,true,false);
 const category=el<HTMLSelectElement>('subscriberAuditCategory')!,from=el<HTMLInputElement>('subscriberAuditFrom')!,until=el<HTMLInputElement>('subscriberAuditUntil')!;
 const results=content.querySelector<HTMLElement>('[data-audit-results]')!,summary=content.querySelector<HTMLElement>('[data-audit-summary]')!,filterDetails=content.querySelector<HTMLDetailsElement>('[data-audit-filters]')!;
 let pendingFocus:string|undefined;
 const controller=createAccountAudit(api,state=>{
  const active=document.activeElement,scopeOpen=summary.querySelector('details')?.open||false,scopeFocus=active===summary.querySelector('summary');
  const retain=state.loaded&&results.querySelector('[data-audit-event]')?.getAttribute('data-audit-event')===state.events[0]?.id;
  const oldScroll=results.querySelector('.data-scroll'),scroll=oldScroll?{top:oldScroll.scrollTop,left:oldScroll.scrollLeft}:null;
  const body=content.closest<HTMLElement>('.rtr-modal-body'),bodyTop=body?.scrollTop||0;
  if(active instanceof HTMLElement&&results.contains(active)&&active.dataset.audit)pendingFocus=active.dataset.audit;
  summary.innerHTML=auditSummary(state);results.innerHTML=auditList(state);results.setAttribute('aria-busy',String(state.busy));
  const scope=summary.querySelector('details');if(scope)scope.open=scopeOpen;
  if(retain){const target=results.querySelector('.data-scroll');if(target&&scroll){target.scrollTop=scroll.top;target.scrollLeft=scroll.left;}if(body)body.scrollTop=bodyTop;}
  if(scopeFocus&&modal.isOpen())summary.querySelector('summary')?.focus();
  if(pendingFocus&&!state.busy&&modal.isOpen()&&(document.activeElement===document.body||results.contains(document.activeElement))){
   (results.querySelector<HTMLElement>(`[data-audit="${pendingFocus}"]`)||summary.querySelector<HTMLElement>('[data-audit-heading]'))?.focus();pendingFocus=undefined;
  }
 });
 for(const field of [from,until])field.addEventListener('input',()=>until.setCustomValidity(''));
 content.querySelector('[data-audit-apply]')!.addEventListener('click',()=>{
  until.setCustomValidity(from.value&&until.value&&from.value>until.value?'End date must be on or after the start date.':'');
  if(!from.reportValidity()||!until.reportValidity())return;
  filterDetails.open=false;filterDetails.querySelector('summary')?.focus();
  pendingFocus=undefined;controller.apply({category:category.value as AuditFilters['category'],from:from.value,until:until.value});
 });
 content.addEventListener('click',event=>{const target=event.target;if(!(target instanceof HTMLElement))return;const action=target.closest<HTMLButtonElement>('[data-audit]')?.dataset.audit;if(action==='refresh'||action==='retry'||action==='more'){pendingFocus=action;controller[action]();}});
 modal.onClose(()=>{pendingFocus=undefined;controller.close();});
 return {open(account:SubscriberAdminAccount):void{category.value='all';from.value='';until.value='';until.setCustomValidity('');pendingFocus=undefined;filterDetails.open=false;modal.open();filterDetails.querySelector('summary')?.focus();controller.open(account.id,account.username+' · '+account.name);}};
}
