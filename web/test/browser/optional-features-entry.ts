// Synthetic transport; the page modules and markup are the shipped ones.
import { applyPalette } from '../../src/appearance';
import { applyPageVisibility } from '../../src/caps';
import { initDnsPage } from '../../src/pages/dns';
import { initWifiMapPage } from '../../src/pages/wifi-map';
import { initWirelessPage } from '../../src/pages/wireless';
import { initCapsmanPage } from '../../src/pages/capsman';
import { initDomainRouteCard } from '../../src/pages/dashboard-card-domainroute';
import { mountSettingsTabs, activateSettingsTab } from '../../src/pages/settings';
const handlers: Record<string, Function[]> = {};
const socket = {on: (e: string, fn: Function) => {(handlers[e] ??= []).push(fn);}, emit: () => {}};
const send = (e: string, d: unknown) => handlers[e]?.forEach(fn => fn(d));
const page = new URL(location.href).searchParams.get('page') || 'dns';
document.getElementById('page-'+page)?.classList.add('active');
if (page === 'dns') initDnsPage(socket as any, () => true);
if (page === 'wifi-clients') initWirelessPage(socket as any, () => true);
if (page === 'wifi-map') initWifiMapPage(socket as any, () => true);
if (page === 'capsman') initCapsmanPage(socket as any, () => true);
if (page === 'dashboard') initDomainRouteCard(socket as any);
if (page === 'settings') { mountSettingsTabs(); activateSettingsTab('features'); }
const features = ['dnsBulkEnabled','dnsCacheEnabled','dnsFleetSyncEnabled','dnsListsEnabled','dnsTraceEnabled',
'domainRoutingEnabled','watchListEnabled','wifiHistoryEnabled','wifiWeakSpotsEnabled','capVersionsEnabled'];
(globalThis as any).fixture = {send, theme(t: string) { applyPalette(t === 'nord' ? 'nord' : 'default', t === 'light' ? 'light' : 'dark'); }, enable(on: boolean) { applyPageVisibility(Object.fromEntries(features.map(k => [k, on]))); }};
send('router:active', {activeId:'synthetic'});
send('router:switched', {activeId:'synthetic'});
document.dispatchEvent(new CustomEvent('mikrodash:pagechange', {detail:page}));
