import assert from 'node:assert/strict';
import { test } from 'node:test';
import { initSubscribersPage } from '../src/pages/subscribers';

test('opening Subscriber Management loads data through the application navigation event', async () => {
  const events = new EventTarget();
  const notice = {textContent: ''};
  const badge = {textContent: ''};
  const originalDocument = globalThis.document;
  const originalFetch = globalThis.fetch;
  const calls: string[] = [];
  globalThis.document = {
    getElementById: (id: string) => id === 'subscriberAdminNotice' ? notice : id === 'subscribersBadge' ? badge : null,
    addEventListener: events.addEventListener.bind(events),
  } as unknown as Document;
  globalThis.fetch = (async (url: string | URL | Request) => {
    calls.push(String(url));
    return Response.json({accounts:[{id:'synthetic',username:'Synthetic account',name:'اختبار',status:'active'}],plans:[],mode:'shadow'});
  }) as typeof fetch;
  try {
    initSubscribersPage();
    events.dispatchEvent(new CustomEvent('mikrodash:pagechange', {detail:'dashboard'}));
    assert.deepEqual(calls, []);
    events.dispatchEvent(new CustomEvent('mikrodash:pagechange', {detail:'subscribers'}));
    await new Promise(resolve => setImmediate(resolve));
    assert.deepEqual(calls, ['/api/subscribers']);
    assert.equal(badge.textContent, '1');
    assert.equal(notice.textContent, 'Mode: shadow');
    events.dispatchEvent(new CustomEvent('mikrodash:pagechange', {detail:'subscribers'}));
    assert.deepEqual(calls, ['/api/subscribers'], 'a loaded page must not duplicate its initial request');
  } finally {
    globalThis.document = originalDocument;
    globalThis.fetch = originalFetch;
  }
});
