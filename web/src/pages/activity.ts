import { el, esc, debounce, renderSortHeader, sortRows, protoPill, type SortState, type SortCol } from '../dom';
import { fmtTs, browserTimezone } from '../timefmt';
import { getDisplayTimezone } from '../caps';
import type { Socket } from '../socket';
import type { ActivityResult, ActivityRanking, ActivityRule, ActivityEvent } from '../gen/payloads';

interface Router { id: string; label?: string; name?: string; host?: string }
const API = '/api/activity/';
const views = ['summary', 'domains', 'clients', 'events', 'rules'];
const filterKeys = ['client', 'service', 'domain', 'country', 'dnsCountry', 'category', 'day', 'hour', 'canonical', 'deviceName', 'dnsIP', 'ipBasis', 'server', 'qtype', 'match'] as const;
const rangeKeys = ['days', 'from', 'to'] as const;
const n = (v: number): string => v.toLocaleString();
const pill = (v: string): string => `<span class="pill pill-rx"><bdi>${esc(v)}</bdi></span>`;
const cell = (v: string): string => `<bdi>${esc(v || '—')}</bdi>`;

export function rankMarkup(rows: ActivityRanking[], kind: string, selected?: string): string {
  if (!rows.length) return '<p class="activity-muted">No matching DNSUSER activity.</p>';
  const max = Math.max(1, ...rows.map(r => r.count));
  return rows.map((r, i) => `<button type="button" class="activity-rank" ${selected === undefined ? '' : `aria-pressed="${r.key === selected}"`} data-filter="${kind}" data-value="${esc(r.key)}"><span class="activity-rank-index">${i + 1}</span><span class="activity-rank-detail"><span class="activity-rank-label"><bdi>${esc(r.label)}</bdi>${kind === 'client' && !r.user ? '<span class="pill pill-warn">Unmapped</span>' : ''}</span><span class="activity-rank-meter" style="--activity-share:${Math.max(1, r.count / max * 100)}%"></span></span><strong>${n(r.count)}</strong></button>`).join('');
}

const tones = ['rx', 'tx', 'alt', 'warn', 'pink', 'ok', 'muted'];
export function donutMarkup(rows: ActivityRanking[], kind: string, label: string, denominator?: number, selected?: string, unit = 'events'): string {
  const total = denominator ?? rows.reduce((sum, r) => sum + r.count, 0);
  if (!total) return '<p class="activity-muted activity-empty">No matching events for this distribution.</p>';
  const shown = rows.slice(0, 6);
  const remainder=total-shown.reduce((sum,r)=>sum+r.count,0);
  if(remainder>0) shown.push({ ...rows[0]!, key: '', label: 'Other', count: remainder });
  let offset = 0;
  const segments = shown.map((r, i) => {
    const share = r.count / total * 100, start = offset; offset += share;
    if (!share) return '';
    const point = (percent: number): string => { const angle = percent / 100 * 2 * Math.PI; return `${21 + 15.9155 * Math.cos(angle)} ${21 + 15.9155 * Math.sin(angle)}`; };
    // Actual arcs keep hit areas on their own slice. Two half-arcs also draw 100%.
    const arc = `M ${point(start)} A 15.9155 15.9155 0 0 1 ${point(start + share / 2)} A 15.9155 15.9155 0 0 1 ${point(offset)}`;
    return `<path class="activity-donut-slice" d="${arc}" fill="none" stroke-width="6" ${r.key ? `data-filter="${kind}" data-value="${esc(r.key)}" data-selected="${r.key === selected}"` : ''} style="--activity-slice:var(--accent-${tones[i]})"><title>${esc(r.label)}: ${n(r.count)} (${share.toFixed(1)}%)</title></path>`;
  }).join('');
  return `<div class="activity-donut"><svg viewBox="0 0 42 42" role="img" aria-label="${esc(label)}: ${n(total)} ${esc(unit)}"><g transform="rotate(-90 21 21)">${segments}</g><text x="21" y="20.5" text-anchor="middle" class="activity-donut-total">${n(total)}</text><text x="21" y="25" text-anchor="middle" class="activity-donut-caption">${esc(unit)}</text></svg><div class="activity-donut-legend">${shown.map((r, i) => `<button type="button" class="activity-legend-item" ${r.key ? `data-filter="${kind}" data-value="${esc(r.key)}" ${selected === undefined ? '' : `aria-pressed="${r.key === selected}"`}` : 'disabled'}><span class="activity-legend-dot" style="--activity-slice:var(--accent-${tones[i]})"></span><bdi>${esc(r.label)}</bdi><strong>${(r.count / total * 100).toFixed(1)}%</strong><span>${n(r.count)}</span></button>`).join('')}</div></div>`;
}

