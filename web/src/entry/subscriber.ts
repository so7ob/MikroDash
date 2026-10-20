import { esc, pill } from '../dom';
import { fmtTs } from '../timefmt';
import { accountingStatusMessage, boundedElapsed, quantity, snapshotCards } from '../subscriber-view';
import { connectionReference, connectionView, portalEntry } from '../subscriber-connection';
import type { SubscriberConnectionStatus, SubscriberSnapshot } from '../gen/payloads';

function node(id: string): HTMLElement { const v = document.getElementById(id); if (!v) throw new Error('Missing portal element'); return v; }
function value(id: string): string { const v = node(id); return v instanceof HTMLInputElement || v instanceof HTMLSelectElement ? v.value : ''; }
function text(id: string, message: string): void { const target = node(id); if (target.textContent !== message) target.textContent = message; }
const entry = portalEntry(location.search);
const referenceKey = 'md_subscriber_connection';
let connectIntent = entry.network;
let csrf = '', mode = '', current: SubscriberSnapshot | null = null, socket: WebSocket | null = null, signedIn = false;
let flowId = entry.flowId, connection: SubscriberConnectionStatus | null = null, connectBusy = false, loginBusy = false, authEpoch = 0;
let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
try { if (!flowId) flowId = connectionReference(sessionStorage.getItem(referenceKey)); } catch { /* Storage is optional in captive browsers. */ }
if (location.search || location.hash) history.replaceState(null, '', location.pathname);
function saveReference(): void {
  try { if (flowId) sessionStorage.setItem(referenceKey, flowId); else sessionStorage.removeItem(referenceKey); } catch { /* The owned return URL also carries the reference. */ }
}
function clearConnection(): void { flowId = ''; connection = null; saveReference(); renderConnection(); }
function notice(message: string): void { text('portalNotice', message); }
class PortalError extends Error { constructor(readonly status: number, message: string) { super(message); } }
async function api<T>(path: string, body?: unknown): Promise<T> {
  const controller = new AbortController(), timeout = setTimeout(() => controller.abort(), 10000);
  try {
    const response = await fetch(path, { method: body === undefined ? 'GET' : 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf }, body: body === undefined ? undefined : JSON.stringify(body), signal: controller.signal });
    const result = await response.json().catch(() => null);
    if (!response.ok) throw new PortalError(response.status, typeof result?.error === 'string' ? result.error : 'Request failed');
    if (result === null) throw new Error('The portal response could not be read.');
    return result as T;
  } finally { clearTimeout(timeout); }
}
function renderConnection(): void {
  const view = connectionView(connection, Date.now());
  text('internetState', view.label); node('internetState').className = `pill pill-${view.tone}`;
  text('internetDetail', view.message);
  node('connectionObserved').hidden = connection === null;
  text('connectionObserved', connection && !connection.observedAt.startsWith('0001-') ? `Router observed: ${fmtTs(connection.observedAt, true, 'Asia/Aden')}` : 'Router confirmation has not been received for this request.');
  const button = node('internetConnect'); if (button instanceof HTMLButtonElement) button.disabled = connectBusy || view.connected;
}
function render(v: SubscriberSnapshot): void {
  const wasPending = (current?.pendingAccounting || 0) > 0;
  current = v;
  text('subscriptionTitle', v.subscription.plan.name);
  text('subscriptionSummary', `Subscription ${v.subscription.id} · ${v.subscription.status} · Expires ${v.subscription.expiresAt.startsWith('0001-') ? 'after first connection' : fmtTs(v.subscription.expiresAt, true, 'Asia/Aden')}`);
  node('subscriberLimits').innerHTML = snapshotCards(v);
  text('syncState', `Data: ${v.quality} · Observed ${v.observedAt} · Last accounting ${v.lastSyncedAt.startsWith('0001-') ? 'not received yet' : v.lastSyncedAt}`);
  text('speedState', `Selected: ${v.subscription.plan.speeds.find(p => p.id === v.subscription.speedId)?.name || v.subscription.speedId} · Application: ${v.subscription.speedState} · Evaluated ceiling: ${v.decision.down} bit/s download / ${v.decision.up} bit/s upload`);
  node('speedButtons').innerHTML = v.subscription.plan.speeds.map(p => `<button class="sbtn sbtn-outline" data-speed="${esc(p.id)}" aria-pressed="${p.id === v.subscription.speedId}">${esc(p.name)}</button>`).join('');
  node('subscriberSessions').innerHTML = v.sessions.map(l => `<div class="subscriber-session"><span>${esc(l.device)}</span>${pill(l.connectionState || 'unknown', l.connectionState === 'connected' ? 'ok' : 'muted')}<span>${esc(l.network)} · ${quantity(l.seconds, 'seconds')} · Applied queue ceiling: ${!l.rateState || l.rateState === 'unknown' ? 'Unknown' : `${esc(l.appliedDown)} bit/s download / ${esc(l.appliedUp)} bit/s upload (${esc(l.rateState)})`}</span>${l.state === 'active' ? `<button class="sbtn sbtn-danger" data-disconnect="${esc(l.id)}">Disconnect device</button>` : ''}</div>`).join('') || 'No internet sessions yet.';
  text('sessionCount', String(v.sessions.filter(l => l.connectionState === 'connected').length));
  text('lifetimeUsage', `Connected time ${quantity(v.lifetimeSeconds, 'seconds')} · Download ${quantity(v.lifetimeDownload, 'download')} · Upload ${quantity(v.lifetimeUpload, 'upload')}`);
  if (v.pendingAccounting > 0) notice(accountingStatusMessage(v));
  else if (v.decision.reasons.length) notice(v.decision.reasons.join(' · '));
  else if (wasPending) notice('Accounting reconciled. The displayed balances are updated.');
}
function closeSocket(): void { clearTimeout(reconnectTimer); const previous = socket; socket = null; previous?.close(); }
function signedOut(message: string): void {
  signedIn = false; authEpoch++; csrf = ''; current = null; connectBusy = false; closeSocket(); clearConnection();
  node('portalAccount').hidden = true; node('portalLogin').hidden = false;
  for (const id of ['subscriberLimits', 'subscriberSessions', 'speedButtons', 'lifetimeUsage', 'liveClock']) node(id).replaceChildren();
  text('connectionState', 'Portal signed out'); text('portalLoginHelp', 'Sign in to view your account. Internet connection is a separate action.'); notice(message); node('subscriberUsername').focus();
}
async function ownedConnection(): Promise<void> {
  if (!flowId) { renderConnection(); return; }
  const expected = flowId;
  try {
    const status = await api<SubscriberConnectionStatus>(`/api/connection?flow=${encodeURIComponent(expected)}`);
    if (flowId !== expected || !signedIn) return;
    if (status.flowId !== expected) throw new Error('Connection request could not be verified.');
    connection = status; saveReference(); renderConnection();
  } catch (e) {
    if (e instanceof PortalError && e.status === 401) throw e;
    clearConnection(); notice('This connection request is unavailable for this device. Select Connect to internet to try again.');
  }
}
async function reconnect(): Promise<void> {
  if (!signedIn) return;
  try { await load(); }
  catch (e) {
    if (e instanceof PortalError && e.status === 401) { signedOut('Your portal session has expired. Sign in again.'); return; }
    if (signedIn) openSocket();
  }
}
function openSocket(): void {
  closeSocket();
  const next = new WebSocket(location.origin.replace(/^http/, 'ws') + '/ws' + (flowId ? `?flow=${encodeURIComponent(flowId)}` : ''));
  socket = next;
  next.onopen = () => { if (socket === next) text('connectionState', 'Portal signed in · Live updates connected'); };
  next.onmessage = event => {
    if (socket !== next || !signedIn) return;
    try {
      const frame = JSON.parse(event.data) as { event: 'subscriber:snapshot'; data: SubscriberSnapshot } | { event: 'subscriber:connection'; data: SubscriberConnectionStatus };
      if (frame.event === 'subscriber:snapshot') render(frame.data);
      else if (frame.event === 'subscriber:connection' && frame.data.flowId === flowId) { connection = frame.data; renderConnection(); }
    } catch { notice('A portal update could not be read. Last confirmed values are retained.'); }
  };
  next.onclose = () => {
    if (socket !== next) return;
    socket = null; text('connectionState', 'Portal signed in · Live updates disconnected'); renderConnection();
    if (signedIn) reconnectTimer = setTimeout(() => { void reconnect(); }, 3000);
  };
}
async function load(): Promise<void> {
  const epoch = authEpoch;
  const session = await api<{ csrf: string; networks: string[]; mode: string }>('/api/session');
  if (epoch !== authEpoch) return;
  csrf = session.csrf; mode = session.mode; signedIn = true;
  node('subscriberNetwork').innerHTML = [...new Set(session.networks)].map(n => `<option value="${esc(n)}">${esc(n)}</option>`).join('');
  const select = node('subscriberNetwork'); if (select instanceof HTMLSelectElement && session.networks.includes(connectIntent)) select.value = connectIntent;
  node('portalLogin').hidden = true; node('portalAccount').hidden = false; text('connectionState', 'Portal signed in · Connecting live updates');
  try { const snapshot = await api<SubscriberSnapshot>('/api/account'); if (epoch === authEpoch) render(snapshot); }
  catch (e) { notice(e instanceof Error ? e.message : 'Account is not ready'); }
  if (epoch !== authEpoch) return;
  await ownedConnection(); if (epoch !== authEpoch || !signedIn) return; openSocket();
  if (entry.failed) notice('HotSpot sign-in failed. Your portal account remains available. Select Connect to internet to try again.');
}
async function connectInternet(): Promise<void> {
  if (connectBusy || !signedIn || connectionView(connection, Date.now()).connected) return;
  const epoch = authEpoch;
  connectBusy = true; renderConnection();
  let submitted = false;
  try {
    const ticket = await api<{ loginUrl: string; username: string; password: string; destination: string; flowId: string }>('/api/connect', { network: value('subscriberNetwork') });
    if (!signedIn || epoch !== authEpoch) return;
    const login = new URL(ticket.loginUrl), destination = new URL(ticket.destination);
    if (login.protocol !== 'https:' || login.pathname !== '/login' || login.username || login.password || login.search || login.hash || !connectionReference(ticket.flowId) || destination.origin !== location.origin || destination.pathname !== '/' || destination.hash || destination.username || destination.password || destination.searchParams.size !== 1 || destination.searchParams.get('connection') !== ticket.flowId) throw new Error('The secure HotSpot destination could not be verified.');
    flowId = ticket.flowId; connection = null; saveReference(); closeSocket();
    const form = document.createElement('form'); form.method = 'POST'; form.action = login.href;
    for (const [name, v] of Object.entries({ username: ticket.username, password: ticket.password, dst: ticket.destination, popup: 'false' })) {
      const input = document.createElement('input'); input.type = 'hidden'; input.name = name; input.value = v; form.append(input);
    }
    document.body.append(form); notice('Opening secure HotSpot sign-in. Internet access will be confirmed by the router.');
    form.submit(); submitted = true;
    setTimeout(() => {
      form.remove(); connectBusy = false; renderConnection();
      if (signedIn) { openSocket(); notice('HotSpot has not returned to your account. You can request a new connection if sign-in failed.'); }
    }, 5000);
  } catch (e) { notice(e instanceof Error ? e.message : 'Internet connection failed'); }
  finally { if (!submitted && epoch === authEpoch) { connectBusy = false; renderConnection(); } }
}
text('portalLoginSubmit', connectIntent && !entry.failed ? 'Sign in and connect' : 'Sign in');
text('portalLoginHelp', connectIntent && !entry.failed ? `Sign in once to view your account and request Internet on ${connectIntent}. All subscription limits remain in effect.` : 'Sign in to view your account. Internet connection is a separate action.');
node('subscriberLoginForm').addEventListener('submit', async e => {
  e.preventDefault(); if (loginBusy) return;
  loginBusy = true; const button = node('portalLoginSubmit'); if (button instanceof HTMLButtonElement) button.disabled = true;
  node('subscriberLoginForm').setAttribute('aria-busy', 'true');
  try {
    await api('/api/login', { username: value('subscriberUsername'), password: value('subscriberPassword') });
    const password = node('subscriberPassword'); if (password instanceof HTMLInputElement) password.value = '';
    authEpoch++; clearConnection(); await load();
    const intent = connectIntent; connectIntent = ''; text('portalLoginSubmit', 'Sign in'); text('portalLoginHelp', 'Sign in to view your account. Internet connection is a separate action.');
    if (intent && !entry.failed && value('subscriberNetwork') === intent && (mode === 'pilot' || mode === 'managed')) await connectInternet();
    else if (intent && mode !== 'pilot' && mode !== 'managed') notice('Signed in to your account. Internet connection is not enabled in shadow mode.');
  } catch (e) { notice(e instanceof Error ? e.message : 'Sign-in failed'); }
  finally { loginBusy = false; if (button instanceof HTMLButtonElement) button.disabled = false; node('subscriberLoginForm').removeAttribute('aria-busy'); }
});
node('portalLogout').addEventListener('click', async () => {
  authEpoch++; signedIn = false; closeSocket();
  try { await api('/api/logout', {}); signedOut('Signed out of portal. Internet sessions are unchanged.'); }
  catch (e) { signedIn = true; notice(e instanceof Error ? e.message : 'Sign-out failed'); void reconnect(); }
});
node('speedButtons').addEventListener('click', async e => { const b = e.target; if (!(b instanceof HTMLButtonElement) || !b.dataset.speed) return; const epoch = authEpoch; b.disabled = true; try { const result = await api<{ status: string }>('/api/speed', { profileId: b.dataset.speed }); const snapshot = await api<SubscriberSnapshot>('/api/account'); if (epoch !== authEpoch || !signedIn) return; render(snapshot); notice(result.status === 'pending-reconnect' ? 'Speed selected. Reconnect a device to request this speed; your balance is preserved.' : 'Speed requested. Applying to active devices; your balance is preserved.'); } catch (e) { notice(e instanceof Error ? e.message : 'Speed request failed'); } finally { b.disabled = false; } });
node('subscriberSessions').addEventListener('click', async e => { const b = e.target; if (!(b instanceof HTMLButtonElement) || !b.dataset.disconnect) return; if (!confirm('Disconnect this device from the internet? Your balance and portal sign-in will be preserved.')) return; b.disabled = true; try { await api('/api/disconnect', { leaseId: b.dataset.disconnect, confirm: true }); notice('Router acknowledged the request. Waiting for final accounting.'); } catch (e) { notice(e instanceof Error ? e.message : 'Disconnect failed'); } finally { b.disabled = false; } });
node('internetConnect').addEventListener('click', () => { void connectInternet(); });
node('portalTheme').addEventListener('change', () => { document.documentElement.dataset.subscriberTheme = value('portalTheme'); });
setInterval(() => {
  renderConnection();
  if (current) {
    const active = current.sessions.filter(s => s.connectionState === 'connected');
    const recorded = active.reduce((sum, s) => sum + BigInt(s.seconds), 0n);
    const estimate = recorded + BigInt(boundedElapsed(current, Date.now())) * BigInt(active.length);
    text('liveClock', `Connected time on active devices: ${quantity(estimate.toString(), 'seconds')} (estimated for up to 5 seconds after an update).`);
  }
}, 1000);
renderConnection();
void load().catch(() => { if (!signedIn) node('portalLogin').hidden = false; });
window.addEventListener('pageshow', e => { if (e.persisted) void load().catch(() => signedOut('Sign in to your account.')); });
