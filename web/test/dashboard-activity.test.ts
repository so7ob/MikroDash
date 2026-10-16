import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ACTIVITY_CARDS, activityCardURL, initActivityCards, renderActivityCards } from '../src/pages/dashboard-card-activity';
import { DEFAULT_LAYOUT, CARD_PAGES } from '../src/gen/grid-tables';

const pause = (ms = 15) => new Promise(resolve => setTimeout(resolve, ms));
const result = (routerId: string, dns = 3): any => ({ routerId, totals: { dns, domains: dns ? 2 : 0, users: dns ? 1 : 0, unmapped: dns ? 1 : 0 }, filter: { from: 1, to: 3600001 }, services: [], clients: [], categories: [], countries: [], dnsCountries: [], series: [], status: { enabled: false, error: '', lastSync: 0 }, geoAvailable: false });

test('Activity cards keep existing layouts and country drilldowns retain explicit Firewall evidence', () => {
  assert.equal(ACTIVITY_CARDS.length, 6);
  for (const kind of ACTIVITY_CARDS) {
    const card = DEFAULT_LAYOUT.find(c => c.id === 'dc-card-activity-' + kind);
    assert.ok(card); assert.equal(card.visible, false); assert.equal(CARD_PAGES[card.id], 'activity');
  }
  const url = new URL(activityCardURL('r&1', 'country', 'Unknown'), 'http://example.test');
  assert.equal(url.pathname, '/activity'); assert.equal(url.searchParams.get('routerId'), 'r&1');
  assert.equal(url.searchParams.get('kind'), 'firewall'); assert.equal(url.searchParams.get('country'), 'Unknown');
});

test('one demand-driven request feeds all cards and stale router responses cannot repaint them', async () => {
  const listeners: Record<string, Function[]> = {}, handlers: Record<string, Function[]> = {};
  const nodes = new Map<string, any>();
  const node = () => ({ style: { display: 'none' }, innerHTML: '', textContent: '', classList: { add() {}, remove() {}, toggle() {}, contains: () => true }, querySelector: () => null, addEventListener() {}, setAttribute() {}, removeAttribute() {} });
  for (const kind of ACTIVITY_CARDS) { nodes.set('dc-card-activity-' + kind, node()); nodes.set('dc-activity-' + kind, node()); }
  nodes.set('dash-grid-root', node()); const page = node(); nodes.set('page-dashboard', page);
  let active = true; page.classList.contains = () => active;
  const doc: any = { hidden: false, getElementById: (id: string) => nodes.get(id) || null, addEventListener: (event: string, fn: Function) => (listeners[event] ??= []).push(fn) };
  const saved = { document: globalThis.document, fetch: globalThis.fetch };
  const calls: { url: string; resolve: (response: any) => void }[] = [];
  globalThis.document = doc;
  globalThis.fetch = ((url: string) => new Promise(resolve => calls.push({ url, resolve }))) as any;
  const emit = (event: string, value?: any) => handlers[event]?.forEach(fn => fn(value));
  const fire = (event: string) => listeners[event]?.forEach(fn => fn({}));
  const answer = (index: number, routerId: string, dns = 3) => calls[index]!.resolve({ ok: true, json: async () => result(routerId, dns) });
  try {
    initActivityCards({ on: (event: string, fn: Function) => (handlers[event] ??= []).push(fn) } as any);
    emit('router:active', { activeId: 'r1' }); await pause(); assert.equal(calls.length, 0, 'hidden cards fetched data');
    for (const kind of ACTIVITY_CARDS) nodes.get('dc-card-activity-' + kind).style.display = '';
    fire('dashboard:layout'); await pause(); assert.equal(calls.length, 1, 'cards did not share one request');
    answer(0, 'r1'); await pause(); assert.match(nodes.get('dc-activity-overview').innerHTML, /DNSUSER events/);
    emit('activity:changed', { routerId: 'r2' }); await pause(520); assert.equal(calls.length, 1, 'another router invalidated the cards');
    emit('activity:changed', { routerId: 'r1' }); await pause(520); assert.equal(calls.length, 2);
    emit('router:active', { activeId: 'r2' }); await pause(); assert.equal(calls.length, 3);
    answer(2, 'r2', 222); await pause(); answer(1, 'r1', 111); await pause();
    assert.match(nodes.get('dc-activity-overview').innerHTML, />222</);
    assert.doesNotMatch(nodes.get('dc-activity-overview').innerHTML, />111</, 'old router response repainted the new router');
    doc.hidden = true; fire('visibilitychange'); emit('activity:changed', { routerId: 'r2' }); await pause(520); assert.equal(calls.length, 3, 'hidden tab fetched data');
    doc.hidden = false; fire('visibilitychange'); await pause(); assert.equal(calls.length, 4); answer(3, 'r2'); await pause();
    active = false; fire('mikrodash:pagechange'); emit('activity:changed', { routerId: 'r2' }); await pause(520); assert.equal(calls.length, 4, 'another page fetched Activity');
    active = true; fire('mikrodash:pagechange'); await pause(); assert.equal(calls.length, 5); answer(4, 'r2'); await pause();
    emit('perms:changed'); await pause(); assert.equal(calls.length, 6);
    calls[5]!.resolve({ ok: false, status: 403 }); await pause();
    assert.match(nodes.get('dc-activity-overview').textContent, /permissions/);
    for (const kind of ACTIVITY_CARDS) nodes.get('dc-card-activity-' + kind).style.display = 'none';
    fire('dashboard:layout'); emit('activity:changed', { routerId: 'r2' }); await pause(520); assert.equal(calls.length, 6, 'removed cards fetched data');
  } finally { globalThis.document = saved.document; globalThis.fetch = saved.fetch; }
});

