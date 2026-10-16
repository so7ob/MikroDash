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
function clickFilter(filter: string, value: string, ctrlKey: boolean, chart = true): URL {
 const root = process.env.MIKRODASH_ROOT || path.resolve(__dirname, '../..');
 const html = fs.readFileSync(path.join(root, 'web/src/ui/page-activity.html'), 'utf8');
 const ids = [...html.matchAll(/id="([^"]+)"/g)].map(m => m[1]);
 assert.ok(ids.length > 30, 'no Activity markup was measured');
 const doc = makeDoc(ids);
 class Target {
  dataset = { filter, value };
  closest(selector: string): unknown { return selector === 'button' ? this : selector === '#activityOverview' && chart ? {} : null; }
 }
 const keys = ['document', 'window', 'location', 'history', 'Element'] as const;
 const saved = keys.map(key => (globalThis as any)[key]);
 let url = '/activity?routerId=r1&tab=' + (chart ? 'summary' : 'clients') + '&category=AI&page=3';
 Object.assign(globalThis, {
  document: doc, window: { addEventListener() {} }, Element: Target,
  location: { pathname: '/activity', search: new URL(url, 'http://example.test').search },
  history: { replaceState(_state: unknown, _title: string, next: string) { url = next; } },
 });
 try {
  initActivityPage({ on() {} } as any, () => false, () => [], () => 'r1');
  doc.nodes['page-activity'].fire('click', { target: new Target(), ctrlKey });
  return new URL(url, 'http://example.test');
 } finally { keys.forEach((key, i) => { (globalThis as any)[key] = saved[i]; }); }
}

test('ordinary chart labels filter in place and preserve the existing filters', () => {
 for (const [filter, value] of [['service', 'ChatGPT'], ['client', 'مستخدم تجريبي'], ['category', 'AI']]) {
  const url = clickFilter(filter, value, false);
  assert.equal(url.searchParams.get('tab'), 'summary', filter + ' left Overview');
  assert.equal(url.searchParams.get(filter), value);
  assert.equal(url.searchParams.get('routerId'), 'r1');
  assert.equal(url.searchParams.get('category'), 'AI');
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
