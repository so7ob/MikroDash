// Browser fixture composes shipped navigation, URL and modal components. HTTP
// and SQLite authority are exercised by the Go fixture; NAS observations are synthetic.
import { initSubscribersPage } from '../../src/pages/subscribers';
import { initNav, navAutoExpand } from '../../src/nav';
import { wireModals } from '../../src/modals';
import { initialPage, initRouting, sync, type NavMode } from '../../src/routing';
const pages = new Set(['subscribers','subscriber-overview','subscriber-plans','subscriber-simulator','subscriber-history','subscriber-reconciliation','interfaces','vlans','bridges']);
let current = '';
function show(key: string, mode: NavMode = 'push'): void {
 current = key;
 document.querySelectorAll<HTMLElement>('.page-view').forEach(n => n.classList.toggle('active', n.id === 'page-' + key));
 document.querySelectorAll<HTMLElement>('.nav-item').forEach(n => {n.classList.toggle('active', n.dataset.page === key); if(n.dataset.page === key) navAutoExpand(n.dataset.cat);});
 sync(key,mode); document.dispatchEvent(new CustomEvent('mikrodash:pagechange',{detail:key}));
}
initNav();wireModals();initSubscribersPage();
document.querySelectorAll<HTMLElement>('.nav-item').forEach(n=>n.addEventListener('click',event=>{event.preventDefault();if(n.dataset.page&&pages.has(n.dataset.page)){show(n.dataset.page);document.getElementById('sidenav')?.classList.remove('mobile-open');document.getElementById('navOverlay')?.classList.remove('show');}}));
document.getElementById('burgerBtn')?.addEventListener('click',()=>{document.getElementById('sidenav')?.classList.toggle('mobile-open');document.getElementById('navOverlay')?.classList.toggle('show');});
document.getElementById('navOverlay')?.addEventListener('click',()=>{document.getElementById('sidenav')?.classList.remove('mobile-open');document.getElementById('navOverlay')?.classList.remove('show');});
initRouting(key=>show(key,'skip'),()=>current);show(initialPage(pages,'subscribers'),'replace');
