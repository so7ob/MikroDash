import { esc } from '../dom';
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

export function historyMarkup(s:HistoryState):string {
  const message=s.busy?'Loading status history…':s.error?s.error:s.loaded?(s.events.length?`${s.events.length} events loaded.`:'No status changes recorded.'):'Choose Status history on a subscriber.';
  return `<h4 tabindex="-1" data-history-heading>Status history${s.accountId?' — '+esc(s.label):''}</h4><p>Administrative changes only. These records do not confirm router application or disconnection.</p><p role="status" aria-live="polite">${esc(message)}</p>`+
    (s.accountId?`<button type="button" class="sbtn sbtn-outline" data-history="refresh" ${s.busy?'disabled':''}>Refresh history</button>`:'')+
    (s.error?` <button type="button" class="sbtn sbtn-outline" data-history="retry" ${s.busy?'disabled':''}>Retry page</button>`:'')+
    `<ol>${s.events.map(e=>`<li><p><strong>${esc(e.action==='account.active'?'Activated':e.action==='account.suspended'?'Suspended':'Reviewed status change')}</strong> · ${esc(fmtTs(e.createdAt*1000,true))}</p><p>Actor: ${esc(e.actor)} · Audit ID: ${esc(e.id)}</p><p class="text-break">${esc(e.reason)}</p></li>`).join('')}</ol>`+
    (s.next?`<button type="button" class="sbtn sbtn-outline" data-history="more" ${s.busy?'disabled':''}>Load older changes</button>`:'');
}

export function initStatusHistory(root:HTMLElement|null,api:API) {
  let pendingFocus:string|undefined, lastAccount='';
  const controller=createStatusHistory(api,state=>{
    if(lastAccount!==state.accountId){pendingFocus=undefined;lastAccount=state.accountId;}
    if(!root)return;
    const active=document.activeElement;
    const focus=active instanceof HTMLElement&&root.contains(active)?active.dataset.history:undefined;
    if(focus)pendingFocus=focus;
    root.innerHTML=historyMarkup(state);
    root.setAttribute('aria-busy',String(state.busy));
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
      if(target.dataset.history==='more')controller.more();
      if(target.dataset.history==='retry')controller.retry();
      if(target.dataset.history==='refresh')controller.refresh();
    });
  }
  return controller;
}
