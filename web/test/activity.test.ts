import { test } from 'node:test';
import assert from 'node:assert/strict';
import { rankMarkup, donutMarkup, initActivityPage, dnsEvidenceMarkup, eventMarkup } from '../src/pages/activity';
import fs from 'node:fs';
import path from 'node:path';
import { makeDoc } from './dom-shim';
test('activity rankings escape untrusted names and preserve the source identity in drilldowns', () => {
 const html = rankMarkup([{ key: '["user","hs-test","<script>"]', label: '<img src=x onerror=alert(1)> مستخدم', user: '', count: 2, service: '', category: '', server: '', ip: '198.51.100.25', mac: '', domains: 1, devices: 1, last: 1 }], 'client');
 assert.ok(!html.includes('<img')); assert.ok(!html.includes('<script>')); assert.ok(html.includes('&lt;img')); assert.ok(html.includes('Unmapped')); assert.ok(html.includes('data-filter="client"')); assert.ok(html.includes('&quot;hs-test&quot;'));
});
test('activity empty state distinguishes no matching data', () => { assert.match(rankMarkup([], 'service'), /No matching DNSUSER/); });

test('circular distribution preserves the full denominator beyond the top rows', () => {
 const row={key:'s',label:'Synthetic',count:20,service:'',category:'',server:'',ip:'',mac:'',user:'',domains:1,devices:1,last:0};
 const html=donutMarkup([row], 'service', 'Services', 100);
 assert.match(html,/20\.0%/);assert.match(html,/80\.0%/);assert.match(html,/Other/);assert.match(html,/aria-label="Services: 100 events"/);
});


// Drive the page's real delegated click handler, so a helper that is never
// called cannot satisfy the modifier contract. No fetch or real data is used.
interface Selection { filter?: string; value?: string; ctrlKey?: boolean; clear?: string; shape?: 'button' | 'path' }
function clickSelections(actions: Selection[], chart = true, initial?: string): URL[] {
 const root = process.env.MIKRODASH_ROOT || path.resolve(__dirname, '../..');
 const html = fs.readFileSync(path.join(root, 'web/src/ui/page-activity.html'), 'utf8');
 const ids = [...html.matchAll(/id="([^"]+)"/g)].map(m => m[1]);
 assert.ok(ids.length > 30, 'no Activity markup was measured');
 const doc = makeDoc(ids);
 class Target {
  dataset: Selection;
  constructor(action: Selection) { this.dataset = action; }
  closest(selector: string): unknown {
   if (selector === '#activityOverview') return chart ? {} : null;
   const expected = this.dataset.shape === 'path' ? '.activity-donut-slice[data-filter]' : 'button';
   return selector.split(',').map(s => s.trim()).includes(expected) ? this : null;
  }
 }
 const keys = ['document', 'window', 'location', 'history', 'Element'] as const;
 const saved = keys.map(key => (globalThis as any)[key]);
 let url = initial || '/activity?routerId=r1&tab=' + (chart ? 'summary' : 'clients') + '&category=Research&page=3';
 Object.assign(globalThis, {
  document: doc, window: { addEventListener() {} }, Element: Target,
  location: { pathname: '/activity', search: new URL(url, 'http://example.test').search },
  history: { replaceState(_state: unknown, _title: string, next: string) { url = next; } },
 });
 try {
  initActivityPage({ on() {} } as any, () => false, () => [], () => 'r1');
  return actions.map(action => {
   doc.nodes['page-activity'].fire('click', { target: new Target(action), ctrlKey: !!action.ctrlKey });
   return new URL(url, 'http://example.test');
  });
 } finally { keys.forEach((key, i) => { (globalThis as any)[key] = saved[i]; }); }
}
function clickFilter(filter: string, value: string, ctrlKey: boolean, chart = true): URL {
 return clickSelections([{ filter, value, ctrlKey }], chart)[0];
}

