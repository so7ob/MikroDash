import { subscriberText, type SubscriberLanguage } from './subscriber-ar';
import { esc, pill } from './dom';
import { fmtTs } from './timefmt';
import type { SubscriberSnapshot, SubscriberLimitView } from './gen/payloads';

export function accountingStatusMessage(v: Pick<SubscriberSnapshot, 'pendingAccounting'>, language: SubscriberLanguage = 'en'): string {
  if (language === 'ar') return v.pendingAccounting > 0 ? `يلزم تسوية الاستهلاك (${v.pendingAccounting} قيد المراجعة). نعرض آخر رصيد مؤكد. الاتصالات الجديدة موقوفة حتى اكتمال التسوية.` : '';
  return v.pendingAccounting > 0 ? `Accounting reconciliation required (${v.pendingAccounting} pending). Last confirmed balances are shown. New Internet connections are blocked until the counters are settled.` : '';
}

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
function instant(raw: string, language: SubscriberLanguage): string {
  if (raw.startsWith('0001-')) return subscriberText('Not started', language);
  return fmtTs(raw, true, 'Asia/Aden') + ' UTC+03:00';
}
export function limitCard(v: SubscriberLimitView, language: SubscriberLanguage = 'en'): string {
  const t = (text: string) => subscriberText(text, language);
  const l = v.limit, amount = l.state === 'limited' ? quantity(l.amount, l.kind) : t(l.state);
  const remaining = l.state === 'limited' ? quantity((BigInt(l.amount) > BigInt(v.used) ? BigInt(l.amount) - BigInt(v.used) : 0n).toString(), l.kind) : t(l.state);
  const percent = percentage(v.used, l.amount);
  return `<section class="card"><div class="card-header"><h2 class="card-title">${esc(l.id)}</h2>${pill(t(l.kind), 'rx')}</div><div class="card-body">
    <dl class="subscriber-values"><dt>${t('Limit')}</dt><dd><bdi dir="auto">${esc(amount)}</bdi></dd><dt>${t('Confirmed usage')}</dt><dd><bdi dir="auto">${esc(quantity(v.used, l.kind))}</bdi></dd><dt>${t('Remaining')}</dt><dd><bdi dir="auto">${esc(remaining)}</bdi></dd>
    <dt>${t('Reserved for devices')}</dt><dd><bdi dir="auto">${esc(quantity((BigInt(v.reserved)-BigInt(v.provisional||'0')).toString(), l.kind))}</bdi></dd><dt>${t('Unresolved exposure')}</dt><dd><bdi dir="auto">${esc(quantity(v.provisional || '0',l.kind))}</bdi></dd><dt>${t('Available for new connections')}</dt><dd><bdi dir="auto">${esc(l.state === 'limited' ? quantity(v.remaining, l.kind) : t(l.state))}</bdi></dd><dt>${t('Period')}</dt><dd><bdi dir="auto">${esc(t(l.period.kind))}</bdi></dd>
    <dt>${t('Period starts')}</dt><dd><bdi dir="auto">${esc(instant(v.periodStart, language))}</bdi></dd><dt>${t('Next reset')}</dt><dd><bdi dir="auto">${l.period.kind === 'none' ? t('Does not reset') : esc(instant(v.nextReset, language))}</bdi></dd>
    <dt>${t('Data quality')}</dt><dd><bdi dir="auto">${pill(t(v.quality), v.quality === 'fresh' ? 'ok' : 'warn')}</bdi></dd></dl>
    <progress max="100" value="${percent}" aria-label="${esc(l.id)} ${t('usage')}">${percent}%</progress></div></section>`;
}
export function snapshotCards(v: SubscriberSnapshot, language: SubscriberLanguage = 'en'): string {
  return v.decision.limits.map(limit => limitCard(limit, language)).join('');
}
export function boundedElapsed(v: SubscriberSnapshot, now: number): number {
  if (v.quality !== 'fresh' || !v.sessions.some(s => s.connectionState === 'connected')) return 0;
  const elapsed = Math.max(0, Math.floor((now - Date.parse(v.observedAt)) / 1000));
  return Math.min(5, elapsed);
}
