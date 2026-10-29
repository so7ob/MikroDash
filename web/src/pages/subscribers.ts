import { initStatusHistory } from './subscriber-status-history';
import { initSubscriberReconciliation } from './subscriber-reconciliation';
import { initSubscriberAccountActions, type SubscriberAdminAccount } from './subscriber-account-actions';
import { el, esc, pill, renderSortHeader, sortRows, type SortState } from '../dom';
import { fieldHtml } from '../form';
import type { SubscriberPlan, SubscriberSnapshot, SubscriberLimit } from '../gen/payloads';
import { accountingStatusMessage, snapshotCards } from '../subscriber-view';

type Account = SubscriberAdminAccount;
type Data = { accounts: Account[]; plans: SubscriberPlan[]; mode: string };
function field(id: string, label: string, input: string, help = ''): string { return fieldHtml({id,label,help},input); }
function input(id: string, type = 'text', value = ''): string { return `<input id="${id}" class="sform-input" type="${type}" value="${esc(value)}">`; }
function select(id: string, values: string[]): string { return `<select id="${id}" class="sform-input">${values.map(v=>`<option value="${esc(v)}">${esc(v)}</option>`).join('')}</select>`; }
function val(id: string): string { return el<HTMLInputElement>(id)?.value || ''; }

export function initSubscribersPage(): void {
  let data: Data = {accounts:[],plans:[],mode:'disabled'}, loaded=false;
  let editingName: Account | null = null, nameSaving = false;
  const sort: SortState = {col:'username',dir:'asc'};
  const limits: SubscriberLimit[]=[];
  const say=(text:string):void=>{const n=el('subscriberAdminNotice');if(n)n.textContent=text;};
  async function api<T>(path:string,body?:unknown):Promise<T>{const r=await fetch(path,{method:body===undefined?'GET':'POST',headers:{'Content-Type':'application/json','X-MikroDash-Request':'subscriber-admin'},body:body===undefined?undefined:JSON.stringify(body)});const v=await r.json();if(!r.ok)throw new Error(v.error||'Request failed');return v;}
  function tab(name:string):void{for(const key of ['accounts','plans','inspect','history','reconciliation']){const n=el('subAdmin-'+key);if(n)n.hidden=name!==key;}document.querySelectorAll<HTMLButtonElement>('[data-subtab]').forEach(b=>{b.classList.toggle('active',b.dataset.subtab===name);b.setAttribute('aria-selected',String(b.dataset.subtab===name));b.tabIndex=b.dataset.subtab===name?0:-1;});}
  const statusHistory=initStatusHistory(el('subscriberStatusHistory'),api);
  const reconcileAccounts=initSubscriberReconciliation(api);
  const accountActions=initSubscriberAccountActions(api,rows,changed=>{data.accounts=data.accounts.map(account=>changed.find(current=>current.id===account.id)||account);rows();},say);
  function runtimeStatus():void {
    const status=el('subscriberRuntimeStatus');if(!status)return;
    const explanations:Record<string,string>={
      disabled:'You can prepare subscribers, vouchers and plans here. The subscriber portal and RADIUS are disabled. An approved -subscriber-config file and a separate HTTPS portal listener are required before a C pilot. Preparing accounts here does not change router settings.',
      shadow:'Shadow mode blocks new Internet connections through MikroDash. Portal sign-in and policy inspection do not prove a HotSpot connection. Complete the C pilot checks before enabling admission.',
      pilot:'Pilot admission is limited to the configured account and network. Portal sign-in is separate from a confirmed HotSpot connection. Target-router acceptance is still required.',
      managed:'Network admission is configured in Managed mode. Inspect the subscriber session and enforcement results to verify connectivity and applied limits.',
    };
    const labels:Record<string,string>={disabled:'Disabled',shadow:'Shadow',pilot:'Pilot',managed:'Managed'};
    status.innerHTML=`<p class="mb-1">Network runtime: ${pill(labels[data.mode]||'Unknown',data.mode==='disabled'?'warn':'rx')}</p><p class="text-secondary mb-0">${esc(explanations[data.mode]||'Runtime state is unavailable. Check the server startup log before connecting a subscriber.')}</p>`;
  }
  function rows():void {
    runtimeStatus();
    reconcileAccounts(data.accounts);
    renderSortHeader('subscriberAccountHead',[{key:'',label:'Select'},{key:'username',label:'Username'},{key:'name',label:'Name'},{key:'status',label:'Status'},{key:'',label:'Actions'}],sort,rows);
    const visibleAccounts=accountActions.filtered(data.accounts);
    const body=el('subscriberAccountRows');if(body)body.innerHTML=sortRows(visibleAccounts,sort.col,sort.dir).map(a=>`<tr>${accountActions.selectionCell(a)}<td>${esc(a.username)}</td><td>${esc(a.name)}</td><td>${pill(a.status,a.status==='active'?'ok':'warn')}</td><td><button class="sbtn sbtn-outline" data-sub-edit="${esc(a.id)}">Edit name</button> <button class="sbtn sbtn-outline" data-sub-inspect="${esc(a.id)}">Inspect</button> <button class="sbtn sbtn-outline" data-sub-history="${esc(a.id)}">Status history</button> <button class="sbtn sbtn-outline" data-sub-assign="${esc(a.id)}">Assign plan</button> <button class="sbtn sbtn-warn" data-sub-status="${esc(a.id)}" data-status="${a.status==='active'?'suspended':'active'}">${a.status==='active'?'Suspend':'Reactivate'}</button></td></tr>`).join('')||'<tr><td class="tbl-empty" colspan="5">No subscribers match the current filters.</td></tr>';
    const badge=el('subscribersBadge');if(badge)badge.textContent=String(data.accounts.length);
    const list=el('subscriberPlanList');if(list)list.innerHTML=data.plans.map(p=>`<p>${esc(p.name)} ${pill('v'+p.version,'alt')} · ${p.policies.reduce((n,po)=>n+po.limits.length,0)} limits · ${p.maxSessions} sessions</p>`).join('');
  }
  async function refresh():Promise<void>{try{data=await api<Data>('/api/subscribers');accountActions.refreshed();rows();say('Mode: '+data.mode);loaded=true;}catch(e){say(e instanceof Error?e.message:'Unable to load subscribers');}}
  function openNameEditor(account: Account): void {
    if(nameSaving)return;
    editingName={...account};
    const form=el('subscriberNameForm'), label=el('subscriberNameAccount');
    if(form)form.hidden=false;
    const create=el('subscriberCreateForm');if(create)create.hidden=true;
    if(label)label.textContent=account.username;
    const name=el<HTMLInputElement>('subEditName'), reason=el<HTMLInputElement>('subEditReason');
    if(name){name.value=account.name;name.focus();}
    if(reason)reason.value='';
  }
  function nameBusy(busy: boolean): void {
    nameSaving=busy;
    for(const id of ['subEditName','subEditReason','subscriberNameSave','subscriberNameCancel']){const n=el<HTMLInputElement|HTMLButtonElement>(id);if(n)n.disabled=busy;}
    document.querySelectorAll<HTMLButtonElement>('[data-sub-edit]').forEach(b=>{b.disabled=busy;});
    const save=el('subscriberNameSave');if(save)save.textContent=busy?'Saving…':'Save display name';
  }
  function closeNameEditor(): void {
    const id=editingName?.id;editingName=null;
    const form=el('subscriberNameForm');if(form)form.hidden=true;
    const create=el('subscriberCreateForm');if(create)create.hidden=false;
    Array.from(document.querySelectorAll<HTMLButtonElement>('[data-sub-edit]')).find(b=>b.dataset.subEdit===id)?.focus();
  }
  const account=el('subscriberAccountForm');if(account)account.innerHTML=`<form id="subscriberCreateForm"><div class="ff-grid">${field('subUsername','Username',input('subUsername'))}${field('subName','Display name',input('subName'),'Names are preserved exactly, including Unicode.')}${field('subCredential','Credential type',select('subCredential',['Password','Voucher']))}${field('subPassword','Password',`<input id="subPassword" class="sform-input" type="password">`,'For password accounts only. At least 8 characters.')}${field('subVoucherLength','Voucher length',input('subVoucherLength','number','24'),'16 to 64 hexadecimal characters. Shown only once.')}${field('subReason','Change reason',input('subReason'),'Required for the audit trail.')}</div><button class="sbtn sbtn-primary" type="submit">Create subscriber</button></form>`;
  if(account)account.innerHTML+=`<form id="subscriberNameForm" hidden><h4>Edit display name</h4><p>Subscriber: <span id="subscriberNameAccount"></span></p><div class="ff-grid">${field('subEditName','New display name',`<input id="subEditName" class="sform-input">`,'Unicode is preserved exactly.')}${field('subEditReason','Change reason',`<input id="subEditReason" class="sform-input" required>`, 'Required for the audit trail.')}</div><button id="subscriberNameSave" class="sbtn sbtn-primary" type="submit">Save display name</button> <button id="subscriberNameCancel" class="sbtn sbtn-outline" type="button">Cancel</button></form>`;
  el('subscriberNameCancel')?.addEventListener('click',()=>{if(!nameSaving)closeNameEditor();});
  el('subscriberNameForm')?.addEventListener('submit',async e=>{
    e.preventDefault();if(!editingName||nameSaving)return;
    const selected={...editingName};nameBusy(true);
    try{
      const result=await api<{account:Account}>('/api/subscribers',{action:'name',accountId:selected.id,expectedName:selected.name,name:val('subEditName'),reason:val('subEditReason')});
      data.accounts=data.accounts.map(a=>a.id===result.account.id?result.account:a);rows();
      nameBusy(false);closeNameEditor();say('Display name updated.');
    }catch(error){say(error instanceof Error?error.message:'Display name update failed');}
    finally{nameBusy(false);}
  });
  const plans=el('subscriberPlanForm');if(plans)plans.innerHTML=`<form id="subscriberSavePlan"><div class="ff-grid">${field('subPlanID','Plan ID',input('subPlanID'))}${field('subPlanName','Plan name',input('subPlanName'))}${field('subPlanVersion','Version',input('subPlanVersion','number','1'),'Existing versions cannot be overwritten.')}${field('subValidity','Validity in seconds',input('subValidity','number','2592000'))}${field('subStart','Validity starts',select('subStart',['first-login','assigned']))}${field('subNetworks','Allowed networks',input('subNetworks','text','C'),'Comma-separated enrollment keys.')}${field('subMaxSessions','Concurrent sessions',input('subMaxSessions','number','1'))}${field('subDown','Download ceiling (bit/s)',input('subDown','text','8000000'))}${field('subUp','Upload ceiling (bit/s)',input('subUp','text','2000000'))}${field('subPlanReason','Change reason',input('subPlanReason'))}</div><h4>Concurrent limits</h4><div class="ff-grid">${field('subLimitKind','Limit type',select('subLimitKind',['seconds','download','upload','total']))}${field('subLimitAmount','Limit amount',input('subLimitAmount','text','3600'),'Integer seconds or bytes; decimal only.')}${field('subLimitPeriod','Renewal period',select('subLimitPeriod',['none','hourly','daily','weekly','monthly']))}${field('subLimitAction','When exhausted',select('subLimitAction',['disconnect','deny','alert']))}${field('subLimitAnchor','Period anchor',input('subLimitAnchor','text','2026-01-01T00:00:00+03:00'),'RFC3339 timestamp. Hourly reset at minute 25 uses 00:25:00.')}</div><button id="subscriberAddLimit" type="button" class="sbtn sbtn-outline">Add limit</button><div id="subscriberDraftLimits"></div><button class="sbtn sbtn-primary" type="submit">Save immutable plan version</button></form>`;
  el('subscriberAddLimit')?.addEventListener('click',()=>{const l:SubscriberLimit={id:'limit-'+(limits.length+1),kind:val('subLimitKind'),state:'limited',amount:val('subLimitAmount'),period:{kind:val('subLimitPeriod'),zone:'Asia/Aden',anchor:val('subLimitAnchor')},action:val('subLimitAction'),down:'0',up:'0'};if(!/^\d+$/.test(l.amount)){say('Enter a non-negative decimal amount.');return;}limits.push(l);const n=el('subscriberDraftLimits');if(n)n.textContent=limits.map(l=>`${l.id}: ${l.amount} ${l.kind}, ${l.period.kind}, ${l.action}`).join(' | ');});
  el('subscriberCreateForm')?.addEventListener('submit',async e=>{e.preventDefault();try{const result=await api<{voucher:string}>('/api/subscribers',{action:'create',username:val('subUsername'),name:val('subName'),password:val('subPassword'),voucherLength:val('subCredential')==='Voucher'?Number(val('subVoucherLength')):0,reason:val('subReason')});const password=el<HTMLInputElement>('subPassword');if(password)password.value='';await refresh();say(result.voucher?'Voucher created. Copy it now: '+result.voucher:'Subscriber created.');}catch(e){say(e instanceof Error?e.message:'Create failed');}});
  el('subscriberSavePlan')?.addEventListener('submit',async e=>{e.preventDefault();try{const down=val('subDown'),up=val('subUp');const plan:SubscriberPlan={id:val('subPlanID'),name:val('subPlanName'),version:Number(val('subPlanVersion')),validSeconds:val('subValidity'),start:val('subStart'),maxSessions:Number(val('subMaxSessions')),networks:val('subNetworks').split(',').map(v=>v.trim()).filter(Boolean),down,up,policies:[{id:'standard',limits:[...limits],windows:[]}],speeds:[{id:'economy',name:'Economy',down:(BigInt(down)/2n).toString(),up:(BigInt(up)/2n).toString()},{id:'fast',name:'Fast',down,up}]};await api('/api/subscribers',{action:'plan',plan,reason:val('subPlanReason')});await refresh();say('Plan version saved. Existing subscriptions keep their assigned version.');}catch(e){say(e instanceof Error?e.message:'Plan save failed');}});
  el('subscriberAccountRows')?.addEventListener('click',async e=>{const b=e.target;if(!(b instanceof HTMLButtonElement))return;try{if(b.dataset.subHistory){const selected=data.accounts.find(a=>a.id===b.dataset.subHistory);if(selected){tab('history');statusHistory.open(selected.id,selected.username);el('subTab-history')?.focus();}}else if(b.dataset.subEdit){const selected=data.accounts.find(a=>a.id===b.dataset.subEdit);if(selected)openNameEditor(selected);}else if(b.dataset.subInspect){const v=await api<SubscriberSnapshot>('/api/subscribers/snapshot?account='+encodeURIComponent(b.dataset.subInspect));const n=el('subscriberInspect');if(n)n.innerHTML=`<p>${pill(v.decision.allowed?'New connection allowed':'New connection denied',v.decision.allowed?'ok':'warn')} ${v.decision.reasons.map(esc).join(' · ')}</p>${v.pendingAccounting > 0 ? `<p role="status">${esc(accountingStatusMessage(v))}</p>` : ''}`+snapshotCards(v);tab('inspect');}else if(b.dataset.subAssign){const choices=data.plans.map(p=>`${p.id}@${p.version}`).join(', ');const choice=prompt('Plan version ('+choices+')');if(!choice)return;const p=data.plans.find(p=>`${p.id}@${p.version}`===choice);if(!p)throw new Error('Choose an existing plan version.');const reason=prompt('Reason for assigning or renewing this subscription');if(!reason)return;await api('/api/subscribers',{action:'assign',accountId:b.dataset.subAssign,planId:p.id,version:p.version,reason});say('Subscription assigned.');}else if(b.dataset.subStatus){const selected=data.accounts.find(a=>a.id===b.dataset.subStatus);if(selected)accountActions.reviewOne(selected,b.dataset.status||'');}}catch(e){say(e instanceof Error?e.message:'Operation failed');}});
  el('subscriberAccountRows')?.addEventListener('change',event=>{const input=event.target;if(!(input instanceof HTMLInputElement)||!input.dataset.subSelect)return;const account=data.accounts.find(a=>a.id===input.dataset.subSelect);if(account)accountActions.toggle(account,input.checked);});
  el('subscriberAdminTabs')?.addEventListener('click',e=>{if(e.target instanceof HTMLButtonElement&&e.target.dataset.subtab)tab(e.target.dataset.subtab);});
  el('subscriberAdminTabs')?.addEventListener('keydown',e=>{if(!(e instanceof KeyboardEvent)||!(e.target instanceof HTMLButtonElement))return;const tabs=Array.from(document.querySelectorAll<HTMLButtonElement>('[data-subtab]'));const at=tabs.indexOf(e.target);if(at<0)return;let next=at;if(e.key==='ArrowRight')next=(at+1)%tabs.length;else if(e.key==='ArrowLeft')next=(at+tabs.length-1)%tabs.length;else if(e.key==='Home')next=0;else if(e.key==='End')next=tabs.length-1;else return;e.preventDefault();const button=tabs[next];if(button?.dataset.subtab){tab(button.dataset.subtab);button.focus();}});
  el('subscribersRefresh')?.addEventListener('click',()=>void refresh());
  document.addEventListener('mikrodash:pagechange',e=>{if(e instanceof CustomEvent&&e.detail==='subscribers'&&!loaded)void refresh();});
}
