import { test } from 'node:test';
import assert from 'node:assert/strict';
import { rankMarkup, donutMarkup, initActivityPage } from '../src/pages/activity';
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
 for (const filter of ['service', 'client', 'category', 'country']) {
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
