import { initDashboard } from '../../src/pages/dashboard';
import { initDashboardGrid } from '../../src/pages/dashboard-grid';
import { initActivityPage } from '../../src/pages/activity';
import { applyCaps } from '../../src/caps';

const handlers: Record<string, Function[]> = {};
const socket = { on: (event: string, fn: Function) => { (handlers[event] ??= []).push(fn); }, emit: () => {} };
const emit = (event: string, value?: unknown): void => { handlers[event]?.forEach(fn => fn(value)); };
const activity = location.pathname === '/activity';
for (const key of ['dashboard', 'activity']) document.getElementById('page-' + key)!.classList.toggle('active', key === (activity ? 'activity' : 'dashboard'));
applyCaps({ pages: { dashboard: true, activity: true } });
if (activity) initActivityPage(socket as any, page => page === 'activity', () => [{ id: 'r1', label: 'Synthetic Router A' }, { id: 'r2', label: 'Synthetic Router B' }], () => 'r1');
else { initDashboard(socket as any); initDashboardGrid(); emit('router:active', { activeId: 'r1' }); }
window.addEventListener('fixture-socket', event => { const d = (event as CustomEvent).detail; emit(d.event, d.data); });
window.addEventListener('fixture-page', event => { const show = (event as CustomEvent).detail === 'dashboard'; document.getElementById('page-dashboard')!.classList.toggle('active', show); document.dispatchEvent(new CustomEvent('mikrodash:pagechange', { detail: show ? 'dashboard' : 'logs' })); });
window.addEventListener('fixture-caps', event => applyCaps({ pages: { dashboard: true, activity: (event as CustomEvent).detail } }));