const countryName = (cc: string): string => { if(cc === 'Unknown') return 'Unknown'; try { return new Intl.DisplayNames(['en'], {type:'region'}).of(cc) || cc; } catch { return cc; } };
const evidenceGroups: Record<string, string> = { dnsIP: 'DNS answer IP', deviceName: 'Device name', canonical: 'Canonical name', domain: 'Requested domain', server: 'Hotspot server' };
const evidenceButton = (kind: string, value: string): string => value ? `<button type="button" class="sbtn sbtn-ghost is-sm" data-filter="${kind}" data-value="${esc(value)}"><bdi>${esc(value)}</bdi></button>` : '—';
export function dnsEvidenceMarkup(e: ActivityEvent): string {
  if (e.kind !== 'dns') return '';
  return `<dl class="activity-dns-details"><div><dt>Device name</dt><dd>${evidenceButton('deviceName', e.deviceName)}</dd></div><div><dt>Canonical name</dt><dd>${evidenceButton('canonical', e.canonical)}</dd></div><div><dt>IP basis</dt><dd>${evidenceButton('ipBasis', e.ipBasis)}</dd></div><div><dt>DNS answers</dt><dd class="activity-dns-ips">${e.dnsAnswers.length ? e.dnsAnswers.map(a => `<span class="activity-dns-answer">${evidenceButton('dnsIP', a.ip)}<button type="button" class="sbtn sbtn-ghost is-sm" data-filter="dnsCountry" data-value="${esc(a.country)}" title="${esc([countryName(a.country), a.city].filter(Boolean).join(' · '))}">${cell([countryName(a.country), a.city].filter(Boolean).join(' · '))}</button></span>`).join('') : '<span class="activity-muted">Not recorded</span>'}</dd></div></dl>`;
}

async function json<T>(url: string, init?: RequestInit): Promise<T> {
  const r = await fetch(url, { credentials: 'same-origin', ...init });
  const body = await r.json();
  if (!r.ok || body?.ok === false) throw new Error(typeof body?.error === 'string' ? body.error : 'Activity request failed');
  return body;
}

