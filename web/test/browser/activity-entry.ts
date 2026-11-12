import { initActivityPage } from '../../src/pages/activity';
document.getElementById('page-activity')!.classList.add('active');
document.getElementById('page-activity')!.style.display='block';
const handlers:Record<string,Function[]>={};
const socket={on:(event:string,fn:Function)=>{(handlers[event]??=[]).push(fn)}};
initActivityPage(socket as any,()=>true,()=>[{id:'r1',label:'Synthetic Router'},{id:'r2',label:'Denied synthetic router'}],()=> 'r1');
window.addEventListener('fixture-invalidate',()=>handlers['activity:changed']?.forEach(fn=>fn({routerId:'r1',at:Date.now()})));

window.addEventListener('fixture-status',()=>handlers['activity:status']?.forEach(fn=>fn({routerId:'r1',at:Date.now()})));
