import { esc, pill } from './dom';
import { fmtTs } from './timefmt';
import type { SubscriberSnapshot, SubscriberLimitView } from './gen/payloads';

export function quantity(value: string, kind: string): string {
  const n = BigInt(value);
  if (kind === 'seconds') {
    return [n / 3600n, n / 60n % 60n, n % 60n].map(v => v.toString().padStart(2, '0')).join(':');
  }
  for (const [unit, size] of [['TiB', 1099511627776n], ['GiB', 1073741824n], ['MiB', 1048576n], ['KiB', 1024n]] as const) {
    if (n >= size) return `${n / size}.${(n % size * 10n) / size} ${unit}`;
  }
  return `${n} B`;
}
export function percentage(used: string, limit: string): number {
  const total = BigInt(limit); if (total <= 0n) return 0;
  return Number((BigInt(used) * 100n / total) > 100n ? 100n : BigInt(used) * 100n / total);
}
function instant(raw: string): string {
  if (raw.startsWith('0001-')) return 'Not started';
  return fmtTs(raw, true, 'Asia/Aden') + ' UTC+03:00';
}
export function limitCard(v: SubscriberLimitView): string {
  const l = v.limit, amount = l.state === 'limited' ? quantity(l.amount, l.kind) : l.state;
  const remaining = l.state === 'limited' ? quantity((BigInt(l.amount) > BigInt(v.used) ? BigInt(l.amount) - BigInt(v.used) : 0n).toString(), l.kind) : l.state;
  const percent = percentage(v.used, l.amount);
  return `<section class="card"><div class="card-header"><h2 class="card-title">${esc(l.id)}</h2>${pill(l.kind, 'rx')}</div><div class="card-body">
    <dl class="subscriber-values"><dt>Limit</dt><dd>${esc(amount)}</dd><dt>Confirmed usage</dt><dd>${esc(quantity(v.used, l.kind))}</dd><dt>Remaining</dt><dd>${esc(remaining)}</dd>
    <dt>Reserved for devices</dt><dd>${esc(quantity((BigInt(v.reserved)-BigInt(v.provisional||'0')).toString(), l.kind))}</dd><dt>Unresolved exposure</dt><dd>${esc(quantity(v.provisional || '0',l.kind))}</dd><dt>Available for new connections</dt><dd>${esc(l.state === 'limited' ? quantity(v.remaining, l.kind) : l.state)}</dd><dt>Period</dt><dd>${esc(l.period.kind)}</dd>
    <dt>Period starts</dt><dd>${esc(instant(v.periodStart))}</dd><dt>Next reset</dt><dd>${l.period.kind === 'none' ? 'Does not reset' : esc(instant(v.nextReset))}</dd>
    <dt>Data quality</dt><dd>${pill(v.quality, v.quality === 'fresh' ? 'ok' : 'warn')}</dd></dl>
    <progress max="100" value="${percent}" aria-label="${esc(l.id)} usage">${percent}%</progress></div></section>`;
}
export function snapshotCards(v: SubscriberSnapshot): string {
  return v.decision.limits.map(limitCard).join('');
}
export function boundedElapsed(v: SubscriberSnapshot, now: number): number {
  if (v.quality !== 'fresh' || !v.sessions.some(s => s.connectionState === 'connected')) return 0;
  const elapsed = Math.max(0, Math.floor((now - Date.parse(v.observedAt)) / 1000));
  return Math.min(5, elapsed);
}
