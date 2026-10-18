import { esc, pill } from '../dom';
import { fmtTs } from '../timefmt';
import { boundedElapsed, quantity, snapshotCards } from '../subscriber-view';
import type { SubscriberSnapshot } from '../gen/payloads';

function node(id: string): HTMLElement { const v = document.getElementById(id); if (!v) throw new Error('Missing portal element'); return v; }
function value(id: string): string { const v = node(id); return v instanceof HTMLInputElement || v instanceof HTMLSelectElement ? v.value : ''; }
let csrf = '', current: SubscriberSnapshot | null = null, socket: WebSocket | null = null, signedIn = false;
function notice(message: string): void { node('portalNotice').textContent = message; }
async function api<T>(path: string, body?: unknown): Promise<T> {
  const response = await fetch(path, { method: body === undefined ? 'GET' : 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf }, body: body === undefined ? undefined : JSON.stringify(body) });
  const result = await response.json(); if (!response.ok) throw new Error(result.error || 'Request failed'); return result;
}
function render(v: SubscriberSnapshot): void {
  current = v;
  node('subscriptionTitle').textContent = v.subscription.plan.name;
  node('subscriptionSummary').textContent = `Subscription ${v.subscription.id} · ${v.subscription.status} · Expires ${v.subscription.expiresAt.startsWith('0001-') ? 'after first connection' : fmtTs(v.subscription.expiresAt, true, 'Asia/Aden')}`;
  node('subscriberLimits').innerHTML = snapshotCards(v);
  node('syncState').textContent = `Data: ${v.quality} · Observed ${v.observedAt} · Last accounting ${v.lastSyncedAt.startsWith('0001-') ? 'not received yet' : v.lastSyncedAt}`;
  node('speedState').textContent = `Selected: ${v.subscription.plan.speeds.find(p => p.id === v.subscription.speedId)?.name || v.subscription.speedId} · Application: ${v.subscription.speedState} · Evaluated ceiling: ${v.decision.down} bit/s download / ${v.decision.up} bit/s upload`;
  node('speedButtons').innerHTML = v.subscription.plan.speeds.map(p => `<button class="sbtn sbtn-outline" data-speed="${esc(p.id)}" aria-pressed="${p.id === v.subscription.speedId}">${esc(p.name)}</button>`).join('');
  node('subscriberSessions').innerHTML = v.sessions.map(l => `<div class="subscriber-session"><span>${esc(l.device)}</span>${pill(l.state, l.state === 'active' ? 'ok' : 'muted')}<span>${esc(l.network)} · ${quantity(l.seconds, 'seconds')}</span>${l.state === 'active' ? `<button class="sbtn sbtn-danger" data-disconnect="${esc(l.id)}">Disconnect device</button>` : ''}</div>`).join('') || 'No internet sessions yet.';
  node('sessionCount').textContent = String(v.sessions.filter(l => l.state === 'active').length);
  node('lifetimeUsage').textContent = `Connected time ${quantity(v.lifetimeSeconds, 'seconds')} · Download ${quantity(v.lifetimeDownload, 'download')} · Upload ${quantity(v.lifetimeUpload, 'upload')}`;
  if (v.decision.reasons.length) notice(v.decision.reasons.join(' · '));
}
function openSocket(): void {
  socket?.close(); socket = new WebSocket(location.origin.replace(/^http/, 'ws') + '/ws');
  socket.onopen = () => { node('connectionState').textContent = 'Portal updates connected'; };
  socket.onmessage = event => { const frame: { event: string; data: SubscriberSnapshot } = JSON.parse(event.data); if (frame.event === 'subscriber:snapshot') render(frame.data); };
  socket.onclose = () => { node('connectionState').textContent = 'Portal updates disconnected'; if (signedIn) setTimeout(openSocket, 3000); };
}
async function load(): Promise<void> {
  const session = await api<{csrf: string; networks: string[]; mode: string}>('/api/session'); csrf = session.csrf; signedIn = true;
  node('subscriberNetwork').innerHTML = session.networks.map(n => `<option value="${esc(n)}">${esc(n)}</option>`).join('');
  node('portalLogin').hidden = true; node('portalAccount').hidden = false;
  try { render(await api<SubscriberSnapshot>('/api/account')); } catch (e) { notice(e instanceof Error ? e.message : 'Account is not ready'); }
  openSocket();
}
node('subscriberLoginForm').addEventListener('submit', async e => { e.preventDefault(); try { await api('/api/login', {username:value('subscriberUsername'),password:value('subscriberPassword')}); const password = node('subscriberPassword'); if(password instanceof HTMLInputElement) password.value=''; await load(); } catch(e) { notice(e instanceof Error ? e.message : 'Sign-in failed'); } });
node('portalLogout').addEventListener('click', async () => { try { await api('/api/logout', {}); signedIn=false; socket?.close(); node('portalAccount').hidden=true; node('portalLogin').hidden=false; notice('Signed out of portal. Internet sessions are unchanged.'); }catch(e){notice(String(e));} });
node('speedButtons').addEventListener('click', async e => { const b=e.target; if (!(b instanceof HTMLButtonElement) || !b.dataset.speed) return; b.disabled=true;try{await api('/api/speed',{profileId:b.dataset.speed});render(await api<SubscriberSnapshot>('/api/account'));notice('Speed selected. Reconnect a device to request this speed; your balance is preserved.');}catch(e){notice(e instanceof Error ? e.message : 'Speed request failed');}finally{b.disabled=false;} });
node('subscriberSessions').addEventListener('click', async e => {const b=e.target;if(!(b instanceof HTMLButtonElement)||!b.dataset.disconnect)return;if(!confirm('Disconnect this device from the internet? Your balance and portal sign-in will be preserved.'))return;b.disabled=true;try{await api('/api/disconnect',{leaseId:b.dataset.disconnect,confirm:true});notice('Router acknowledged the request. Waiting for final accounting.');}catch(e){notice(e instanceof Error?e.message:'Disconnect failed');}finally{b.disabled=false;}});
node('internetConnect').addEventListener('click', async () => {try{const ticket=await api<{loginUrl:string;username:string;password:string;destination:string}>('/api/connect',{network:value('subscriberNetwork')});const form=document.createElement('form');form.method='POST';form.action=ticket.loginUrl;for(const [name,v] of Object.entries({username:ticket.username,password:ticket.password,dst:ticket.destination,popup:'false'})){const input=document.createElement('input');input.type='hidden';input.name=name;input.value=v;form.append(input);}document.body.append(form);form.submit();}catch(e){notice(e instanceof Error?e.message:'Internet connection failed');}});
node('portalTheme').addEventListener('change',()=>{document.documentElement.dataset.subscriberTheme=value('portalTheme');});
setInterval(()=>{if(current){const active=current.sessions.filter(s=>s.state==='active');const recorded=active.reduce((sum,s)=>sum+BigInt(s.seconds),0n);const estimate=recorded+BigInt(boundedElapsed(current,Date.now()))*BigInt(active.length);node('liveClock').textContent=`Connected time on active devices: ${quantity(estimate.toString(),'seconds')} (estimated for up to 5 seconds after an update).`;}},1000);
load().catch(()=>{node('portalLogin').hidden=false;});
