import { el } from '../dom';
import { fieldHtml, toggleHtml } from '../form';
import { managedModal } from '../modals';
import type { SubscriberAdminAccount } from './subscriber-account-actions';

type API = <T>(path:string,body?:unknown)=>Promise<T>;
export type CredentialReview = Readonly<{accountId:string;username:string;name:string;revision:string}>;

// A missing or numeric revision is not a zero-epoch proof. Keep the reviewed
// identity detached and never coerce exact server revisions through Number.
export function credentialReview(account:SubscriberAdminAccount):CredentialReview {
 const revision=account.credentialRevision;
 if(!account.id||typeof revision!=='string'||!/^\d{1,19}$/.test(revision)||BigInt(revision)>9223372036854775807n)throw new Error('Refresh the subscriber list before reviewing credentials.');
 return Object.freeze({accountId:account.id,username:account.username,name:account.name,revision:BigInt(revision).toString()});
}

export function initSubscriberCredentials(api:API,committed:(account:SubscriberAdminAccount)=>void,say:(message:string)=>void){
 let busy=false,refreshRequired=false,blockedReview=false,review:CredentialReview|null=null;
 if(!el('subscriberAccountTools'))return {open(_account:SubscriberAdminAccount):void{},refreshed():void{}};
 const form=document.createElement('form');form.id='subscriberCredentialForm';
 form.innerHTML=`<p class="text-break">Subscriber: <bdi id="subscriberCredentialAccount"></bdi></p><p>Reviewed credential revision: <span id="subscriberCredentialRevision"></span></p><p>The subscriber must use their username and the new password. Existing passwords, voucher codes and portal sessions will stop working. Subscription balances and recorded usage stay intact. Existing network connections can continue until normal enforcement.</p><div class="ff-grid">${fieldHtml({id:'subscriberCredentialPassword',label:'New password',help:'8 to 128 UTF-8 bytes. This password is never read back.'},'<input id="subscriberCredentialPassword" class="sform-input" type="password" autocomplete="new-password" required>')}${fieldHtml({id:'subscriberCredentialRepeat',label:'Confirm new password',help:'Enter the same password again.'},'<input id="subscriberCredentialRepeat" class="sform-input" type="password" autocomplete="new-password" required>')}${fieldHtml({id:'subscriberCredentialReason',label:'Reset reason',help:'Required for the audit trail. Do not include the password.'},'<input id="subscriberCredentialReason" class="sform-input" required>')}</div><div class="ff-grid ff-one">${fieldHtml({id:'subscriberCredentialConfirm',label:'I reviewed this account and approve revoking its old credentials and portal sessions.'},toggleHtml('subscriberCredentialConfirm',false,'required'))}</div><button id="subscriberCredentialSave" type="submit" class="sbtn sbtn-warn">Reset password</button>`;
 const modal=managedModal('subscriberCredentialModal','Reset Subscriber Password',form);
 const password=el<HTMLInputElement>('subscriberCredentialPassword')!,repeat=el<HTMLInputElement>('subscriberCredentialRepeat')!,reason=el<HTMLInputElement>('subscriberCredentialReason')!,confirm=el<HTMLInputElement>('subscriberCredentialConfirm')!,save=el<HTMLButtonElement>('subscriberCredentialSave')!;
 function controls():void{form.querySelectorAll<HTMLInputElement|HTMLButtonElement>('input,button').forEach(n=>{n.disabled=busy;});save.disabled=busy||blockedReview||!review;save.textContent=busy?'Resetting…':'Reset password';modal.busy(busy);}
 for(const input of [password,repeat,reason])input.addEventListener('input',()=>{for(const field of [password,repeat,reason])field.setCustomValidity('');});
 modal.onClose(()=>{form.reset();password.setCustomValidity('');repeat.setCustomValidity('');reason.setCustomValidity('');review=null;blockedReview=false;controls();});
 el<HTMLFormElement>('subscriberCredentialForm')!.addEventListener('submit',async event=>{
  event.preventDefault();if(busy||blockedReview||!review)return;
  const bytes=new TextEncoder().encode(password.value).length;
  password.setCustomValidity(bytes<8||bytes>128||/[\x00\r\n]/.test(password.value)?'Use 8 to 128 UTF-8 bytes without NUL or line breaks.':'');
  repeat.setCustomValidity(password.value===repeat.value?'':'The passwords do not match.');
  reason.setCustomValidity(!reason.value.trim()||new TextEncoder().encode(reason.value).length>2048?'Enter a reason of at most 2048 UTF-8 bytes.':password.value&&reason.value.includes(password.value)?'Do not include the new password in the audit reason.':'');
  if(!form.reportValidity()||!confirm.checked)return;
  const intent=review;busy=true;controls();modal.error('');
  try{
   const result=await api<{account:SubscriberAdminAccount}>('/api/subscribers',{action:'credential',accountId:intent.accountId,expectedCredentialRevision:intent.revision,password:password.value,reason:reason.value,confirmCredentialReset:true});
   // Only the reviewed account and a newer exact revision can acknowledge this
   // reset. An unexpected response retains the draft as an uncertain result.
   const updated=credentialReview(result.account);
   if(updated.accountId!==intent.accountId||BigInt(updated.revision)!==BigInt(intent.revision)+1n)throw new Error('Unexpected credential reset acknowledgement.');
   password.value='';repeat.value='';committed(result.account);busy=false;controls();modal.close(true);
   Array.from(document.querySelectorAll<HTMLButtonElement>('[data-sub-credential]')).find(n=>n.dataset.subCredential===intent.accountId)?.focus();
   say('Password reset. Old credentials and portal sessions are revoked. Subscription balances and usage are preserved.');
  }catch(error){
   const status=error!==null&&typeof error==='object'&&'status' in error?error.status:undefined;
   if(status!==400){refreshRequired=true;blockedReview=true;}
   modal.error((error instanceof Error?error.message:'Password reset was not confirmed.')+(blockedReview?' Close this dialog, refresh the subscriber list and review the current credential revision before retrying.':''));
  }finally{busy=false;controls();}
 });
 return {
  open(account:SubscriberAdminAccount):void{
   if(busy||modal.isOpen())return;
   if(refreshRequired){say('Refresh the subscriber list before reviewing another credential reset.');return;}
   try{review=credentialReview(account);}catch(error){say(error instanceof Error?error.message:'Credential review unavailable.');return;}
   form.reset();blockedReview=false;el('subscriberCredentialAccount')!.textContent=review.username+' · '+review.name;el('subscriberCredentialRevision')!.textContent=review.revision;controls();modal.open();
  },
  refreshed():void{refreshRequired=false;},
 };
}
