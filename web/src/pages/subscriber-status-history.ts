import { managedModal } from '../modals';
import { el, esc, pill } from '../dom';
import { fmtTs } from '../timefmt';

type Event = {id:string;actor:string;action:string;reason:string;createdAt:number};
type Page = {accountId:string;events:Event[];nextBefore?:string};
type API = <T>(path:string)=>Promise<T>;
export type HistoryState = {accountId:string;label:string;events:Event[];next?:string;busy:boolean;error:string;loaded:boolean};
const id = (v:unknown):v is string => typeof v==='string' && /^[1-9][0-9]*$/.test(v) && v.length<=19 && BigInt(v)<=9223372036854775807n;

export function createStatusHistory(api:API, changed:(state:HistoryState)=>void) {
  let generation=0;
  let state:HistoryState={accountId:'',label:'',events:[],busy:false,error:'',loaded:false};
  const emit=():void=>changed({...state,events:[...state.events]});
  async function load(before?:string):Promise<void> {
    if(state.busy||!state.accountId)return;
    const request=++generation, account=state.accountId;
    state={...state,busy:true,error:''};emit();
    try {
      const page=await api<Page>('/api/subscribers/status-history?account='+encodeURIComponent(account)+(before?'&before='+encodeURIComponent(before):''));
      if(request!==generation)return;
      // Fail closed on account mismatch or broken pagination; never append an
      // ambiguous page or coerce exact audit IDs through JavaScript numbers.
      if(!page || page.accountId!==account || !Array.isArray(page.events)||page.events.length>50)throw new Error('Unexpected history response. Retry this page.');
      let previous=before;
      for(const event of page.events){
        if(!event || !id(event.id) || previous!==undefined&&BigInt(event.id)>=BigInt(previous) || typeof event.actor!=='string'||typeof event.reason!=='string'||!['account.active','account.suspended','account.status.batch'].includes(event.action)||!Number.isSafeInteger(event.createdAt))throw new Error('Unexpected history response. Retry this page.');
        previous=event.id;
      }
      if(page.nextBefore!==undefined && (!id(page.nextBefore)||page.events.length!==50||page.nextBefore!==previous))throw new Error('Unexpected history cursor. Retry this page.');
      state={...state,events:before?[...state.events,...page.events]:page.events,next:page.nextBefore,loaded:true};
    } catch(e) {
      if(request!==generation)return;
      state={...state,error:e instanceof Error?e.message:'Unable to load status history.'};
    } finally {
      if(request===generation){state={...state,busy:false};emit();}
    }
  }
  return {
    open(accountId:string,label:string):void {generation++;state={accountId,label,events:[],busy:false,error:'',loaded:false};void load();},
    more():void {if(state.next)void load(state.next);},
    retry():void {void load(state.loaded?state.next:undefined);},
    refresh():void {if(state.busy)return;state={...state,events:[],next:undefined,loaded:false};void load();},
  };
}

function reasonMarkup(event:Event):string {
  if(event.action==='account.status.batch'){
    try {
      const value:unknown=JSON.parse(event.reason);
      if(value!==null && typeof value==='object' && !Array.isArray(value)){
        const {reason,before,after,batchId}=value as Record<string,unknown>;
        if(typeof reason==='string' && typeof before==='string' && typeof after==='string'
            && ['active','suspended'].includes(before) && ['active','suspended'].includes(after)
            && typeof batchId==='string' && batchId.length>0){
          return `<p class="text-break">Reason: <bdi>${esc(reason)}</bdi></p><p>Before: ${esc(before)} · After: ${esc(after)}</p><p class="text-break">Batch ID: ${esc(batchId)}</p><details><summary>Original audit record</summary><p class="text-break">${esc(event.reason)}</p></details>`;
        }
      }
    } catch { /* Legacy or incomplete evidence stays verbatim below. */ }
  }
  return `<p class="text-break">${esc(event.reason)}</p>`;
}