test('ordinary chart labels filter in place and preserve the existing filters', () => {
 for (const [filter, value] of [['service', 'ChatGPT'], ['client', 'مستخدم تجريبي'], ['category', 'AI']]) {
  const url = clickFilter(filter, value, false);
  assert.equal(url.searchParams.get('tab'), 'summary', filter + ' left Overview');
  assert.equal(url.searchParams.get(filter), value);
  assert.equal(url.searchParams.get('routerId'), 'r1');
  assert.equal(url.searchParams.get('category'), filter === 'category' ? 'AI' : 'Research');
  assert.equal(url.searchParams.has('page'), false);
 }
});

test('Ctrl chart labels open Events with the same filter and country evidence', () => {
 const url = clickFilter('service', 'ChatGPT', true);
 assert.equal(url.searchParams.get('tab'), 'events'); assert.equal(url.searchParams.get('service'), 'ChatGPT');
 for (const ctrl of [false, true]) {
  const country = clickFilter('country', 'Unknown', ctrl);
  assert.equal(country.searchParams.get('tab'), ctrl ? 'events' : 'summary');
  assert.equal(country.searchParams.get('country'), 'Unknown');
  assert.equal(country.searchParams.get('kind'), 'firewall');
 }
});

test('table row drilldowns keep opening their event evidence', () => {
 const url = clickFilter('client', 'Synthetic User', false, false);
 assert.equal(url.searchParams.get('tab'), 'events'); assert.equal(url.searchParams.get('client'), 'Synthetic User');
});

test('repeated chart selections toggle only their own filter across labels and slices', () => {
 for (const filter of ['service', 'client', 'category', 'country', 'dnsCountry', 'canonical', 'deviceName', 'dnsIP', 'ipBasis', 'server', 'qtype', 'match']) {
  const [on, off] = clickSelections([{ filter, value: 'Synthetic' }, { filter, value: 'Synthetic', shape: 'path' }]);
  assert.equal(on.searchParams.get(filter), 'Synthetic');
  assert.equal(off.searchParams.has(filter), false);
  assert.equal(off.searchParams.get('tab'), 'summary');
  assert.equal(off.searchParams.get('routerId'), 'r1');
  if (filter !== 'category') assert.equal(off.searchParams.get('category'), 'Research');
 }
 const [first, second, off] = clickSelections([
  { filter: 'service', value: 'First', shape: 'path' },
  { filter: 'service', value: 'Second', shape: 'path' },
  { filter: 'service', value: 'Second' },
 ]);
 assert.equal(first.searchParams.get('service'), 'First');
 assert.equal(second.searchParams.get('service'), 'Second');
 assert.equal(off.searchParams.has('service'), false);
});

test('Ctrl selects an already active slice without toggling away its evidence', () => {
 const [, events] = clickSelections([{ filter: 'service', value: 'ChatGPT' }, { filter: 'service', value: 'ChatGPT', ctrlKey: true, shape: 'path' }]);
 assert.equal(events.searchParams.get('tab'), 'events');
 assert.equal(events.searchParams.get('service'), 'ChatGPT');
});

test('country deselection restores the prior source after shared URL reload and chip removal', () => {
 for (const kind of ['', 'dns', 'firewall']) {
  const initial = '/activity?routerId=r1&tab=summary&service=ChatGPT&kind=' + kind;
  const [on] = clickSelections([{ filter: 'country', value: 'Unknown', shape: 'path' }], true, initial);
  assert.equal(on.searchParams.get('kind'), 'firewall');
  for (const action of [{ filter: 'country', value: 'Unknown' }, { clear: 'country' }, { clear: 'all' }]) {
   const [off] = clickSelections([action], true, on.pathname + on.search);
   assert.equal(off.searchParams.has('country'), false);
   assert.equal(off.searchParams.get('kind') || '', kind);
   assert.equal(off.searchParams.has('countrySource'), false);
   if (!action.clear || action.clear !== 'all') assert.equal(off.searchParams.get('service'), 'ChatGPT');
  }
 }
});

