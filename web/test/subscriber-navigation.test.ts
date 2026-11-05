import assert from 'node:assert/strict';
import { test } from 'node:test';
import { initSubscribersPage } from '../src/pages/subscribers';

for (const mode of ['disabled','shadow','pilot','managed']) test('Subscriber Management loads independently of network runtime: '+mode, async () => {
  const events = new EventTarget();
  const notice = {textContent: ''};
  const badge = {textContent: ''};
  const runtime = {innerHTML: ''};
  const originalDocument = globalThis.document;
  const originalFetch = globalThis.fetch;
  const calls: string[] = [];
  globalThis.document = {
    getElementById: (id: string) => id === 'subscriberAdminNotice' ? notice : id === 'subscribersBadge' ? badge : id === 'subscriberRuntimeStatus' ? runtime : null,
    addEventListener: events.addEventListener.bind(events),
    querySelectorAll: () => [],
  } as unknown as Document;
  globalThis.fetch = (async (url: string | URL | Request) => {
    calls.push(String(url));
    return Response.json({accounts:[{id:'synthetic',username:'Synthetic account',name:'اختبار',status:'active'}],plans:[],mode});
  }) as typeof fetch;
  try {
    initSubscribersPage();
    events.dispatchEvent(new CustomEvent('mikrodash:pagechange', {detail:'dashboard'}));
    assert.deepEqual(calls, []);
    events.dispatchEvent(new CustomEvent('mikrodash:pagechange', {detail:'subscribers'}));
    await new Promise(resolve => setImmediate(resolve));
    assert.deepEqual(calls, ['/api/subscribers']);
    assert.equal(badge.textContent, '1');
    assert.equal(notice.textContent, 'Mode: '+mode);
    assert.match(runtime.innerHTML,/Network runtime:/);
    if(mode==='disabled') {
      assert.match(runtime.innerHTML,/You can prepare subscribers/);
      assert.match(runtime.innerHTML,/portal and RADIUS are disabled/);
      assert.match(runtime.innerHTML,/-subscriber-config/);
    }
    notice.textContent='Subscriber created.';
    assert.match(runtime.innerHTML,/Network runtime:/, 'transient notices must not hide operational status');
    events.dispatchEvent(new CustomEvent('mikrodash:pagechange', {detail:'subscribers'}));
    assert.deepEqual(calls, ['/api/subscribers'], 'a loaded page must not duplicate its initial request');
  } finally {
    globalThis.document = originalDocument;
    globalThis.fetch = originalFetch;
  }
});
