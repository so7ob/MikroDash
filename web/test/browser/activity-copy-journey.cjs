const assert = require('node:assert/strict');
const { AxeBuilder } = require('@axe-core/playwright');

// Runs inside the existing real-page/real-API journey, using only synthetic data.
module.exports = async function copyJourney(page, context, settled) {
 const origin = new URL(page.url()).origin;
 await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin });
 const notice = page.locator('#activityCopyNotice');
 const readCopy = () => page.evaluate(() => navigator.clipboard.readText());
 let requests = 0;
 const countRequest = request => { if (new URL(request.url()).pathname.startsWith('/api/activity/')) requests++; };
 page.on('request', countRequest);
 // Add adversarial text and known geolocation to one synthetic response only.
 const special = 'مستخدم <"&> تجريبي';
 const enrich = async route => {
  const response = await route.fetch(), data = await response.json();
  const row = data.events.find(e => e.kind === 'dns' && e.dnsAnswers.length);
  if (row) { row.user = special; row.deviceName = 'جهاز <script> & "'; row.dnsAnswers[0].country = 'DE'; row.dnsAnswers[0].city = 'Example City'; }
  await route.fulfill({ response, json: data });
 };
 await page.route('**/api/activity/query?**', enrich);
 await page.locator('[data-view="events"]').click(); await settled();
 assert.equal(await page.locator('#activityCopyHint').isVisible(), true);
 const enriched = page.locator('#activityTableBody tr').filter({ has: page.locator('[data-activity-copy="Example City"]') }).first();
 assert.equal(await enriched.count(), 1, 'no enriched DNS row measured');
 assert.equal(await page.locator('#activityTableBody script, #activityTableBody img').count(), 0);
 const copy = async (target, expected) => {
  const url = page.url(), before = requests;
  await target.click({ modifiers: ['Control'] });
  await page.waitForFunction(() => document.querySelector('#activityCopyNotice')?.textContent === 'Copied to clipboard');
  assert.equal(await readCopy(), expected, 'clipboard differs from the individual visible value');
  assert.equal(page.url(), url, 'copy changed the URL');
  assert.equal(requests, before, 'copy requested Activity data');
 };
 const dnsValues = enriched.locator('[data-activity-copy]');
 assert.ok(await dnsValues.count() >= 20, 'too few independently copyable DNS fields');
 for (let i = 0; i < await dnsValues.count(); i++) {
  const target = dnsValues.nth(i), expected = await target.textContent();
  await copy(target, expected);
 }
 assert.ok((await dnsValues.allTextContents()).includes(special), 'Arabic/special-name coverage missing');
 assert.ok((await dnsValues.allTextContents()).some(v => v.includes('2001:db8:')), 'IPv6 coverage missing');
 // A nested bdi must copy the name, never the serialized client filter key.
 const client = enriched.locator('button[data-filter="client"]');
 assert.notEqual(await client.getAttribute('data-value'), special);
 await copy(client.locator('bdi'), special);
 // Labels and missing values leave the previous clipboard intact.
 await enriched.locator('dt').first().click({ modifiers: ['Control'] });
 assert.equal(await readCopy(), special);
 const rowURL = page.url(), beforeLabel = requests;
 await page.locator('#activityTableBody tr').filter({ hasText: 'Not recorded' }).first().locator('.activity-dns-ips').click({ modifiers: ['Control'] });
 assert.equal(await readCopy(), special); assert.equal(page.url(), rowURL); assert.equal(requests, beforeLabel);
 await page.unroute('**/api/activity/query?**', enrich);
 await page.locator('#activityKind').selectOption('firewall'); await settled();
 const firewall = page.locator('#activityTableBody tr').first().locator('[data-activity-copy]');
 assert.ok(await firewall.count() >= 10, 'no Firewall coverage');
 for (let i = 0; i < await firewall.count(); i++) await copy(firewall.nth(i), await firewall.nth(i).textContent());
 // Clipboard rejection gives failure, not a success toast or a filter.
 await page.evaluate(() => {
  window.activityOriginalClipboard = navigator.clipboard;
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: () => Promise.reject(new Error('private clipboard error')) } });
 });
 const failedURL = page.url(), failedRequests = requests;
 await firewall.first().click({ modifiers: ['Control'] });
 await page.waitForFunction(() => document.querySelector('#activityCopyNotice')?.textContent === 'Could not copy');
 assert.equal(page.url(), failedURL); assert.equal(requests, failedRequests);
 // No Clipboard API is the HTTP/LAN path. Real execCommand writes to the clipboard.
 await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined }));
 const fallbackValue = await firewall.first().textContent();
 await firewall.first().click({ modifiers: ['Control'] });
 await page.waitForFunction(() => document.querySelector('#activityCopyNotice')?.textContent === 'Copied to clipboard');
 assert.equal(await page.evaluate(() => window.activityOriginalClipboard.readText()), fallbackValue);
 assert.equal(await page.locator('textarea[readonly]').count(), 0, 'fallback textarea leaked');
 await page.evaluate(() => { delete navigator.clipboard; delete window.activityOriginalClipboard; });
 // Restore DNS rows; ordinary clicks keep filtering and copy survives redraws.
 await page.locator('#activityKind').selectOption(''); await settled();
 const domain = page.locator('#activityTableBody button[data-filter="domain"]').first();
 const domainValue = await domain.getAttribute('data-value');
 await domain.locator('bdi').click(); await settled();
 assert.equal(new URL(page.url()).searchParams.get('domain'), domainValue);
 await page.locator('[data-clear="all"]').click(); await settled();
 await page.locator('#activityNext').click(); await settled();
 assert.match(await page.locator('#activityPageLabel').textContent(), /^2 /);
 let target = page.locator('#activityTableBody [data-activity-copy]').first();
 await copy(target, await target.textContent());
 await page.locator('#activityTableHead th').first().click(); await settled();
 await copy(target, await target.textContent());
 await page.locator('#activityRefresh').click(); await settled();
 await copy(target, await target.textContent());
 // Controlled time proves replacement, expiry and leaving-tab cleanup without sleeps.
 await page.clock.install(); await page.clock.pauseAt(new Date());
 await copy(target, await target.textContent());
 await page.clock.runFor(1500);
 await copy(target, await target.textContent());
 assert.equal(await page.locator('#activityCopyNotice').count(), 1);
 await page.clock.runFor(1000); assert.equal(await notice.textContent(), 'Copied to clipboard');
 await page.clock.runFor(1001); assert.equal(await notice.textContent(), '');
 await copy(target, await target.textContent());
 await page.locator('[data-view="clients"]').click(); await settled();
 assert.equal(await notice.textContent(), ''); assert.equal(await page.locator('#activityCopyHint').isVisible(), false);
 await page.clock.resume();
 await page.locator('[data-view="events"]').click(); await settled();
 for (const width of [1280, 375]) for (const mode of ['dark', 'light', 'nord']) {
  await page.setViewportSize({ width, height: 900 });
  await page.evaluate(mode => {
   document.documentElement.dataset.theme = mode === 'light' ? 'light' : 'dark';
   document.documentElement.dataset.palette = mode === 'nord' ? 'nord' : '';
  }, mode);
  target = page.locator('#activityTableBody [data-activity-copy]').first();
  await copy(target, await target.textContent());
  assert.equal(await notice.getAttribute('role'), 'status'); assert.equal(await notice.getAttribute('aria-live'), 'polite');
  assert.equal(await notice.evaluate(e => getComputedStyle(e).pointerEvents), 'none');
  const bounds = await notice.boundingBox(); assert.ok(bounds && bounds.x >= 0 && bounds.x + bounds.width <= width, 'notification exceeds viewport');
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'horizontal page overflow');
  await page.screenshot({ path: `/tmp/mikrodash-activity-copy-${width}-${mode}.png` });
  const axe = await new AxeBuilder({ page }).include('#page-activity').withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze();
  assert.deepEqual(axe.violations.map(v => ({ id: v.id, targets: v.nodes.map(n => n.target) })), [], `${width}/${mode} copy accessibility`);
 }
 await page.setViewportSize({ width: 1280, height: 1000 });
 await page.evaluate(() => { delete document.documentElement.dataset.theme; delete document.documentElement.dataset.palette; });
 await page.locator('[data-view="summary"]').click(); await settled();
 page.off('request', countRequest);
 console.log('clipboard: individual DNS/Firewall values, Arabic/escaping, nested targets, no filter/requests, rejection, real HTTP fallback, redraws, timer/cleanup and 375/1280 dark/light/Nord axe passed');
};