test('doughnut arcs keep escaped values, selected legends and a non-selectable Other', () => {
 const row={key:'<"مستخدم>',label:'<script>تجربة</script>',count:20,service:'',category:'',server:'',ip:'',mac:'',user:'',domains:1,devices:1,last:0};
 const html=donutMarkup([row], 'client', 'Users', 100, row.key);
 const paths=[...html.matchAll(/<path\b[^>]*>/g)].map(m=>m[0]);
 assert.equal(paths.length,2);
 assert.match(paths[0], /data-value="&lt;&quot;مستخدم&gt;"/);
 assert.match(paths[0], /data-selected="true"/);
 assert.doesNotMatch(paths[1], /data-filter/);
 assert.match(html, /aria-pressed="true"/);
 assert.match(html, /disabled><span[^]*Other/);
 assert.doesNotMatch(html, /<script>/);
 assert.match(rankMarkup([row],'client',row.key), /aria-pressed="true"/);
 const full=donutMarkup([{...row,count:100}], 'client', 'Users');
 assert.equal([...full.matchAll(/ A 15\.9155/g)].length,2,'full circle needs two actual arcs');
});

test('DNS event metadata renders both address families and escapes every supplied value', () => {
 const html=dnsEvidenceMarkup({kind:'dns',canonical:'<img src=x>',deviceName:'جهاز <script>',ipBasis:'" onmouseover=evil',dnsIPs:['198.51.100.7','2001:db8::7'],dnsAnswers:[{ip:'198.51.100.7',country:'Unknown',city:''},{ip:'2001:db8::7',country:'DE',city:'<City>'}]} as any);
 for(const key of ['canonical','deviceName','ipBasis','dnsIP','dnsCountry'])assert.ok(html.includes('data-filter="'+key+'"'),key+' has no event drilldown');
 assert.match(html,/198\.51\.100\.7/);assert.match(html,/2001:db8::7/);assert.match(html,/جهاز/);assert.match(html,/>Germany<\/bdi> · <bdi[^>]*>&lt;City&gt;<\/bdi>/);
 assert.doesNotMatch(html,/<img|<script>|data-value="" onmouseover=/);
 const empty=dnsEvidenceMarkup({kind:'dns',canonical:'',deviceName:'',ipBasis:'',dnsIPs:[],dnsAnswers:[]} as any);
 assert.match(empty,/Not recorded/);assert.equal(dnsEvidenceMarkup({kind:'firewall'} as any),'');
});


