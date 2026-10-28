import { el, esc } from '../dom';
import { validTimezone } from '../timefmt';
import type { SubscriberLimit } from '../gen/payloads';

export type LimitInput = {
  kind: string; amount: string; period: string; zone: string; anchor: string;
  action: string; down: string; up: string;
};

function quantity(value: string, positive = false): string {
  if (!/^\d+$/.test(value)) throw new Error('Use decimal integers for amounts and rates.');
  const n = BigInt(value);
  if (n > 9223372036854775807n || n < (positive ? 1n : 0n)) {
    throw new Error(positive ? 'Rate ceilings must be positive signed 64-bit integers.' : 'Amount exceeds the signed 64-bit range.');
  }
  return n.toString();
}

export function draftLimit(id: string, input: LimitInput): SubscriberLimit {
  if (!['seconds', 'download', 'upload', 'total'].includes(input.kind)
      || !['none', 'hourly', 'daily', 'weekly', 'monthly'].includes(input.period)
      || !['disconnect', 'deny', 'alert', 'throttle'].includes(input.action)) {
    throw new Error('Choose a supported limit, period and exhaustion action.');
  }
  if (!validTimezone(input.zone)) throw new Error('Enter a recognized IANA time zone, such as Asia/Aden or UTC.');
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(input.anchor)
      || !Number.isFinite(Date.parse(input.anchor))) throw new Error('Enter an RFC3339 period anchor with an explicit offset.');
  return {
    id, kind: input.kind, state: 'limited', amount: quantity(input.amount),
    period: { kind: input.period, zone: input.zone, anchor: input.anchor },
    action: input.action,
    down: input.action === 'throttle' ? quantity(input.down, true) : '0',
    up: input.action === 'throttle' ? quantity(input.up, true) : '0',
  };
}

// IDs are stable while editing and never reused after removal. Saved payloads
// are detached from later edits, including nested calendar period fields.
export class SubscriberLimitDraft {
  private limits: SubscriberLimit[] = [];
  private sequence = 0;
  save(input: LimitInput, id?: string): void {
    const index = id === undefined ? -1 : this.limits.findIndex(limit => limit.id === id);
    if (id !== undefined && index < 0) throw new Error('That draft limit no longer exists.');
    const limit = draftLimit(id ?? `limit-${this.sequence + 1}`, input);
    if (index < 0) { this.sequence++; this.limits.push(limit); }
    else this.limits[index] = limit;
  }
  remove(id: string): void { this.limits = this.limits.filter(limit => limit.id !== id); }
  snapshot(): SubscriberLimit[] { return structuredClone(this.limits); }
}

export function initSubscriberPlanLimits(say: (message: string) => void, busy: () => boolean): () => SubscriberLimit[] {
  const draft = new SubscriberLimitDraft();
  let editing: string | undefined;
  const fields = {
    kind: 'subLimitKind', amount: 'subLimitAmount', period: 'subLimitPeriod',
    zone: 'subLimitZone', anchor: 'subLimitAnchor', action: 'subLimitAction',
    down: 'subLimitDown', up: 'subLimitUp',
  };
  function render(): void {
    const list = el('subscriberDraftLimits');
    if (list) list.innerHTML = draft.snapshot().map(limit => `<p>${esc(limit.id)}: ${esc(limit.amount)} ${esc(limit.kind)}, ${esc(limit.period.kind)} (${esc(limit.period.zone)}), ${esc(limit.action)}${limit.action === 'throttle' ? ` · download ${esc(limit.down)}, upload ${esc(limit.up)} bit/s` : ''} <button type="button" class="sbtn sbtn-outline" data-limit-edit="${esc(limit.id)}" aria-label="Edit ${esc(limit.id)}">Edit</button> <button type="button" class="sbtn sbtn-outline" data-limit-remove="${esc(limit.id)}" aria-label="Remove ${esc(limit.id)}">Remove</button></p>`).join('') || '<p class="text-secondary">No draft limits. Add time and data limits to apply them concurrently.</p>';
    const add = el('subscriberAddLimit');
    if (add) add.textContent = editing ? `Save ${editing}` : 'Add limit';
    const cancel = el('subscriberCancelLimit');
    if (cancel) cancel.hidden = editing === undefined;
  }
  function rateFields(): void {
    const throttle = el<HTMLSelectElement>('subLimitAction')?.value === 'throttle';
    const rates = el('subscriberThrottleFields');
    if (rates) rates.hidden = !throttle;
  }
  el('subLimitAction')?.addEventListener('change', rateFields);
  el('subscriberAddLimit')?.addEventListener('click', () => {
    if (busy()) return;
    try {
      const input = Object.fromEntries(Object.entries(fields).map(([key, id]) => [key, el<HTMLInputElement | HTMLSelectElement>(id)?.value || ''])) as LimitInput;
      draft.save(input, editing);
      editing = undefined; render(); say('Draft limit saved. Save the plan version to persist it.');
      el('subLimitKind')?.focus();
    } catch (error) { say(error instanceof Error ? error.message : 'Invalid limit.'); }
  });
  el('subscriberCancelLimit')?.addEventListener('click', () => {
    if (busy()) return;
    editing = undefined; render(); say('Limit edit cancelled. The saved draft is unchanged.');
    el('subLimitKind')?.focus();
  });
  el('subscriberDraftLimits')?.addEventListener('click', event => {
    if (busy() || !(event.target instanceof HTMLButtonElement)) return;
    const id = event.target.dataset.limitEdit || event.target.dataset.limitRemove;
    const limit = draft.snapshot().find(item => item.id === id);
    if (!limit) return;
    if (event.target.dataset.limitRemove) {
      draft.remove(limit.id); if (editing === limit.id) editing = undefined;
      render(); say('Limit removed from the draft.'); el('subLimitKind')?.focus(); return;
    }
    editing = limit.id;
    const values: LimitInput = { kind: limit.kind, amount: limit.amount, period: limit.period.kind, zone: limit.period.zone, anchor: limit.period.anchor, action: limit.action, down: limit.down, up: limit.up };
    for (const [key, id] of Object.entries(fields)) {
      const field = el<HTMLInputElement | HTMLSelectElement>(id);
      if (field) field.value = values[key as keyof LimitInput];
    }
    render(); rateFields(); el('subLimitAmount')?.focus();
  });
  render(); rateFields();
  return () => {
    if (editing !== undefined) throw new Error('Save or cancel the current limit edit before saving the plan.');
    return draft.snapshot();
  };
}
