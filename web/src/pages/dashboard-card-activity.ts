// Optional Activity cards share one SQLite query, regardless of how many are
// visible. Layout and page visibility control demand; no RouterOS collector is
// woken and no browser polling timer is created.
import { el, esc } from '../dom';
import type { Socket } from '../socket';
import type { ActivityResult } from '../gen/payloads';
import { fmtTs } from '../timefmt';
import { donutMarkup, rankMarkup } from './activity';

const CONTENT_IDS = {
  overview: 'dc-activity-overview', timeline: 'dc-activity-timeline',
  services: 'dc-activity-services', users: 'dc-activity-users',
  categories: 'dc-activity-categories', countries: 'dc-activity-countries',
} as const;
type CardKind = keyof typeof CONTENT_IDS;
export const ACTIVITY_CARDS = Object.keys(CONTENT_IDS) as CardKind[];
const cardId = (kind: string): string => 'dc-card-activity-' + kind;
const contentId = (kind: CardKind): string => CONTENT_IDS[kind];

export function activityCardURL(routerId: string, filter?: string, value?: string): string {
  const query = new URLSearchParams({ routerId, days: '1' });
  if (filter && value) {
    query.set('tab', 'events'); query.set(filter, value);
    if (filter === 'country') query.set('kind', 'firewall');
  }
  return '/activity?' + query;
}

export function renderActivityCards(data: ActivityResult): void {
  const n = (v: number): string => v.toLocaleString();
  const set = (kind: CardKind, html: string): void => { const node = el(contentId(kind)); if (node) node.innerHTML = html; };
  const summary = [[data.totals.dns, 'DNSUSER events'], [data.totals.domains, 'Unique domains'], [data.totals.users, 'Known users'], [data.totals.unmapped, 'Unmapped sources']];
  set('overview', '<div class="dc-activity-metrics">' + summary.map(([value, label]) => `<div><strong>${n(Number(value))}</strong><span>${label}</span></div>`).join('') + '</div><p class="activity-note">Recorded DNS events, not confirmed visits.</p>');
  set('services', rankMarkup(data.services.slice(0, 5), 'service'));
  set('users', rankMarkup(data.clients.slice(0, 5), 'client'));
  set('categories', donutMarkup(data.categories, 'category', 'Activity categories'));
  const countries = data.countries.map(row => {
    let label = row.key;
    try { if (label !== 'Unknown') label = new Intl.DisplayNames(['en'], { type: 'region' }).of(label) || label; } catch { /* retain the code */ }
    return { ...row, label };
  });
  set('countries', donutMarkup(countries, 'country', 'Activity destination countries') + `<p class="activity-note">${data.geoAvailable ? 'Approximate country from Firewall destination IPs. DNS-only domains have no country evidence.' : 'Local GeoIP unavailable. Destination countries remain Unknown.'}</p>`);
  const max = Math.max(1, ...data.series.map(point => point.count));
  set('timeline', data.totals.dns ? '<div class="activity-timeline">' + data.series.map((point, i) => {
    const from = Math.max(point.at, data.filter.from), to = data.series[i + 1]?.at || data.filter.to;
    const href = activityCardURL(data.routerId) + '&tab=events&from=' + from + '&to=' + to;
    const label = fmtTs(point.at) + ': ' + n(point.count) + ' DNSUSER events';
    return `<a class="dc-activity-bucket" href="${esc(href)}" style="--activity-height:${point.count / max * 100}%" aria-label="${esc(label)}" title="${esc(label)}"><span></span></a>`;
  }).join('') + '</div><div class="dc-activity-axis"><span>' + esc(fmtTs(data.filter.from)) + '</span><span>' + esc(fmtTs(data.filter.to)) + '</span></div>' : '<p class="activity-empty">No DNSUSER events in the last 24 hours.</p>');
  for (const kind of ACTIVITY_CARDS) {
    const card = el(cardId(kind)); if (!card) continue;
    const state = card.querySelector<HTMLElement>('.dc-activity-status');
    if (state) {
      state.textContent = data.status.error ? 'Archive sync needs attention · ' + data.status.error : !data.status.enabled ? 'Archive sync disabled · showing retained events' : (data.status.syncing ? 'Synchronizing archive' : 'Last sync: ' + fmtTs(data.status.lastSync));
      state.classList.toggle('dc-activity-attention', !!data.status.error || !data.status.enabled);
    }
    const link = card.querySelector<HTMLAnchorElement>('.dc-activity-open');
    if (link) link.href = activityCardURL(data.routerId);
  }
}