test('DNS country selection filters DNS without claiming a Firewall connection', () => {
 const url=clickFilter('dnsCountry','DE',false);
 assert.equal(url.searchParams.get('tab'),'summary');
 assert.equal(url.searchParams.get('dnsCountry'),'DE');
 assert.equal(url.searchParams.has('kind'),false);
 const row={key:'DE',label:'Germany',count:3,service:'',category:'',server:'',ip:'',mac:'',user:'',domains:1,devices:1,last:0};
 const html=donutMarkup([row],'dnsCountry','DNS answer countries',undefined,'DE','answers');
 assert.match(html,/aria-label="DNS answer countries: 3 answers"/);
 assert.match(html,/activity-donut-caption">answers/);
 assert.doesNotMatch(html,/3 events/);
});

// The real page handler owns copy-vs-filter precedence and notification lifetime.
async function copyPage(run: (h: any) => Promise<void>): Promise<void> {
 const root = process.env.MIKRODASH_ROOT || path.resolve(__dirname, '../..');
 const html = fs.readFileSync(path.join(root, 'web/src/ui/page-activity.html'), 'utf8');
 const doc = makeDoc([...html.matchAll(/id="([^"]+)"/g)].map(m => m[1]));
 const keys = ['document', 'window', 'location', 'history', 'Element', 'navigator', 'setTimeout', 'clearTimeout'];
 const saved = keys.map(key => Object.getOwnPropertyDescriptor(globalThis, key));
 const writes: string[] = [], pending: { resolve: () => void; reject: () => void }[] = [];
 const timers = new Map<number, { fn: () => void; ms: number }>(); let nextTimer = 0;
 const initial = '/activity?routerId=r1&tab=events&service=Research&page=2'; let url = initial;
 const nav: any = { clipboard: { writeText(text: string) { writes.push(text); return new Promise<void>((resolve, reject) => pending.push({ resolve, reject: () => reject(new Error('private error')) })); } } };
 class Target {
  dataset: Record<string, string>; parent?: Target; inTable: boolean;
  constructor(dataset: Record<string, string>, inTable = true, parent?: Target) { this.dataset = dataset; this.inTable = inTable; this.parent = parent; }
  closest(selector: string): any {
   if (selector === '#activityTableBody') return this.inTable ? doc.nodes.activityTableBody : null;
   if (selector === '[data-activity-copy]') return this.dataset.activityCopy !== undefined ? this : this.parent?.closest(selector);
   if (selector.includes('button')) return this.dataset.filter || this.dataset.view ? this : this.parent?.closest(selector);
   return null;
  }
 }
 const globals = { document: doc, window: { addEventListener() {} }, Element: Target, navigator: nav,
  location: { pathname: '/activity', search: initial.slice(initial.indexOf('?')) },
  history: { replaceState(_state: unknown, _title: string, next: string) { url = next; } },
  setTimeout(fn: () => void, ms: number) { timers.set(++nextTimer, { fn, ms }); return nextTimer; },
  clearTimeout(id: number) { timers.delete(id); },
 };
 for (const [key, value] of Object.entries(globals)) Object.defineProperty(globalThis, key, { configurable: true, writable: true, value });
 try {
  initActivityPage({ on() {} } as any, () => false, () => [], () => 'r1');
  const click = (dataset: Record<string, string>, ctrlKey = true, nested = false, inTable = true) => {
   const parent = new Target(dataset, inTable), target = nested ? new Target({}, inTable, parent) : parent;
   const e = { target, ctrlKey, button: 0, prevented: false, preventDefault() { e.prevented = true; } };
   doc.nodes['page-activity'].fire('click', e); return e;
  };
  await run({ doc, nav, writes, pending, timers, click, initial, url: () => url, notice: doc.nodes.activityCopyNotice });
 } finally {
  keys.forEach((key, i) => { if (saved[i]) Object.defineProperty(globalThis, key, saved[i]!); else delete (globalThis as any)[key]; });
 }
}

test('Ctrl copies only the clicked event value, including nested targets, without filtering', async () => {
 await copyPage(async h => {
  for (const value of ['مستخدم "<تجريبي>"', '198.51.100.7', '2001:db8::7', 'Example City']) {
   const e = h.click({ activityCopy: value, filter: 'client', value: '["internal","key"]' }, true, true);
   assert.equal(h.writes.at(-1), value); assert.equal(h.url(), h.initial); assert.ok(e.prevented);
   assert.notEqual(h.notice.textContent, 'Copied to clipboard', 'success shown before clipboard completed');
   h.pending.shift().resolve(); await Promise.resolve();
   assert.equal(h.notice.textContent, 'Copied to clipboard');
   const timer = [...h.timers.values()][0] as any; assert.equal(h.timers.size, 1); assert.equal(timer.ms, 2000);
   timer.fn(); h.timers.clear(); assert.equal(h.notice.textContent, '');
  }
 });
});

test('plain event clicks filter, while Ctrl on labels or outside Events does not copy', async () => {
 await copyPage(async h => {
  h.click({}); assert.deepEqual(h.writes, []); assert.equal(h.url(), h.initial);
  h.click({ activityCopy: 'Visible', filter: 'client', value: 'internal-key' }, false);
  assert.equal(new URL(h.url(), 'https://example.test').searchParams.get('client'), 'internal-key');
  assert.deepEqual(h.writes, []);
  h.click({ activityCopy: 'Outside' }, true, false, false); assert.deepEqual(h.writes, []);
  h.click({ view: 'clients' }, false, false, false);
  h.click({ activityCopy: 'Other view', filter: 'client', value: 'other' }); assert.deepEqual(h.writes, []);
 });
});

test('copy failure, repeated clicks and leaving Events cannot show stale success', async () => {
 await copyPage(async h => {
  h.click({ activityCopy: 'first' }); h.click({ activityCopy: 'second' });
  h.pending[1].reject(); await Promise.resolve(); assert.equal(h.notice.textContent, 'Could not copy');
  h.pending[0].resolve(); await Promise.resolve(); assert.equal(h.notice.textContent, 'Could not copy');
  h.click({ activityCopy: 'third' });
  h.click({ view: 'summary' }, false, false, false);
  h.pending[2].resolve(); await Promise.resolve(); assert.equal(h.notice.textContent, ''); assert.equal(h.timers.size, 0);
 });
 await copyPage(async h => {
  h.click({ activityCopy: 'leaving page' });
  h.doc.dispatchEvent({ type: 'mikrodash:pagechange' });
  h.pending[0].resolve(); await Promise.resolve(); assert.equal(h.notice.textContent, '');
 });
});

test('event copy uses the shared HTTP fallback and reports its actual outcome', async () => {
 await copyPage(async h => {
  delete h.nav.clipboard;
  let textarea: any, selected = '', removed = 0, success = true;
  h.doc.createElement = () => textarea = { value: '', style: {}, setAttribute() {}, select() { selected = this.value; } };
  h.doc.body = { appendChild() {}, removeChild(node: any) { assert.equal(node, textarea); removed++; } };
  h.doc.execCommand = (command: string) => { assert.equal(command, 'copy'); return success; };
  h.click({ activityCopy: '198.51.100.9' });
  assert.equal(selected, '198.51.100.9'); assert.equal(removed, 1); assert.equal(h.notice.textContent, 'Copied to clipboard');
  success = false; h.click({ activityCopy: '2001:db8::9' });
  assert.equal(h.notice.textContent, 'Could not copy'); assert.equal(removed, 2);
 });
});

test('every DNS and Firewall event field has its own escaped copy value, excluding missing placeholders', () => {
 const dns = { at: Date.UTC(2026, 9, 18, 12), timeCertain: true, kind: 'dns', client: '["private-key"]', user: 'مستخدم <"&>', server: 'hs-example', ip: '198.51.100.25', mac: '02:00:00:00:00:25', domain: 'example.test', service: 'Service', qtype: 'AAAA', match: 'active-now', category: 'Research', deviceName: 'Device', canonical: 'edge.example.test', ipBasis: 'dns-cache', dnsAnswers: [{ ip: '2001:db8::7', country: 'DE', city: 'Example City' }], count: 1234 };
 const html = eventMarkup(dns as any);
 for (const value of ['DNSUSER', 'مستخدم &lt;&quot;&amp;&gt;', dns.server, dns.ip, dns.mac, dns.domain, dns.service, dns.qtype, dns.match, dns.category, dns.deviceName, dns.canonical, dns.ipBasis, '2001:db8::7', 'Germany', 'Example City', (1234).toLocaleString()]) assert.ok(html.includes('data-activity-copy="' + value + '"'), value + ' is not independently copyable');
 assert.match(html, /data-activity-copy="2026-10-18 [0-9:]+"/);
 assert.ok(!html.includes('data-activity-copy="[&quot;private-key'));
 const fw = eventMarkup({ ...dns, kind: 'firewall', domain: '', destination: '203.0.113.4:443', protocol: 'tcp', country: 'DE', city: 'Example City', in: 'bridge', out: 'ether1', length: '60' } as any);
 for (const value of ['Firewall', '203.0.113.4:443', 'tcp', 'DE', 'Example City', 'bridge', 'ether1', '60']) assert.ok(fw.includes('data-activity-copy="' + value + '"'), value);
 const empty = eventMarkup({ ...dns, user: '', server: '', ip: '', mac: '', deviceName: '', canonical: '', ipBasis: '', qtype: '', match: '', category: '', dnsAnswers: [] } as any);
 assert.doesNotMatch(empty, /data-activity-copy="(?:|—|\?|-)"/);
 assert.match(empty, /Not recorded/);
});
