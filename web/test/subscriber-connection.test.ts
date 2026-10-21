import assert from 'node:assert/strict';
import { test } from 'node:test';
import { connectionReference, connectionReason, connectionView, portalEntry } from '../src/subscriber-connection';
import { hotspotDestination, hotspotReturn } from '../src/subscriber-handoff';
import type { SubscriberConnectionStatus } from '../src/gen/payloads';

const flow = 'a'.repeat(64);
const observed = Date.parse('2026-10-21T00:00:00Z');
const status: SubscriberConnectionStatus = { version: 'synthetic', flowId: flow, stage: 'connected', network: 'شبكة C', device: '02:00:00:00:00:64', leaseId: 'synthetic', reasons: [], requestedAt: '2026-10-21T00:00:00Z', observedAt: '2026-10-21T00:00:00Z' };

test('portal entry parses hints without treating them as network authority', () => {
  assert.deepEqual(portalEntry('?connect=C'), {network:'C',flowId:'',failed:false});
  assert.deepEqual(portalEntry('?connect=C&hotspot=failed'), {network:'C',flowId:'',failed:true});
  assert.deepEqual(portalEntry(`?connection=${flow}&connect=C`), {network:'',flowId:flow,failed:false});
  assert.equal(portalEntry('?connect=C&connect=D').network, '');
  assert.equal(portalEntry(`?connection=${flow}&connection=${flow}`).flowId, '');
  assert.equal(portalEntry('?connect='+encodeURIComponent('سحب')).network, 'سحب');
  assert.equal(portalEntry('?connect='+ 'x'.repeat(129)).network, '');
  for (const bad of [null,'',flow.toUpperCase(),flow+'0','https://foreign.example.test/'+flow,'mdt_'+flow,'<script>']) assert.equal(connectionReference(bad),'');
});

test('Internet connected requires this flow fresh router evidence and expires visibly', () => {
  const live=connectionView(status,observed+5000);
  assert.equal(live.connected,true); assert.equal(live.tone,'ok'); assert.match(live.message,/شبكة C/);
  for (const now of [observed+5001,observed-1001]) {
    const stale=connectionView(status,now);
    assert.equal(stale.connected,false); assert.equal(stale.tone,'warn');
  }
  assert.equal(connectionView({...status,observedAt:'invalid'},observed).connected,false);
  assert.equal(connectionView(null,observed).connected,false);
  for (const stage of ['requested','authorizing','authorized','rejected','expired','disconnected','unconfirmed','unexpected']) assert.equal(connectionView({...status,stage},observed).connected,false,stage);
});

test('denial messages preserve original constraint names without translating user data', () => {
  assert.equal(connectionReason('exhausted:باقة-1'),'Allowance exhausted: باقة-1');
  assert.match(connectionReason('accounting-reconciliation-required'),/reconciliation/);
  const view=connectionView({...status,stage:'rejected',reasons:['exhausted:DAILY-1H','session-limit']},observed);
  assert.equal(view.tone,'err');assert.match(view.message,/DAILY-1H/);assert.match(view.message,/session limit/);
});


test('HotSpot returns accept only one owned root reference after a single decode', () => {
  const origin='https://portal.example.test:9444';
  const owned=`${origin}/?connection=${flow}`;
  assert.equal(hotspotReturn(encodeURIComponent(owned),origin),owned);
  for (const destination of [
    `https://foreign.example.test/?connection=${flow}`,
    `${origin}/admin?connection=${flow}`,
    `${origin}/?connection=${flow}&return=https://foreign.example.test`,
    `${origin}/?connection=${flow}&connection=${flow}`,
    `${origin}/?connection=not-a-reference`,
    `${origin}/?connection=${flow}#fragment`,
    `https://user:pass@portal.example.test:9444/?connection=${flow}`,
    `http://portal.example.test:9444/?connection=${flow}`,
    '%2568ttps%253A%252F%252Fforeign.example.test',
    '$(link-redirect-esc)', '%E0%A4%A', 'javascript:alert(1)',
  ]) assert.equal(hotspotReturn(encodeURIComponent(destination),origin),'',destination);
  assert.equal(hotspotDestination('success',origin,origin,encodeURIComponent(owned),''),owned);
  assert.equal(hotspotDestination('failure',origin,origin,'',encodeURIComponent(owned)),owned+'&hotspot=failed');
  assert.equal(hotspotDestination('failure',origin,origin,'',''),origin+'/?hotspot=failed');
  assert.equal(hotspotDestination('account',origin,origin,'',''),origin+'/');
  assert.equal(hotspotDestination('login',origin,origin+'/?connect=C','',''),origin+'/?connect=C');
  assert.throws(()=>hotspotDestination('login',origin,'https://foreign.example.test/','',''));
  assert.throws(()=>hotspotDestination('success','http://portal.example.test',origin,'',''));
});