export function historyMarkup(s:HistoryState):string {
  const message=s.busy?'Loading status history…':s.error?s.error:s.loaded?(s.events.length?`${s.events.length} events loaded.`:'No status changes recorded.'):'Choose Status history on a subscriber.';
  return `<h4 tabindex="-1" data-history-heading>Status history${s.accountId?' - '+esc(s.label):''}</h4><p class="text-secondary">Administrative changes only. These records do not confirm router application or disconnection. Date and status filters apply to loaded events; load older changes to extend the range.</p><p role="status" aria-live="polite">${esc(message)}</p>`+
    `<div class="wl-summary-row wl-summary-row-4">${[['Changes loaded',s.events.length],['Administrators',new Set(s.events.map(e=>e.actor)).size],['Activations',s.events.filter(e=>e.action==='account.active').length],['Reviewed batches',s.events.filter(e=>e.action==='account.status.batch').length]].map(([label,count])=>`<div class="wl-summary-card"><div class="wl-summary-title">${label}</div><div class="fa-stat-val">${count}</div></div>`).join('')}</div>`+
    (s.accountId?`<button type="button" class="sbtn sbtn-outline" data-history="refresh" ${s.busy?'disabled':''}>Refresh history</button>`:'')+
    (s.error?` <button type="button" class="sbtn sbtn-outline" data-history="retry" ${s.busy?'disabled':''}>Retry page</button>`:'')+
    `<div class="data-scroll"><table class="table table-vcenter"><thead><tr><th>Date</th><th>Actor</th><th>Transition</th><th>Operation</th><th>Reason & evidence</th><th>Actions</th></tr></thead><tbody>${s.events.map(e=>`<tr data-history-date="${new Date(e.createdAt*1000).toISOString().slice(0,10)}" data-history-action="${esc(e.action)}"><td>${esc(fmtTs(e.createdAt*1000,true))}</td><td>${esc(e.actor)}</td><td>${pill(e.action==='account.active'?'Activated':e.action==='account.suspended'?'Suspended':'Reviewed status change',e.action==='account.active'?'ok':'warn')}</td><td>${esc(e.id)}</td><td>${reasonMarkup(e)}</td><td><button type="button" class="sbtn sbtn-outline" data-history-detail="${esc(e.id)}">View details</button></td></tr>`).join('')||'<tr><td colspan="6" class="tbl-empty">No status changes recorded.</td></tr>'}</tbody></table></div>`+
    (s.next?`<button type="button" class="sbtn sbtn-outline" data-history="more" ${s.busy?'disabled':''}>Load older changes</button>`:'');
}

export function initStatusHistory(root:HTMLElement|null,api:API) {
  let pendingFocus:string|undefined, lastAccount='';
  let events:Event[]=[];
  const content=root?document.createElement('div'):null;
  const detail=content?managedModal('subscriberHistoryModal','Status Change Details',content):null;
  const filters=root?document.createElement('div'):null;
  if(filters){filters.className='subscriber-toolbar';filters.innerHTML='<label class="sform-label" for="historyFrom">From</label><input id="historyFrom" type="date" class="sform-input sform-sm"><label class="sform-label" for="historyUntil">Until</label><input id="historyUntil" type="date" class="sform-input sform-sm"><label class="sform-label" for="historyStatus">Status</label><select id="historyStatus" class="sform-input sform-sm"><option value="">All changes</option><option value="account.active">Activated</option><option value="account.suspended">Suspended</option><option value="account.status.batch">Reviewed batch</option></select>';root?.before(filters);filters.addEventListener('change',filter);}
  function filter():void{const from=filters?.querySelector<HTMLInputElement>('#historyFrom')?.value||'',until=filters?.querySelector<HTMLInputElement>('#historyUntil')?.value||'',status=filters?.querySelector<HTMLSelectElement>('#historyStatus')?.value||'';root?.querySelectorAll<HTMLElement>('[data-history-date]').forEach(row=>{const date=row.dataset.historyDate||'';row.hidden=!!(from&&date<from||until&&date>until||status&&row.dataset.historyAction!==status);});}
  const controller=createStatusHistory(api,state=>{
    if(lastAccount!==state.accountId){pendingFocus=undefined;lastAccount=state.accountId;}
    if(!root)return;events=state.events;const badge=el('subscriberHistoryCount');if(badge)badge.textContent=state.loaded?String(state.events.length):'—';
    const active=document.activeElement;
    const focus=active instanceof HTMLElement&&root.contains(active)?active.dataset.history:undefined;
    if(focus)pendingFocus=focus;
    root.innerHTML=historyMarkup(state);
    filter();root.setAttribute('aria-busy',String(state.busy));
    if(pendingFocus&&!state.busy&&(document.activeElement===document.body||root.contains(document.activeElement))){
      (root.querySelector<HTMLButtonElement>(`[data-history="${pendingFocus}"]`)||root.querySelector<HTMLElement>('[data-history-heading]'))?.focus();
      pendingFocus=undefined;
    }
  });
  if(root){
    root.innerHTML=historyMarkup({accountId:'',label:'',events:[],busy:false,error:'',loaded:false});
    root.addEventListener('click',event=>{
      const target=event.target;
      if(!(target instanceof HTMLButtonElement)||target.disabled)return;
      if(target.dataset.historyDetail&&content){const event=events.find(e=>e.id===target.dataset.historyDetail);if(event){content.innerHTML=`<p>Actor: ${esc(event.actor)} · Audit ID: ${esc(event.id)}</p>${reasonMarkup(event)}`;detail?.open();}}
      if(target.dataset.history==='more')controller.more();
      if(target.dataset.history==='retry')controller.retry();
      if(target.dataset.history==='refresh')controller.refresh();
    });
  }
  return controller;
}