export function initActivityPage(socket: Socket, visible: (page: string) => boolean, routers: () => Router[], activeId: () => string): void {
  const root = el('page-activity');
  if (!root) return;
  const input = (id: string): HTMLInputElement => el<HTMLInputElement>(id)!;
  const select = (id: string): HTMLSelectElement => el<HTMLSelectElement>(id)!;
  const showError = (message: string): void => { const e = el('activityError')!; e.textContent = message; e.hidden = !message; };
  let state = new URLSearchParams(location.search);
  let data: ActivityResult | null = null;
  let request = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let controller: AbortController | undefined;
  let busy = false;
  const rulesSort: SortState = { col: 'domain', dir: 'asc' };

  function saveURL(): void { history.replaceState(null, '', location.pathname + (state.size ? '?' + state.toString() : '')); }
  function view(): string { const v = state.get('tab') || 'summary'; return views.includes(v) ? v : 'summary'; }
  function params(): URLSearchParams {
    const p = new URLSearchParams(state); p.set('routerId', select('activityRouter').value); p.set('view', view());
    if (!p.has('sort')) p.set('sort', view() === 'events' ? 'last' : 'count');
    return p;
  }
  function countryBasis(): string { return state.get('countryBasis') === 'firewall' || (!state.has('countryBasis') && (state.has('country') || state.get('kind') === 'firewall')) ? 'firewall' : 'dns'; }
  function countryKey(): string { return countryBasis() === 'firewall' ? 'country' : 'dnsCountry'; }
  function clearFilter(key: string): void {
    state.delete(key);
    if (key === 'country' && state.has('countrySource')) {
      const source = state.get('countrySource');
      if (source) state.set('kind', source); else state.delete('kind');
      state.delete('countrySource');
    }
  }
  function update(key: string, value: string): void {
    if (key === 'country' && !value) clearFilter(key);
    if (key === 'kind') state.delete('countrySource');
    if (value) state.set(key, value); else state.delete(key);
    if (key !== 'page') state.delete('page');
    if (key === 'tab') { state.delete('sort'); state.delete('dir'); }
    saveURL(); void load();
  }
  function fillRouters(): void {
    const sel = select('activityRouter'); const current = state.get('routerId') || sel.value || activeId();
    sel.innerHTML = routers().map(r => `<option value="${esc(r.id)}">${esc(r.label || r.name || r.host || r.id)}</option>`).join('');
    if (routers().some(r => r.id === current)) sel.value = current;
    if (sel.value) state.set('routerId', sel.value);
  }
  function syncControls(): void {
    const v = view();
    root!.querySelectorAll<HTMLButtonElement>('[data-view]').forEach(b => { const on = b.dataset.view === v; b.classList.toggle('active', on); b.setAttribute('aria-selected', String(on)); b.tabIndex = on ? 0 : -1; });
    select('activityDays').value = state.has('from') ? 'custom' : state.get('days') || '1';
    el('activityCustomRange')!.hidden = select('activityDays').value !== 'custom';
    if (!el('activityCustomRange')!.contains(document.activeElement)) {
      input('activityFrom').value = state.has('from') ? fmtTs(Number(state.get('from')), false, '').replace(' ', 'T') : '';
      input('activityTo').value = state.has('to') ? fmtTs(Number(state.get('to')), false, '').replace(' ', 'T') : '';
    }
    input('activitySearch').value = state.get('q') || '';
    select('activityKind').value = state.get('kind') || '';
    select('activityCountry').value = state.get(countryKey()) || '';
    select('activityCountryBasis').value = countryBasis();
    select('activityGroup').value = state.get('group') || 'service';
    el('activityGroupWrap')!.hidden = v !== 'domains';
    select('activityEvidenceGroup').value = data?.filter.group && evidenceGroups[data.filter.group] ? data.filter.group : 'dnsIP';
    el('activityOverview')!.hidden = v !== 'summary';
    el('activityTablePane')!.hidden = !['domains', 'clients', 'events'].includes(v);
    el('activityRulesPane')!.hidden = v !== 'rules';
    el('activityFilters')!.innerHTML = filterKeys.filter(k => state.has(k)).map(k => `<button type="button" class="sbtn sbtn-outline is-sm" data-clear="${k}">${esc(k)}: ${esc(k === 'client' ? (data?.clients.find(c => c.key === state.get(k))?.label || 'Selected user / device') : state.get(k))} ×</button>`).join('') + (filterKeys.some(k => state.has(k)) ? '<button type="button" class="sbtn sbtn-ghost is-sm" data-clear="all">Clear drilldown</button>' : '');
    const csv = el<HTMLAnchorElement>('activityExport')!; csv.href = API + 'export?' + params();
  }
  function render(): void {
    if (!data) return; const d = data; syncControls();
    el('activityCount')!.textContent = n(d.totals.dns);
    el('activityMetrics')!.innerHTML = [[d.totals.dns, 'DNSUSER events'], [d.totals.domains, 'Unique domains'], [d.totals.users, 'Known users'], [d.totals.unmapped, 'Unmapped sources']].map(([value, label]) => `<div class="activity-metric"><span>${label}</span><strong>${n(Number(value))}</strong></div>`).join('');
    const s = d.status;
    el('activityState')!.textContent = `${s.enabled ? (s.error ? 'Sync needs attention' : s.syncing ? 'Synchronizing archive' : 'Archive sync enabled') : 'Archive sync disabled'} · ${s.files} files · Last sync: ${fmtTs(s.lastSync)} · Invalid DNSUSER: ${n(s.invalid)}${s.dropped ? ' · Lost: ' + n(s.dropped) : ''}${s.error ? ' · ' + s.error : ''}`;
    el('activityState')!.classList.toggle('activity-attention', !!s.error);
    if (!busy && !el('activityConfig')!.contains(document.activeElement)) {
      input('activityEnabled').checked = d.settings.enabled;
      input('activityFolder').value = d.settings.folder || 'usb1-part1/Log';
      input('activityTimezone').value = d.settings.timezone || getDisplayTimezone() || browserTimezone();
    }
    ['activityEnabled', 'activityFolder', 'activityTimezone', 'activitySaveSettings', 'activityRuleDomain', 'activityRuleService', 'activityRuleCategory', 'activityRuleSubdomains', 'activitySaveRule'].forEach(id => { (el<HTMLInputElement | HTMLButtonElement>(id))!.disabled = !d.canWrite || busy; });
    const cat = select('activityCategory'); const selected = state.get('category') || '';
    const cats = [...new Set([...d.rules.map(r => r.category), ...d.categories.map(c => c.key), selected].filter(Boolean))].sort();
    cat.innerHTML = '<option value="">All categories</option>' + cats.map(c => `<option value="${esc(c)}">${esc(c)}</option>`).join(''); cat.value = selected;
    el('activityServices')!.innerHTML = rankMarkup(d.services, 'service', state.get('service') || '');
    el('activityClients')!.innerHTML = rankMarkup(d.clients, 'client', state.get('client') || '');
    el('activityCategories')!.innerHTML = donutMarkup(d.categories, 'category', 'Category distribution', undefined, state.get('category') || '');
    el('activityServiceDonut')!.innerHTML = donutMarkup(d.services, 'service', 'Service distribution', d.totals.dns, state.get('service') || '');
    el('activityUserDonut')!.innerHTML = donutMarkup(d.clients, 'client', 'User distribution', d.totals.dns, state.get('client') || '');
    const dnsCountries = countryBasis() === 'dns';
    const countries = (dnsCountries ? d.dnsCountries : d.countries).map(r => ({...r, label:countryName(r.key)}));
    el('activityCountryDonut')!.innerHTML = donutMarkup(countries, countryKey(), dnsCountries ? 'DNS answer countries' : 'Firewall destination countries', undefined, state.get(countryKey()) || '', dnsCountries ? 'answers' : 'events');
    el('activityDNSMetrics')!.innerHTML = [[d.totals.answered, 'DNSUSER events with answers'], [d.totals.answerIPs, 'Unique answer addresses'], [d.totals.deviceNames, 'Recorded device names'], [d.totals.canonicals, 'Recorded canonical names']].map(([value, label]) => `<div class="activity-metric"><span>${label}</span><strong>${n(Number(value))}</strong></div>`).join('');
    el('activityQueryTypes')!.innerHTML = donutMarkup(d.queryTypes, 'qtype', 'DNS query types', d.totals.dns, state.get('qtype') || '');
    el('activityMatchMethods')!.innerHTML = donutMarkup(d.matchMethods, 'match', 'User matching methods', d.totals.dns, state.get('match') || '');
    el('activityIPBases')!.innerHTML = donutMarkup(d.ipBases, 'ipBasis', 'DNS address evidence', d.totals.dns, state.get('ipBasis') || '');
    el('activityGeoNote')!.textContent = (d.geoAvailable ? 'Approximate location from local GeoIP. ' : 'Local GeoIP database unavailable; recorded IPs remain Unknown. ') + (dnsCountries ? 'All recorded DNS answers in matching events; one event can contain several addresses. These are not confirmed connections.' : 'Counts Firewall events by their explicit destination IP.');
    el('activityGeoSetup')!.hidden = d.geoAvailable;
    const csel = select('activityCountry');
    csel.innerHTML = '<option value="">All countries</option>' + [...new Set([...countries.map(c=>c.key), state.get(countryKey()) || ''].filter(Boolean))].map(c=>`<option value="${esc(c)}">${esc(countryName(c))}</option>`).join('');
    csel.value = state.get(countryKey()) || '';
    const timeSelected = state.has('timeRange');
    // A selected six-hour bucket remains one selectable range after re-querying,
    // even though the API returns hourly points for its narrower window.
    const timeline = timeSelected ? [{ at: d.filter.from, count: d.totals.dns }] : d.series;
    const max = Math.max(1, ...timeline.map(p => p.count));
    el('activityTimeline')!.innerHTML = timeline.map((p, i) => {
      const label = `${fmtTs(p.at)}${timeSelected ? ' — ' + fmtTs(d.filter.to) : ''}: ${n(p.count)} events`;
      return `<button type="button" class="activity-timebar" data-bucket="${i}" aria-pressed="${timeSelected}" style="--activity-height:${Math.max(2, p.count / max * 100)}%" title="${esc(label)}" aria-label="${esc(label)}"><span></span></button>`;
    }).join('');
    const first = d.series[0], last = d.series[d.series.length - 1];
    el('activityCoverage')!.textContent = `${timeSelected ? 'Selected period: ' + fmtTs(d.filter.from, false) + ' — ' + fmtTs(d.filter.to, false) + ' · Click again to restore the previous period' : (first ? fmtTs(first.at, false) : '') + ' — ' + (last ? fmtTs(last.at, false) : '')} · ${d.coverage.length ? 'Recording coverage available' : 'Historical coverage is not verified'} · Imported data begins with the available archive.`;
    el('activityComparison')!.textContent = d.comparison ? `${d.previous ? ((d.totals.dns - d.previous) / d.previous * 100).toFixed(1) + '%' : String(d.totals.dns - d.previous)} vs previous period` : 'Comparison awaits two fully covered periods';
    const peak = Math.max(1, ...d.heatmap.map(h => h.count));
    const days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
    el('activityHeat')!.innerHTML = '<span></span>' + Array.from({ length: 24 }, (_, h) => `<span class="activity-hour">${h % 3 === 0 ? h : ''}</span>`).join('') + days.map((day, i) => `<span class="activity-day">${day}</span>` + d.heatmap.filter(h => h.day === i).map(h => `<span class="activity-heat-cell" role="img" style="--activity-intensity:${h.count ? Math.max(.15, h.count / peak) : 0}" title="${day} ${h.hour}:00: ${n(h.count)} events" aria-label="${day} ${h.hour}:00: ${n(h.count)} events"></span>`).join('')).join('');
    el('activityHeatZone')!.textContent = d.settings.timezone || 'UTC (router zone not set)';
    if (view() === 'summary') renderEvidenceTable(); else renderTable();
    renderRules();
  }
  function renderEvidenceTable(): void {
    if (!data) return;
    const d = data, kind = d.filter.group;
    const sort: SortState = { col: state.get('sort') || 'count', dir: state.get('dir') === 'asc' ? 'asc' : 'desc' };
    renderSortHeader('activityEvidenceHead', [{key:'label',label:evidenceGroups[kind] || 'Value'}, {key:'count',label:'Events'}, {key:'domains',label:'Domains'}, {key:'devices',label:'Devices'}, {key:'last',label:'Last recorded'}], sort, () => { state.set('sort', sort.col); state.set('dir', sort.dir); state.delete('page'); saveURL(); void load(); });
    keyboardSort('activityEvidenceHead');
    el('activityEvidenceBody')!.innerHTML = d.rows.map(r => `<tr><td><button type="button" class="sbtn sbtn-ghost is-sm" data-filter="${kind}" data-value="${esc(r.key)}" aria-pressed="${state.get(kind) === r.key}"><bdi>${esc(r.label)}</bdi></button>${kind === 'dnsIP' ? pill(r.key.includes(':') ? 'IPv6' : 'IPv4') : ''}</td><td class="activity-number">${n(r.count)}</td><td class="activity-number">${n(r.domains)}</td><td class="activity-number">${n(r.devices)}</td><td class="activity-mono">${esc(fmtTs(r.last))}</td></tr>`).join('') || '<tr><td colspan="5" class="activity-empty">No recorded values for this group. Older DNSUSER messages may not contain these fields.</td></tr>';
    const page = Number(state.get('page') || 1), pages = Math.max(1, Math.ceil(d.totalRows / d.filter.limit));
    el('activityEvidencePage')!.textContent = `${page} / ${pages} · ${n(d.totalRows)} values`;
    el<HTMLButtonElement>('activityEvidencePrev')!.disabled = page <= 1;
    el<HTMLButtonElement>('activityEvidenceNext')!.disabled = page >= pages;
  }
  function renderTable(): void {
    if (!data) return; const d = data; const v = view();
    const group=state.get('group')||'service';
    const groupTitle:Record<string,string>={service:'Service',domain:'Domain',country:'Destination country (Firewall)',dnsCountry:'DNS answer country',category:'Category',client:'User / device',day:'Day',hour:'Hour'};
    const sort: SortState = { col: state.get('sort') || (v === 'events' ? 'last' : 'count'), dir: state.get('dir') === 'asc' ? 'asc' : 'desc' };
    let cols: SortCol[];
    if (v === 'events') cols = [{ key: 'last', label: 'When' }, { key: 'kind', label: 'Source' }, { key: 'user', label: 'User / device' }, { key: 'label', label: 'Domain / destination' }, { label: 'Details' }, { key: 'count', label: 'Count' }];
    else cols = [{ key: 'label', label: v === 'clients' ? 'User / device' : groupTitle[group] || evidenceGroups[group] || ({ qtype: 'Query type', match: 'User matching method', ipBasis: 'Address evidence' } as Record<string, string>)[group] || 'Service' }, { key: 'category', label: v === 'clients' ? 'Identity' : 'Category' }, { key: 'count', label: group === 'dnsCountry' && v === 'domains' ? 'DNS answers' : 'Events' }, { key: 'domains', label: 'Domains' }, { key: 'devices', label: 'Devices' }, { key: 'last', label: 'Last activity' }];
    renderSortHeader('activityTableHead', cols, sort, () => { state.set('sort', sort.col); state.set('dir', sort.dir); state.delete('page'); saveURL(); void load(); });
    keyboardSort('activityTableHead');
    const rows = v === 'events' ? d.events.map(e => `<tr><td class="activity-mono">${esc(fmtTs(e.at))}${e.timeCertain ? '' : '<span class="pill pill-warn">Uncertain time</span>'}</td><td>${pill(e.kind === 'dns' ? 'DNSUSER' : 'Firewall')}</td><td><button type="button" class="sbtn sbtn-ghost" data-filter="client" data-value="${esc(e.client)}">${cell(e.user || e.mac || e.ip)}</button>${!e.user ? '<span class="pill pill-warn">Unmapped</span>' : ''}<div class="activity-muted">Server: ${cell(e.server)} · IP: ${cell(e.ip)} · MAC: ${cell(e.mac)}</div></td><td>${e.domain ? `<button type="button" class="sbtn sbtn-ghost" data-filter="domain" data-value="${esc(e.domain)}">${cell(e.domain)}</button><div>${cell(e.service)}</div>` : cell(e.destination)}</td><td>${e.kind === 'dns' ? `${pill(e.qtype || '?')} Match: ${cell(e.match)} ${pill(e.category)}${dnsEvidenceMarkup(e)}` : `${protoPill(e.protocol)} ${pill(e.country)} ${cell(e.city)} ${cell(e.in)} → ${cell(e.out)}<div class="activity-muted">Packet length: ${esc(e.length || '?')} bytes</div>`}</td><td class="activity-number">${n(e.count)}</td></tr>`) : d.rows.map(r => `<tr><td><button type="button" class="sbtn sbtn-ghost" data-filter="${v === 'clients' ? 'client' : group}" data-value="${esc(r.key)}">${cell(r.label)}</button>${v === 'clients' ? `<div class="activity-muted">${cell(r.server)} · ${cell(r.ip)}</div>` : ''}</td><td>${v === 'clients' ? (r.user ? pill('Named user') : '<span class="pill pill-warn">Unmapped</span>') : pill(r.category)}</td><td class="activity-number">${n(r.count)}</td><td class="activity-number">${n(r.domains)}</td><td class="activity-number">${n(r.devices)}</td><td class="activity-mono">${esc(fmtTs(r.last))}</td></tr>`);
    el('activityTableBody')!.innerHTML = rows.join('') || '<tr><td colspan="6" class="activity-empty">No matching activity. Enable archive sync or adjust the filters.</td></tr>';
    const page = Number(state.get('page') || 1); const pages = Math.max(1, Math.ceil(d.totalRows / d.filter.limit));
    el('activityPageLabel')!.textContent = `${page} / ${pages} · ${n(d.totalRows)} ${v === 'events' ? 'event groups' : 'rows'}`;
    el<HTMLButtonElement>('activityPrev')!.disabled = page <= 1;
    el<HTMLButtonElement>('activityNext')!.disabled = page >= pages;
  }
  function keyboardSort(id: string): void {
    el(id)!.querySelectorAll<HTMLElement>('.th-sort').forEach(th => { th.tabIndex = 0; th.setAttribute('role', 'button'); th.addEventListener('keydown', e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); th.click(); } }); });
  }
  function renderRules(): void {
    if (!data) return;
    renderSortHeader('activityRulesHead', [{ key: 'domain', label: 'Domain' }, { key: 'service', label: 'Service' }, { key: 'category', label: 'Category' }, { label: 'Match' }, { label: 'Origin' }, { label: 'Actions' }], rulesSort, renderRules); keyboardSort('activityRulesHead');
    el('activityRulesBody')!.innerHTML = sortRows(data.rules, rulesSort.col, rulesSort.dir).map(r => `<tr><td class="activity-mono">${cell(r.domain)}</td><td>${cell(r.service)}</td><td>${pill(r.category)}</td><td>${r.subdomains ? 'Domain + subdomains' : 'Exact domain'}</td><td>${pill(r.custom ? 'Local' : 'Built-in')}</td><td><button type="button" class="sbtn sbtn-outline" data-edit-rule="${esc(r.domain)}" data-custom="${r.custom}" ${data?.canWrite ? '' : 'disabled'}>Edit</button>${r.custom ? `<button type="button" class="sbtn sbtn-danger" data-remove-rule="${esc(r.domain)}" ${data?.canWrite ? '' : 'disabled'}>Remove override</button>` : ''}</td></tr>`).join('');
  }
  function clearData(): void {
    data=null;
    ['activityMetrics','activityServices','activityClients','activityCategories','activityServiceDonut','activityUserDonut','activityCountryDonut','activityTimeline','activityHeat','activityTableBody','activityRulesBody','activityDNSMetrics','activityQueryTypes','activityMatchMethods','activityIPBases','activityEvidenceBody','activityEvidencePage'].forEach(id=>{el(id)!.innerHTML='';});
    ['activityEnabled','activityFolder','activityTimezone','activitySaveSettings','activityRuleDomain','activityRuleService','activityRuleCategory','activityRuleSubdomains','activitySaveRule'].forEach(id=>{el<HTMLInputElement | HTMLButtonElement>(id)!.disabled=true;});
    el<HTMLButtonElement>('activityEvidencePrev')!.disabled = true; el<HTMLButtonElement>('activityEvidenceNext')!.disabled = true;
  }
  async function load(): Promise<void> {
    if (!visible('activity')) return;
    fillRouters(); if(data && data.routerId!==select('activityRouter').value)clearData(); syncControls();
    if (!select('activityRouter').value) { showError('No accessible router selected.'); return; }
    const current = ++request; controller?.abort(); controller = new AbortController();
    root!.setAttribute('aria-busy', 'true');
    try {
      const result = await json<ActivityResult>(API + 'query?' + params(), { signal: controller.signal });
      if (current !== request) return; data = result; showError(''); render();
    } catch (e) { if (current === request && !(e instanceof DOMException && e.name === 'AbortError')) { clearData();showError(e instanceof Error ? e.message : 'Activity could not be loaded'); } }
    finally { if (current === request) root!.setAttribute('aria-busy', 'false'); }
  }
  async function mutate(endpoint: string, method: string, body?: unknown): Promise<void> {
    if (busy || !data?.canWrite) return; busy = true; render();
    try { await json<{ ok: boolean }>(API + endpoint + (endpoint.includes('?') ? '&' : '?') + 'routerId=' + encodeURIComponent(select('activityRouter').value), { method, headers: { 'Content-Type': 'application/json' }, ...(body ? { body: JSON.stringify(body) } : {}) }); showError(''); await load(); }
    catch (e) { showError(e instanceof Error ? e.message : 'Change failed'); }
    finally { busy = false; if (data) render(); }
  }
  root.addEventListener('click', e => {
    const target = e.target instanceof Element ? e.target.closest<HTMLElement | SVGElement>('button, .activity-donut-slice[data-filter]') : null; if (!target) return;
    if (target.dataset.view) update('tab', target.dataset.view);
    if (target.dataset.filter) {
      const key = target.dataset.filter, value = target.dataset.value || '';
      const overview = !!target.closest('#activityOverview');
      if (overview && !e.ctrlKey && state.get(key) === value) clearFilter(key);
      else {
        if (key === 'country') {
          // Keep the prior source in the URL so deselection also works after reload.
          if (!state.has('countrySource')) state.set('countrySource', state.get('kind') || '');
          state.set('kind', 'firewall');
        }
        state.set(key, value);
      }
      if (e.ctrlKey || !overview) state.set('tab', 'events');
      state.delete('page'); state.delete('sort'); saveURL(); void load();
    }
    if (target.dataset.clear) { if (target.dataset.clear === 'all') filterKeys.forEach(clearFilter); else clearFilter(target.dataset.clear); state.delete('page'); saveURL(); void load(); }
    if (target.dataset.bucket && data) {
      const index = Number(target.dataset.bucket), p = data.series[index];
      if (p) {
        if (state.has('timeRange') && !e.ctrlKey) {
          const previous = new URLSearchParams(state.get('timeRange')!);
          rangeKeys.forEach(key => { const value = previous.get(key); if (value !== null) state.set(key, value); else state.delete(key); });
          state.delete('timeRange');
        } else if (!state.has('timeRange')) {
          const previous = new URLSearchParams();
          rangeKeys.forEach(key => { if (state.has(key)) previous.set(key, state.get(key)!); });
          state.set('timeRange', previous.toString());
          state.set('from', String(Math.max(p.at, data.filter.from)));
          state.set('to', String(data.series[index + 1]?.at || data.filter.to));
        }
        if (e.ctrlKey) state.set('tab', 'events');
        state.delete('page'); saveURL(); void load();
      }
    }
    if (target.dataset.editRule && data) { const r = data.rules.find(r => r.domain === target.dataset.editRule && r.custom === (target.dataset.custom === 'true')); if (r) { input('activityRuleDomain').value = r.domain; input('activityRuleService').value = r.service; input('activityRuleCategory').value = r.category; input('activityRuleSubdomains').checked = r.subdomains; input('activityRuleDomain').focus(); } }
    if (target.dataset.removeRule) void mutate('rules?domain=' + encodeURIComponent(target.dataset.removeRule), 'DELETE');
  });
  // Native keyboard clicks omit Ctrl in Chromium. Preserve it for chart
  // activation before the browser dispatches its unmodified click.
  root.addEventListener('keydown', e => {
    if (!e.ctrlKey || (e.key !== 'Enter' && e.key !== ' ')) return;
    const button = e.target instanceof Element ? e.target.closest<HTMLButtonElement>('#activityOverview button[data-filter], #activityOverview button[data-bucket]') : null;
    if (!button || button.disabled) return;
    e.preventDefault();
    button.dispatchEvent(new MouseEvent('click', { bubbles: true, ctrlKey: true }));
  });
  el('activityTabs')!.addEventListener('keydown', e => { if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return; e.preventDefault(); const i = views.indexOf(view()); const next = e.key === 'Home' ? 0 : e.key === 'End' ? views.length - 1 : (i + (e.key === 'ArrowRight' ? 1 : -1) + views.length) % views.length; update('tab', views[next]!); root.querySelector<HTMLButtonElement>(`[data-view="${views[next]}"]`)?.focus(); });
  select('activityRouter').addEventListener('change', () => update('routerId', select('activityRouter').value));
  select('activityDays').addEventListener('change', () => { const days = select('activityDays').value; el('activityCustomRange')!.hidden = days !== 'custom'; if (days !== 'custom') { state.delete('timeRange'); state.delete('from'); state.delete('to'); update('days', days); } });
  el('activityApplyRange')!.addEventListener('click', () => { const from = new Date(input('activityFrom').value).getTime(), to = new Date(input('activityTo').value).getTime(); if (!from || !to || to <= from || to - from > 30 * 86400000) { showError('Choose a valid range of up to 30 days.'); return; } state.delete('timeRange'); state.set('from', String(from)); update('to', String(to)); });
  input('activitySearch').addEventListener('input', debounce(() => update('q', input('activitySearch').value.trim()), 300));
  select('activityCountry').addEventListener('change', () => update(countryKey(), select('activityCountry').value));
  select('activityCountryBasis').addEventListener('change', () => { clearFilter('country'); clearFilter('dnsCountry'); update('countryBasis', select('activityCountryBasis').value); });
  select('activityCategory').addEventListener('change', () => update('category', select('activityCategory').value));
  select('activityKind').addEventListener('change', () => update('kind', select('activityKind').value));
  select('activityEvidenceGroup').addEventListener('change', () => update('evidenceGroup', select('activityEvidenceGroup').value));
  el('activityEvidencePrev')!.addEventListener('click', () => update('page', String(Math.max(1, Number(state.get('page') || 1) - 1))));
  el('activityEvidenceNext')!.addEventListener('click', () => update('page', String(Number(state.get('page') || 1) + 1)));
  select('activityGroup').addEventListener('change', () => update('group', select('activityGroup').value));
  el('activityRefresh')!.addEventListener('click', () => { void load(); });
  el('activityPrev')!.addEventListener('click', () => update('page', String(Math.max(1, Number(state.get('page') || 1) - 1))));
  el('activityNext')!.addEventListener('click', () => update('page', String(Number(state.get('page') || 1) + 1)));
  el('activitySaveSettings')!.addEventListener('click', () => { void mutate('settings', 'PUT', { enabled: input('activityEnabled').checked, folder: input('activityFolder').value.trim(), timezone: input('activityTimezone').value.trim() }); });
  el('activityRuleForm')!.addEventListener('submit', e => { e.preventDefault(); const rule: ActivityRule = { domain: input('activityRuleDomain').value, service: input('activityRuleService').value, category: input('activityRuleCategory').value, subdomains: input('activityRuleSubdomains').checked, custom: true }; void mutate('rules', 'PUT', rule); });
  document.addEventListener('mikrodash:pagechange', () => { if (visible('activity')) void load(); else { controller?.abort(); request++; if (timer) clearTimeout(timer); } });
  window.addEventListener('popstate', () => { state = new URLSearchParams(location.search); void load(); });
  socket.on('activity:changed', d => { if (!visible('activity') || d.routerId !== select('activityRouter').value || busy || timer) return; timer = setTimeout(() => { timer = undefined; void load(); }, 500); });
  socket.on('connect', () => { void load(); });
  socket.on('routers:update', () => { void load(); });
  socket.on('perms:changed', () => { clearData(); void load(); });
  if (visible('activity')) void load();
}