test('empty retained activity shows zero metrics and explicit empty charts', () => {
  const nodes = new Map<string, any>();
  for (const kind of ACTIVITY_CARDS) {
    nodes.set('dc-card-activity-' + kind, { querySelector: () => null });
    nodes.set('dc-activity-' + kind, { innerHTML: '' });
  }
  const saved = globalThis.document;
  globalThis.document = { getElementById: id => nodes.get(id) || null } as any;
  try {
    renderActivityCards(result('r1', 0));
    assert.match(nodes.get('dc-activity-overview').innerHTML, /<strong>0<\/strong>/);
    assert.match(nodes.get('dc-activity-timeline').innerHTML, /No DNSUSER events/);
    assert.match(nodes.get('dc-activity-services').innerHTML, /No matching DNSUSER activity/);
    assert.match(nodes.get('dc-activity-categories').innerHTML, /No matching events/);
  } finally { globalThis.document = saved; }
});


test('country card prefers labelled DNS answer evidence and preserves Firewall fallback', () => {
 const node={innerHTML:''}, saved=globalThis.document;
 globalThis.document={getElementById:(id:string)=>id==='dc-activity-countries'?node:null} as any;
 const row={key:'Unknown',label:'Unknown',count:2,service:'',category:'',server:'',ip:'',mac:'',user:'',domains:1,devices:1,last:0};
 try {
  const data=result('r1');data.countries=[{...row,count:1}];data.dnsCountries=[row];
  renderActivityCards(data);
  assert.match(node.innerHTML,/data-filter="dnsCountry"/);
  assert.match(node.innerHTML,/activity-donut-caption">answers/);
  assert.match(node.innerHTML,/not confirmed connections/);
  assert.doesNotMatch(node.innerHTML,/data-filter="country"/);
  const url=new URL(activityCardURL('r1','dnsCountry','Unknown'),'http://example.test');
  assert.equal(url.searchParams.get('dnsCountry'),'Unknown');assert.equal(url.searchParams.has('kind'),false);
  data.dnsCountries=[];renderActivityCards(data);
  assert.match(node.innerHTML,/data-filter="country"/);
  assert.match(node.innerHTML,/activity-donut-caption">events/);
  assert.match(node.innerHTML,/Firewall destination/);
 } finally {globalThis.document=saved;}
});