export function initActivityCards(socket: Socket): void {
  const root = el('dash-grid-root'); if (!root) return;
  let routerId = '', generation = 0, dirty = true;
  let data: ActivityResult | null = null;
  let controller: AbortController | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const wanted = (): boolean => !document.hidden && !!el('page-dashboard')?.classList.contains('active') && ACTIVITY_CARDS.some(kind => {
    const card = el(cardId(kind)); return !!card && card.style.display !== 'none';
  });
  function message(text: string): void {
    for (const kind of ACTIVITY_CARDS) {
      const node = el(contentId(kind));
      if (node) { node.textContent = text; node.classList.add('dc-activity-content'); }
      const status = el(cardId(kind))?.querySelector<HTMLElement>('.dc-activity-status');
      if (status) { status.textContent = ''; status.classList.remove('dc-activity-attention'); }
      const link = el(cardId(kind))?.querySelector<HTMLAnchorElement>('.dc-activity-open');
      if (link) link.href = activityCardURL(routerId);
    }
  }
  function cancel(): void {
    generation++; controller?.abort(); controller = undefined;
    if (timer !== undefined) clearTimeout(timer); timer = undefined;
    for (const kind of ACTIVITY_CARDS) el(cardId(kind))?.removeAttribute('aria-busy');
  }
  function schedule(delay = 0): void {
    if (!wanted() || !routerId || timer !== undefined || controller) return;
    timer = setTimeout(() => { timer = undefined; void load(); }, delay);
  }
  async function load(): Promise<void> {
    if (!wanted() || !routerId || controller) return;
    dirty = false; const current = ++generation, id = routerId;
    controller = new AbortController();
    for (const kind of ACTIVITY_CARDS) el(cardId(kind))?.setAttribute('aria-busy', 'true');
    try {
      const response = await fetch('/api/activity/dashboard?routerId=' + encodeURIComponent(id), { credentials: 'same-origin', signal: controller.signal });
      if (!response.ok) throw new Error(response.status === 403 ? 'Activity is unavailable for this router or your permissions.' : 'Activity could not be loaded. Open Activity to check the source.');
      const result: ActivityResult = await response.json();
      if (current !== generation) return;
      if (result.routerId !== id) throw new Error('Activity router changed. Select the router again.');
      data = result; renderActivityCards(result);
    } catch (error) {
      if (current === generation) { data = null; message(error instanceof Error ? error.message : 'Activity could not be loaded.'); }
    } finally {
      if (current === generation) {
        controller = undefined;
        for (const kind of ACTIVITY_CARDS) el(cardId(kind))?.removeAttribute('aria-busy');
        if (dirty) schedule(500);
      }
    }
  }
  function sync(): void {
    if (!wanted()) { cancel(); dirty = true; return; }
    if (data) renderActivityCards(data);
    if (dirty) schedule();
  }
  root.addEventListener('click', event => {
    const button = event.target instanceof Element ? event.target.closest<HTMLElement>('[data-filter]') : null;
    if (!button?.closest('.dc-activity-card') || !data || button.hasAttribute('disabled')) return;
    window.location.assign(activityCardURL(routerId, button.dataset.filter, button.dataset.value));
  });
  document.addEventListener('dashboard:layout', sync);
  document.addEventListener('mikrodash:pagechange', sync);
  document.addEventListener('visibilitychange', sync);
  socket.on('router:active', d => {
    cancel(); routerId = d.activeId || ''; data = null; dirty = true;
    message(routerId ? 'Loading Activity…' : 'Select a router to view Activity.'); sync();
  });
  socket.on('activity:changed', d => { if (d.routerId === routerId) { dirty = true; schedule(500); } });
  socket.on('perms:changed', () => { cancel(); data = null; dirty = true; message('Checking Activity access…'); sync(); });
  socket.on('disconnect', () => { cancel(); data = null; dirty = true; message('Live updates disconnected. Reconnecting…'); });
  socket.on('connect', () => { dirty = true; sync(); });
  message('Select a router to view Activity.');
}
