import { subscriberText } from '../subscriber-ar';
import { loginCredentials, type LoginMode } from '../subscriber-login';
import { initSubscriberQR } from '../subscriber-qr';
import { esc, pill } from '../dom';
import { fmtTs } from '../timefmt';
import { accountingStatusMessage, boundedElapsed, quantity, snapshotCards } from '../subscriber-view';
import { connectionReference, connectionReason, connectionView, portalEntry } from '../subscriber-connection';
import type { SubscriberConnectionStatus, SubscriberSnapshot } from '../gen/payloads';

function node(id: string): HTMLElement { const v = document.getElementById(id); if (!v) throw new Error('Missing portal element'); return v; }
function value(id: string): string { const v = node(id); return v instanceof HTMLInputElement || v instanceof HTMLSelectElement ? v.value : ''; }
function text(id: string, message: string): void { const target = node(id); if (target.textContent !== message) target.textContent = message; }
const lastHTML = new WeakMap<HTMLElement, string>();
function updateHTML(id: string, html: string): void {
  const target = node(id); if (lastHTML.get(target) === html) return;
  const focused = target.contains(document.activeElement) ? document.activeElement as HTMLButtonElement : null;
  const key = focused?.dataset.speed || focused?.dataset.disconnect;
  target.innerHTML = html; lastHTML.set(target, html);
  if (key) {
    const replacement = Array.from(target.querySelectorAll<HTMLButtonElement>('button')).find(button => (button.dataset.speed || button.dataset.disconnect) === key);
    (replacement || node('subscriptionTitle')).focus({ preventScroll: true });
  }
}
const entry = portalEntry(location.search);
const referenceKey = 'md_subscriber_connection';
let connectIntent = entry.network;
let csrf = '', mode = '', current: SubscriberSnapshot | null = null, socket: WebSocket | null = null, signedIn = false;
let flowId = entry.flowId, connection: SubscriberConnectionStatus | null = null, connectBusy = false, loginBusy = false, authEpoch = 0;
let speedBusy = false, logoutBusy = false;
const disconnectBusy = new Set<string>();
let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
try { if (!flowId) flowId = connectionReference(sessionStorage.getItem(referenceKey)); } catch { /* Storage is optional in captive browsers. */ }
if (location.search || location.hash) history.replaceState(null, '', location.pathname);
function saveReference(): void {
  try { if (flowId) sessionStorage.setItem(referenceKey, flowId); else sessionStorage.removeItem(referenceKey); } catch { /* The owned return URL also carries the reference. */ }
}
function clearConnection(): void { flowId = ''; connection = null; saveReference(); renderConnection(); }
function notice(message: string): void { text('portalNotice', subscriberText(message, 'ar')); }
class PortalError extends Error { constructor(readonly status: number, message: string) { super(message); } }
async function api<T>(path: string, body?: unknown): Promise<T> {
  const controller = new AbortController(), timeout = setTimeout(() => controller.abort(), 10000);
  try {
    const response = await fetch(path, { method: body === undefined ? 'GET' : 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf }, body: body === undefined ? undefined : JSON.stringify(body), signal: controller.signal });
    const result = await response.json().catch(() => null);
    if (!response.ok) throw new PortalError(response.status, typeof result?.error === 'string' ? result.error : 'تعذر إكمال الطلب.');
    if (result === null) throw new Error('تعذرت قراءة استجابة البوابة.');
    return result as T;
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') throw new Error('انتهت مهلة الطلب. تحقق من الاتصال وحاول مجدداً.');
    if (error instanceof TypeError) throw new Error('تعذر الوصول إلى البوابة. تحقق من الاتصال وحاول مجدداً.');
    throw error;
  } finally { clearTimeout(timeout); }
}
function renderConnection(): void {
  const view = connectionView(connection, Date.now(), 'ar');
  text('internetState', view.label); node('internetState').className = `pill pill-${view.tone}`;
  text('internetDetail', view.message);
  node('connectionObserved').hidden = connection === null;
  text('connectionObserved', connection && !connection.observedAt.startsWith('0001-') ? `آخر تأكيد من الراوتر: ${fmtTs(connection.observedAt, true, 'Asia/Aden')}` : 'لم يصل تأكيد الراوتر لهذا الطلب بعد.');
  const button = node('internetConnect'); if (button instanceof HTMLButtonElement) button.disabled = connectBusy || view.connected;
}
function render(v: SubscriberSnapshot): void {
  const wasPending = (current?.pendingAccounting || 0) > 0;
  current = v;
  text('subscriptionTitle', v.subscription.plan.name);
  text('subscriptionSummary', `حالة الاشتراك: ${subscriberText(v.subscription.status, 'ar')} · ينتهي: ${v.subscription.expiresAt.startsWith('0001-') ? 'بعد أول اتصال' : fmtTs(v.subscription.expiresAt, true, 'Asia/Aden')}`);
  updateHTML('subscriberLimits', snapshotCards(v, 'ar'));
  text('syncState', `حالة البيانات: ${subscriberText(v.quality, 'ar')} · آخر تحديث: ${fmtTs(v.observedAt, true, 'Asia/Aden')} · آخر استهلاك: ${v.lastSyncedAt.startsWith('0001-') ? 'لم تصل بعد' : fmtTs(v.lastSyncedAt, true, 'Asia/Aden')}`);
  text('speedState', `السرعة المختارة: ${v.subscription.plan.speeds.find(p => p.id === v.subscription.speedId)?.name || v.subscription.speedId} · التطبيق: ${subscriberText(v.subscription.speedState, 'ar')} · الحد المتاح: ${v.decision.down} بت/ث تنزيل / ${v.decision.up} بت/ث رفع`);
  updateHTML('speedButtons', v.subscription.plan.speeds.map(p => `<button class="sbtn sbtn-outline" data-speed="${esc(p.id)}" aria-pressed="${p.id === v.subscription.speedId}">${esc(p.name)}</button>`).join(''));
  updateHTML('subscriberSessions', v.sessions.map(l => `<div class="subscriber-session"><bdi>${esc(l.device)}</bdi>${pill(subscriberText(l.connectionState || 'unknown', 'ar'), l.connectionState === 'connected' ? 'ok' : 'muted')}<span class="session-detail"><bdi>${esc(l.network)}</bdi> · ${quantity(l.seconds, 'seconds')} · السرعة المطبقة: ${!l.rateState || l.rateState === 'unknown' ? 'غير معروفة' : `${esc(l.appliedDown)} بت/ث تنزيل / ${esc(l.appliedUp)} بت/ث رفع (${esc(subscriberText(l.rateState, 'ar'))})`}</span>${l.state === 'active' ? `<button class="sbtn sbtn-danger" data-disconnect="${esc(l.id)}">فصل الجهاز</button>` : ''}</div>`).join('') || 'لا توجد جلسات إنترنت حتى الآن.');
  syncBusyButtons();
  text('sessionCount', String(v.sessions.filter(l => l.connectionState === 'connected').length));
  updateHTML('lifetimeUsage', `مدة الاتصال <bdi dir="ltr">${quantity(v.lifetimeSeconds, 'seconds')}</bdi> · تنزيل <bdi dir="ltr">${quantity(v.lifetimeDownload, 'download')}</bdi> · رفع <bdi dir="ltr">${quantity(v.lifetimeUpload, 'upload')}</bdi>`);
  if (v.pendingAccounting > 0) notice(accountingStatusMessage(v, 'ar'));
  else if (v.decision.reasons.length) notice(v.decision.reasons.map(reason => connectionReason(reason, 'ar')).join(' · '));
  else if (wasPending) notice('اكتملت تسوية الاستهلاك. تم تحديث الأرصدة.');
}
function closeSocket(): void { clearTimeout(reconnectTimer); const previous = socket; socket = null; previous?.close(); }
function signedOut(message: string): void {
  qr.close(); signedIn = false; authEpoch++; csrf = ''; current = null; connectBusy = false; closeSocket(); clearConnection();
  node('portalAccount').hidden = true; node('portalLogin').hidden = false;
  for (const id of ['subscriberLimits', 'subscriberSessions', 'speedButtons', 'lifetimeUsage', 'liveClock']) { node(id).replaceChildren(); lastHTML.delete(node(id)); }
  text('connectionState', 'لم تسجّل الدخول'); text('portalLoginHelp', 'سجّل الدخول لمتابعة حسابك. الاتصال بالإنترنت إجراء مستقل.'); notice(message); node('subscriberUsername').focus();
}
async function ownedConnection(): Promise<void> {
  if (!flowId) { renderConnection(); return; }
  const expected = flowId;
  try {
    const status = await api<SubscriberConnectionStatus>(`/api/connection?flow=${encodeURIComponent(expected)}`);
    if (flowId !== expected || !signedIn) return;
    if (status.flowId !== expected) throw new Error('تعذر التحقق من طلب الاتصال.');
    connection = status; saveReference(); renderConnection();
  } catch (e) {
    if (e instanceof PortalError && e.status === 401) throw e;
    clearConnection(); notice('طلب الاتصال غير متاح لهذا الجهاز. اضغط الاتصال بالإنترنت للمحاولة مجدداً.');
  }
}
async function reconnect(): Promise<void> {
  if (!signedIn) return;
  try { await load(); }
  catch (e) {
    if (e instanceof PortalError && e.status === 401) { signedOut('انتهت جلسة البوابة. سجّل الدخول مجدداً.'); return; }
    if (signedIn) openSocket();
  }
}
function openSocket(): void {
  closeSocket();
  const next = new WebSocket(location.origin.replace(/^http/, 'ws') + '/ws' + (flowId ? `?flow=${encodeURIComponent(flowId)}` : ''));
  socket = next;
  next.onopen = () => { if (socket === next) text('connectionState', 'مسجّل الدخول · تحديث مباشر'); };
  next.onmessage = event => {
    if (socket !== next || !signedIn) return;
    try {
      const frame = JSON.parse(event.data) as { event: 'subscriber:snapshot'; data: SubscriberSnapshot } | { event: 'subscriber:connection'; data: SubscriberConnectionStatus };
      if (frame.event === 'subscriber:snapshot') render(frame.data);
      else if (frame.event === 'subscriber:connection' && frame.data.flowId === flowId) { connection = frame.data; renderConnection(); }
    } catch { notice('تعذرت قراءة التحديث. نعرض آخر بيانات مؤكدة.'); }
  };
  next.onclose = () => {
    if (socket !== next) return;
    socket = null; text('connectionState', 'مسجّل الدخول · انقطع التحديث المباشر'); renderConnection();
    if (signedIn) reconnectTimer = setTimeout(() => { void reconnect(); }, 3000);
  };
}
async function load(): Promise<void> {
  const epoch = authEpoch;
  const session = await api<{ csrf: string; networks: string[]; mode: string }>('/api/session');
  if (epoch !== authEpoch) return;
  csrf = session.csrf; mode = session.mode; signedIn = true;
  const selectedNetwork = value('subscriberNetwork');
  node('subscriberNetwork').innerHTML = [...new Set(session.networks)].map(n => `<option value="${esc(n)}">${esc(n)}</option>`).join('');
  const select = node('subscriberNetwork'); if (select instanceof HTMLSelectElement) {
    if (session.networks.includes(selectedNetwork)) select.value = selectedNetwork;
    else if (session.networks.includes(connectIntent)) select.value = connectIntent;
  }
  node('portalLogin').hidden = true; node('portalAccount').hidden = false; text('connectionState', 'مسجّل الدخول · جارٍ تحميل التحديثات');
  try { const snapshot = await api<SubscriberSnapshot>('/api/account'); if (epoch === authEpoch) render(snapshot); }
  catch (e) { if (e instanceof PortalError && e.status === 401) throw e; notice(e instanceof Error ? e.message : 'بيانات الحساب غير جاهزة.'); }
  if (epoch !== authEpoch) return;
  await ownedConnection(); if (epoch !== authEpoch || !signedIn) return; openSocket();
  if (entry.failed) notice('تعذر دخول الشبكة. حسابك متاح؛ اضغط الاتصال بالإنترنت للمحاولة مجدداً.');
}
async function connectInternet(): Promise<void> {
  if (connectBusy || !signedIn || connectionView(connection, Date.now(), 'ar').connected) return;
  const epoch = authEpoch;
  connectBusy = true; renderConnection();
  let submitted = false;
  try {
    const ticket = await api<{ loginUrl: string; username: string; password: string; destination: string; flowId: string }>('/api/connect', { network: value('subscriberNetwork') });
    if (!signedIn || epoch !== authEpoch) return;
    const login = new URL(ticket.loginUrl), destination = new URL(ticket.destination);
    if (login.protocol !== 'https:' || login.pathname !== '/login' || login.username || login.password || login.search || login.hash || !connectionReference(ticket.flowId) || destination.origin !== location.origin || destination.pathname !== '/' || destination.hash || destination.username || destination.password || Array.from(destination.searchParams).length !== 1 || destination.searchParams.get('connection') !== ticket.flowId) throw new Error('تعذر التحقق من وجهة الدخول الآمنة.');
    flowId = ticket.flowId; connection = null; saveReference(); closeSocket();
    const form = document.createElement('form'); form.method = 'POST'; form.action = login.href;
    for (const [name, v] of Object.entries({ username: ticket.username, password: ticket.password, dst: ticket.destination, popup: 'false' })) {
      const input = document.createElement('input'); input.type = 'hidden'; input.name = name; input.value = v; form.append(input);
    }
    document.body.append(form); notice('جارٍ فتح الدخول الآمن للشبكة. سيؤكد الراوتر اتصالك بالإنترنت.');
    form.submit(); submitted = true;
    setTimeout(() => {
      form.remove(); connectBusy = false; renderConnection();
      if (signedIn) { openSocket(); notice('لم تعد الشبكة إلى حسابك. يمكنك طلب اتصال جديد إذا تعذر الدخول.'); }
    }, 5000);
  } catch (e) { actionError(e, 'تعذر الاتصال بالإنترنت.', epoch); }
  finally { if (!submitted && epoch === authEpoch) { connectBusy = false; renderConnection(); } }
}
let loginMode: LoginMode = 'voucher';
function setLoginMode(next: LoginMode, focus = true): void {
  loginMode = next;
  for (const [id, selected] of [['voucherMode', next === 'voucher'], ['accountMode', next === 'account']] as const) {
    node(id).classList.toggle('is-active', selected); node(id).setAttribute('aria-pressed', String(selected));
  }
  const username = node('subscriberUsername') as HTMLInputElement, password = node('subscriberPassword') as HTMLInputElement;
  node('subscriberPasswordField').hidden = next === 'voucher'; password.disabled = next === 'voucher'; password.required = next === 'account'; password.value = '';
  username.autocomplete = next === 'voucher' ? 'off' : 'username';
  username.placeholder = next === 'voucher' ? 'أدخل رمز القسيمة' : 'أدخل اسم المستخدم';
  text('subscriberUsernameLabel', next === 'voucher' ? 'رمز القسيمة' : 'اسم المستخدم');
  if (focus) username.focus();
}
node('voucherMode').addEventListener('click', () => setLoginMode('voucher'));
node('accountMode').addEventListener('click', () => setLoginMode('account'));
const qr = initSubscriberQR(code => { setLoginMode('voucher'); (node('subscriberUsername') as HTMLInputElement).value = code; notice('تمت قراءة القسيمة. راجع الرمز ثم سجّل الدخول.'); });
text('portalLoginSubmit', connectIntent && !entry.failed ? 'تسجيل الدخول والاتصال' : 'تسجيل الدخول');
text('portalLoginHelp', connectIntent && !entry.failed ? `سجّل الدخول لمتابعة حسابك وطلب الاتصال على ${connectIntent}. تبقى حدود اشتراكك سارية.` : 'سجّل الدخول لمتابعة حسابك. الاتصال بالإنترنت إجراء مستقل.');
node('subscriberLoginForm').addEventListener('submit', async e => {
  e.preventDefault(); if (loginBusy) return;
  loginBusy = true; const button = node('portalLoginSubmit'); if (button instanceof HTMLButtonElement) button.disabled = true;
  node('subscriberLoginForm').setAttribute('aria-busy', 'true');
  for (const id of ['voucherMode', 'accountMode', 'portalQR']) (node(id) as HTMLButtonElement).disabled = true;
  try {
    await api('/api/login', loginCredentials(loginMode, value('subscriberUsername'), value('subscriberPassword')));
    const password = node('subscriberPassword'); if (password instanceof HTMLInputElement) password.value = '';
    (node('subscriberUsername') as HTMLInputElement).value = ''; qr.close();
    authEpoch++; clearConnection(); await load(); node('subscriptionTitle').focus();
    const intent = connectIntent; connectIntent = ''; text('portalLoginSubmit', 'تسجيل الدخول'); text('portalLoginHelp', 'سجّل الدخول لمتابعة حسابك. الاتصال بالإنترنت إجراء مستقل.');
    if (intent && !entry.failed && value('subscriberNetwork') === intent && (mode === 'pilot' || mode === 'managed')) await connectInternet();
    else if (intent && mode !== 'pilot' && mode !== 'managed') notice('تم تسجيل الدخول. الاتصال بالإنترنت غير مفعّل من هذه البوابة حالياً.');
  } catch (e) { notice(e instanceof Error ? e.message : 'تعذر تسجيل الدخول.'); }
  finally { loginBusy = false; if (button instanceof HTMLButtonElement) button.disabled = false; node('subscriberLoginForm').removeAttribute('aria-busy'); for (const id of ['voucherMode', 'accountMode', 'portalQR']) (node(id) as HTMLButtonElement).disabled = false; }
});
node('portalLogout').addEventListener('click', async () => {
  if (logoutBusy) return; logoutBusy = true; (node('portalLogout') as HTMLButtonElement).disabled = true;
  authEpoch++; signedIn = false; closeSocket();
  try { await api('/api/logout', {}); signedOut('خرجت من البوابة. جلسات الإنترنت مستمرة.'); }
  catch (e) { signedIn = true; notice(e instanceof Error ? e.message : 'تعذر الخروج من البوابة.'); void reconnect(); }
  finally { logoutBusy = false; (node('portalLogout') as HTMLButtonElement).disabled = false; }
});
function syncBusyButtons(): void {
  node('speedButtons').setAttribute('aria-busy', String(speedBusy));
  for (const button of node('speedButtons').querySelectorAll<HTMLButtonElement>('button')) button.disabled = speedBusy;
  for (const button of node('subscriberSessions').querySelectorAll<HTMLButtonElement>('button')) button.disabled = disconnectBusy.has(button.dataset.disconnect || '');
}
function actionError(error: unknown, fallback: string, epoch: number): void {
  if (epoch !== authEpoch) return;
  if (error instanceof PortalError && error.status === 401) { signedOut('انتهت جلسة البوابة. سجّل الدخول مجدداً.'); return; }
  notice(error instanceof Error ? error.message : fallback);
}
node('speedButtons').addEventListener('click', async e => {
  const b = e.target; if (!(b instanceof HTMLButtonElement) || !b.dataset.speed || speedBusy) return;
  const epoch = authEpoch, profileId = b.dataset.speed; speedBusy = true; syncBusyButtons();
  try {
    const result = await api<{ status: string }>('/api/speed', { profileId });
    if (epoch !== authEpoch || !signedIn) return;
    const snapshot = await api<SubscriberSnapshot>('/api/account');
    if (epoch !== authEpoch || !signedIn) return;
    render(snapshot); notice(result.status === 'pending-reconnect' ? 'تم اختيار السرعة. أعد اتصال الجهاز لتطبيقها؛ رصيدك محفوظ.' : 'تم طلب السرعة. جارٍ تطبيقها على الأجهزة النشطة؛ رصيدك محفوظ.');
  } catch (e) { actionError(e, 'تعذر تغيير السرعة.', epoch); }
  finally {
    speedBusy = false; syncBusyButtons();
    if (epoch === authEpoch && signedIn && (document.activeElement === document.body || document.activeElement === b)) {
      Array.from(node('speedButtons').querySelectorAll<HTMLButtonElement>('button')).find(button => button.dataset.speed === profileId)?.focus({ preventScroll: true });
    }
  }
});
node('subscriberSessions').addEventListener('click', async e => {
  const b = e.target; if (!(b instanceof HTMLButtonElement) || !b.dataset.disconnect || disconnectBusy.has(b.dataset.disconnect)) return;
  if (!confirm('هل تريد فصل هذا الجهاز عن الإنترنت؟ سيبقى رصيدك ودخولك للبوابة محفوظين.')) return;
  const leaseId = b.dataset.disconnect, epoch = authEpoch; disconnectBusy.add(leaseId); syncBusyButtons();
  try { await api('/api/disconnect', { leaseId, confirm: true }); if (epoch === authEpoch && signedIn) notice('استلم الراوتر طلب الفصل. بانتظار تأكيد الاستهلاك النهائي.'); }
  catch (e) { actionError(e, 'تعذر فصل الجهاز.', epoch); }
  finally { disconnectBusy.delete(leaseId); syncBusyButtons(); }
});
node('internetConnect').addEventListener('click', () => { void connectInternet(); });
setInterval(() => {
  renderConnection();
  if (current) {
    const active = current.sessions.filter(s => s.connectionState === 'connected');
    const recorded = active.reduce((sum, s) => sum + BigInt(s.seconds), 0n);
    const estimate = recorded + BigInt(boundedElapsed(current, Date.now())) * BigInt(active.length);
    text('liveClock', `مدة الاتصال على الأجهزة النشطة: ${quantity(estimate.toString(), 'seconds')} (تقديرية لمدة تصل إلى 5 ثوانٍ بعد التحديث).`);
  }
}, 1000);
renderConnection();
const initialEpoch = authEpoch;
void load().catch(e => { if (initialEpoch !== authEpoch) return; if (e instanceof PortalError && e.status === 401 && signedIn) signedOut('انتهت جلسة البوابة. سجّل الدخول مجدداً.'); else if (!signedIn) node('portalLogin').hidden = false; });
window.addEventListener('pageshow', e => { if (e.persisted) void load().catch(() => signedOut('سجّل الدخول إلى حسابك.')); });
