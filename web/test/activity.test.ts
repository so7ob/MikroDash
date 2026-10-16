import { test } from 'node:test';
import assert from 'node:assert/strict';
import { rankMarkup, donutMarkup } from '../src/pages/activity';
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
