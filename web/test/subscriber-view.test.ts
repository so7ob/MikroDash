import assert from 'node:assert/strict';
import { test } from 'node:test';
import { accountingStatusMessage, quantity, percentage, limitCard } from '../src/subscriber-view';
import type { SubscriberLimitView } from '../src/gen/payloads';

test('subscriber quantities preserve large integers and exact time', () => {
 assert.equal(quantity('3600','seconds'),'01:00:00');
 assert.equal(quantity('3440','seconds'),'00:57:20');
 assert.equal(quantity('4294967296','download'),'4.0 GiB');
 assert.equal(percentage('9007199254740993','18014398509481986'),50);
 assert.equal(percentage('10','0'),0);
});
test('limit cards preserve Unicode data, escape markup and distinguish zero from unlimited', () => {
 const v:SubscriberLimitView={limit:{id:'اختبار <script>',kind:'seconds',state:'limited',amount:'3600',period:{kind:'daily',zone:'Asia/Aden',anchor:'2026-01-01T00:00:00+03:00'},action:'disconnect',down:'0',up:'0'},used:'3600',reserved:'0',remaining:'0',periodStart:'2026-10-01T00:00:00+03:00',nextReset:'2026-10-02T00:00:00+03:00',quality:'fresh'};
 const html=limitCard(v);assert.ok(html.includes('اختبار &lt;script&gt;'));assert.ok(!html.includes('<script>'));assert.ok(html.includes('00:00:00'));assert.ok(html.includes('Remaining'));
 v.limit.state='unlimited';assert.ok(limitCard(v).includes('unlimited'));
 v.limit.state='unknown';assert.ok(limitCard(v).includes('unknown'));
});

test('pending accounting keeps last confirmed balances distinct from new admission',()=>{
 assert.equal(accountingStatusMessage({pendingAccounting:0}),'');
 const message=accountingStatusMessage({pendingAccounting:2});
 assert.match(message,/2 pending/);assert.match(message,/Last confirmed balances/);assert.match(message,/New Internet connections are blocked/);assert.ok(!message.includes('Unknown'));
});
