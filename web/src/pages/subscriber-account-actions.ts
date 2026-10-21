import { el, esc, pill } from '../dom';
import { fieldHtml, toggleHtml } from '../form';

export type SubscriberAdminAccount = { id: string; username: string; name: string; status: string; statusRevision: string };
type BatchResult = { batchId: string; changed: SubscriberAdminAccount[]; skipped: number };
type API = <T>(path: string, body?: unknown) => Promise<T>;

export function filterSubscriberAccounts(accounts: SubscriberAdminAccount[], query: string, status: string): SubscriberAdminAccount[] {
  const needle = query.trim().toLowerCase();
  return accounts.filter(account => (status === 'all' || account.status === status) &&
    (!needle || account.username.toLowerCase().includes(needle) || account.name.toLowerCase().includes(needle)));
}

export function initSubscriberAccountActions(api: API, redraw: () => void, committed: (accounts: SubscriberAdminAccount[]) => void, say: (message: string) => void) {
  const selected = new Map<string, SubscriberAdminAccount>();
  let busy = false, needsRefresh = false;
  let review: { accounts: SubscriberAdminAccount[]; status: string; reason: string } | null = null;
  const host = el('subscriberAccountTools');
  const field = (id: string, label: string, control: string, help = '') => fieldHtml({ id, label, help }, control);
  if (host) host.innerHTML = `<form id="subscriberAccountFilter"><div class="ff-grid">${field('subscriberAccountSearch', 'Search subscribers', '<input id="subscriberAccountSearch" class="sform-input" type="search">', 'Match the original username or display name.')}${field('subscriberAccountStatus', 'Account status', '<select id="subscriberAccountStatus" class="sform-input"><option value="all">All statuses</option><option value="active">Active</option><option value="suspended">Suspended</option></select>')}</div></form>
<p id="subscriberAccountFilterCount" class="text-secondary"></p><p id="subscriberSelectionCount" class="text-secondary" role="status" aria-live="polite"></p>
<form id="subscriberBatchForm"><div class="ff-grid">${field('subscriberBatchStatus', 'New account status', '<select id="subscriberBatchStatus" class="sform-input"><option value="suspended">Suspended</option><option value="active">Active</option></select>', 'Suspension blocks new Internet admission and is processed by background enforcement. Portal sign-in remains available.')}${field('subscriberBatchReason', 'Batch change reason', '<input id="subscriberBatchReason" class="sform-input" required>', 'Required. At most 2048 UTF-8 bytes. Usage and subscriptions are preserved.')}</div><button id="subscriberBatchPrepare" type="submit" class="sbtn sbtn-outline" disabled>Review selected subscribers</button> <button id="subscriberBatchClear" type="button" class="sbtn sbtn-outline" disabled>Clear selection</button></form>
<section id="subscriberBatchReview" class="mt-3" aria-labelledby="subscriberBatchHeading" hidden><h4 id="subscriberBatchHeading">Review account status</h4><ul id="subscriberBatchAccounts" class="text-break"></ul><p id="subscriberBatchReviewedReason" class="text-break"></p><div class="ff-grid ff-one">${field('subscriberBatchConfirm', 'I reviewed these subscribers and their new status.', toggleHtml('subscriberBatchConfirm', false), 'Required before applying the frozen selection.')}</div><div class="mt-2"><button id="subscriberBatchApply" class="sbtn sbtn-primary" type="button" disabled>Apply reviewed status change</button> <button id="subscriberBatchBack" class="sbtn sbtn-outline" type="button">Back to selection</button></div></section>`;
  const value = (id: string) => el<HTMLInputElement>(id)?.value || '';
  const query = () => value('subscriberAccountSearch');
  const status = () => value('subscriberAccountStatus') || 'all';
  const validReason = () => value('subscriberBatchReason').trim() !== '' && new TextEncoder().encode(value('subscriberBatchReason')).length <= 2048;
  function controls(): void {
    const count = el('subscriberSelectionCount');
    if (count) count.textContent = `${selected.size} selected. Maximum 50. Changing filters or refreshing clears the selection.`;
    const prepare = el<HTMLButtonElement>('subscriberBatchPrepare');
    if (prepare) prepare.disabled = busy || needsRefresh || !!review || selected.size === 0 || !validReason();
    const clear = el<HTMLButtonElement>('subscriberBatchClear');
    if (clear) clear.disabled = busy || selected.size === 0;
    for (const id of ['subscriberBatchStatus', 'subscriberBatchReason']) {
      const control = el<HTMLInputElement>(id);
      if (control) control.disabled = busy || !!review;
    }
    for (const id of ['subscriberAccountSearch', 'subscriberAccountStatus', 'subscriberBatchBack', 'subscriberBatchConfirm', 'subscribersRefresh']) {
      const control = el<HTMLInputElement | HTMLButtonElement>(id);
      if (control) control.disabled = busy;
    }
    const apply = el<HTMLButtonElement>('subscriberBatchApply');
    if (apply) {
      apply.disabled = busy || needsRefresh || !review || !el<HTMLInputElement>('subscriberBatchConfirm')?.checked;
      apply.textContent = busy ? 'Applying…' : 'Apply reviewed status change';
    }
    document.querySelectorAll<HTMLInputElement>('[data-sub-select]').forEach(control => { control.disabled = busy || !!review; });
    document.querySelectorAll<HTMLButtonElement>('#subscriberAccountRows button').forEach(control => { control.disabled = busy; });
  }
  function clearReview(): void {
    review = null;
    const section = el('subscriberBatchReview'); if (section) section.hidden = true;
    const confirm = el<HTMLInputElement>('subscriberBatchConfirm'); if (confirm) confirm.checked = false;
  }
  function clearSelection(): void { selected.clear(); clearReview(); redraw(); controls(); }
  el('subscriberAccountFilter')?.addEventListener('submit', event => event.preventDefault());
  for (const id of ['subscriberAccountSearch', 'subscriberAccountStatus']) {
    el(id)?.addEventListener(id === 'subscriberAccountSearch' ? 'input' : 'change', () => {
      if (busy) return;
      el('subscriberAccountSearch')?.classList.toggle('active', query().trim() !== '');
      el('subscriberAccountStatus')?.classList.toggle('active', status() !== 'all');
      clearSelection();
    });
  }
  el('subscriberBatchReason')?.addEventListener('input', controls);
  el('subscriberBatchConfirm')?.addEventListener('change', controls);
  el('subscriberBatchClear')?.addEventListener('click', () => { if (!busy) clearSelection(); });
  el('subscriberBatchBack')?.addEventListener('click', () => {
    if (busy) return;
    clearReview(); redraw(); controls(); el('subscriberBatchReason')?.focus();
  });
  el('subscriberBatchForm')?.addEventListener('submit', event => {
    event.preventDefault();
    if (busy || needsRefresh || review || selected.size === 0 || selected.size > 50 || !validReason()) return;
    review = { accounts: [...selected.values()].map(account => ({ ...account })), status: value('subscriberBatchStatus'), reason: value('subscriberBatchReason') };
    const list = el('subscriberBatchAccounts');
    if (list) list.innerHTML = review.accounts.map(account => `<li><bdi>${esc(account.username)}</bdi>: ${pill(account.status, account.status === 'active' ? 'ok' : 'warn')} to ${pill(review!.status, review!.status === 'active' ? 'ok' : 'warn')}</li>`).join('');
    const reason = el('subscriberBatchReviewedReason'); if (reason) reason.textContent = 'Reason: ' + review.reason;
    const section = el('subscriberBatchReview'); if (section) section.hidden = false;
    redraw(); controls(); el('subscriberBatchConfirm')?.focus();
  });
  el('subscriberBatchApply')?.addEventListener('click', async () => {
    if (busy || needsRefresh || !review || !el<HTMLInputElement>('subscriberBatchConfirm')?.checked) return;
    const intent = review;
    busy = true; controls();
    try {
      const result = await api<BatchResult>('/api/subscribers', { action: 'status-batch', status: intent.status, reason: intent.reason, items: intent.accounts.map(account => ({ accountId: account.id, expectedStatus: account.status, expectedRevision: account.statusRevision })) });
      selected.clear(); clearReview();
      committed(result.changed);
      say(`Status updated for ${result.changed.length} subscribers. ${result.skipped} already had that status.`);
      el('subscriberAccountSearch')?.focus();
    } catch (error) {
      needsRefresh = true;
      say((error instanceof Error ? error.message : 'Status change was not confirmed.') + ' Refresh the list before reviewing another batch.');
    } finally {
      busy = false; controls();
      if (!needsRefresh) el('subscriberAccountSearch')?.focus();
    }
  });
  controls();
  return {
    filtered(accounts: SubscriberAdminAccount[]): SubscriberAdminAccount[] {
      const visible = filterSubscriberAccounts(accounts, query(), status());
      const count = el('subscriberAccountFilterCount');
      if (count) count.textContent = `${visible.length} of ${accounts.length} subscribers shown.`;
      return visible;
    },
    selectionCell(account: SubscriberAdminAccount): string {
      return `<td class="tbl-pick"><input type="checkbox" data-sub-select="${esc(account.id)}" aria-label="Select ${esc(account.username)}"${selected.has(account.id) ? ' checked' : ''}${busy || review ? ' disabled' : ''}></td>`;
    },
    toggle(account: SubscriberAdminAccount, checked: boolean): void {
      if (busy || review) return;
      if (checked && !selected.has(account.id) && selected.size >= 50) {
        say('Select at most 50 subscribers for one reviewed status change.');
      } else if (checked) selected.set(account.id, { ...account });
      else selected.delete(account.id);
      redraw(); controls();
      Array.from(document.querySelectorAll<HTMLInputElement>('[data-sub-select]')).find(control => control.dataset.subSelect === account.id)?.focus();
    },
    reviewOne(account: SubscriberAdminAccount, next: string): void {
      if (busy) return;
      selected.clear(); clearReview(); selected.set(account.id, { ...account });
      const target = el<HTMLSelectElement>('subscriberBatchStatus'); if (target) target.value = next;
      redraw(); controls(); el('subscriberBatchReason')?.focus();
      say('Enter a change reason and review this subscriber before applying the new status.');
    },
    refreshed(): void { needsRefresh = false; selected.clear(); clearReview(); controls(); },
  };
}
