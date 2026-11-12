import assert from 'node:assert/strict';
import { test } from 'node:test';
import { draftLimit, SubscriberLimitDraft, type LimitInput } from '../src/pages/subscriber-plan-limits';

const input: LimitInput = {kind:'seconds',amount:'003600',period:'daily',zone:'Asia/Aden',anchor:'2026-01-01T00:25:00+03:00',action:'disconnect',down:'0',up:'0'};

test('draft limits retain exact decimal quantities, calendar zone and positive throttle rates', () => {
  const limit = draftLimit('limit-1', {...input,kind:'total',amount:'09007199254740993',action:'throttle',down:'008000000',up:'01000000',zone:'UTC'});
  assert.equal(limit.amount, '9007199254740993');
  assert.equal(limit.down, '8000000'); assert.equal(limit.up, '1000000');
  assert.deepEqual(limit.period, {kind:'daily',zone:'UTC',anchor:input.anchor});
  assert.equal(draftLimit('limit-1', {...input,amount:'0'}).amount, '0');
  assert.equal(draftLimit('limit-1', {...input,amount:'9223372036854775807'}).amount, '9223372036854775807');
});

test('invalid quota, calendar and throttle edits leave the previous draft intact', () => {
  const draft = new SubscriberLimitDraft(); draft.save(input);
  const original = draft.snapshot();
  for (const patch of [
    {amount:'9223372036854775808'}, {amount:'1e3'}, {amount:'-1'}, {amount:' 8'}, {amount:''},
    {kind:'unknown'}, {period:'fortnightly'}, {action:'unknown'}, {zone:''}, {zone:'Not/AZone'},
    {anchor:'2026-01-01'}, {anchor:'2026-01-01T00:00:00'}, {anchor:'not-a-date'},
    {action:'throttle',down:'0',up:'1'}, {action:'throttle',down:'1',up:'0'},
    {action:'throttle',down:'1',up:'9223372036854775808'},
  ]) {
    assert.throws(() => draft.save({...input,...patch}, 'limit-1'));
    assert.deepEqual(draft.snapshot(), original);
  }
});

test('editing, removing and adding concurrent limits preserves identity and detached saved payloads', () => {
  const draft = new SubscriberLimitDraft();
  draft.save(input); draft.save({...input,kind:'total',amount:'0100'});
  const submitted = draft.snapshot();
  draft.save({...input,amount:'7200'}, 'limit-1');
  assert.deepEqual(draft.snapshot().map(l => [l.id,l.amount]), [['limit-1','7200'],['limit-2','100']]);
  draft.remove('limit-1'); draft.save({...input,kind:'upload',amount:'200'});
  assert.deepEqual(draft.snapshot().map(l => l.id), ['limit-2','limit-3']);
  assert.throws(() => draft.save(input, 'limit-1'));
  assert.equal(submitted[0].amount, '3600');
  submitted[1].period.zone = 'UTC';
  assert.equal(draft.snapshot()[0].period.zone, 'Asia/Aden');
  draft.remove('limit-2'); draft.remove('limit-3');
  assert.deepEqual(draft.snapshot(), []);
});
