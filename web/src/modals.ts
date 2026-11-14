// Closing a dialog: the × button, the backdrop, and Escape.
//
// SHELL wiring, not page wiring. The extracted markup uses `data-modal-close`
// on every dialog's × and Cancel, and the live app handles all three routes once
// with delegated listeners rather than per page - which is why porting the first
// route per page left wanWarnWrap's two buttons dead.
//
// ── THE LIST IS GENERATED BECAUSE ITS NAME LIES ─────────────────────────────
//
// Upstream it is `_PRINCIPAL_MODALS`, which is a leftover: it began as the
// Settings principals dialogs and has not been that for a long time. The live
// source says so in as many words - "Escape and backdrop-click are handled here
// for every dialog in the app". An earlier version of this port read the name,
// believed it, and skipped both routes on the stated grounds that none of the
// list was ported. Four of the ten are, so five dialogs here had no Escape and
// no backdrop click behind a justification that read as considered.

import { el } from './dom.js';
import { CLOSABLE_MODALS } from './gen/modals.js';

export function wireModals(): void {
  document.addEventListener('click', (e) => {
    const t = e.target as HTMLElement | null;
    const closer = t?.closest?.('[data-modal-close]');
    if (closer) {
      e.preventDefault();
      el(closer.getAttribute('data-modal-close') || '')?.classList.remove('open');
      return;
    }
    // Clicking INSIDE must not close, which is what testing the target itself
    // rather than an ancestor achieves: only the backdrop element carries the
    // class, so a click on the dialog's own content never matches.
    if (t && t.classList && t.classList.contains('rtr-modal-bg') &&
        CLOSABLE_MODALS.includes(t.id)) {
      t.classList.remove('open');
    }
  });

  // ── ESCAPE CLOSES THE TOPMOST ONE, NOT ALL OF THEM ────────────────────
  //
  // It used to close every dialog in the list, open or not, on the stated
  // grounds that "removing a class an element does not have is a no-op, and
  // checking first would be more code for the same result". That was true while
  // no dialog could open over another.
  //
  // One can now: the notification channel's Events tab has a gear that opens an
  // interface-type picker over it. Escape in the picker closed the channel
  // dialog with it, throwing away every edit the operator had made and not yet
  // saved - and it looked like the picker doing something violent rather than
  // Escape doing too much.
  //
  // BY z-index, NOT DOM ORDER. A dialog stacks above another because its
  // z-index says so, and that is the same thing the operator sees; DOM order
  // agrees today and is not what decides what is on top.
  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return;
    let top: HTMLElement | null = null;
    let topZ = -Infinity;
    for (const id of CLOSABLE_MODALS) {
      const m = el(id);
      if (!m || !m.classList.contains('open')) continue;
      // A dialog with no z-index of its own reads as 'auto' and parses NaN; it
      // sits at the shared default, so treat it as 0 rather than dropping it.
      const z = parseInt(getComputedStyle(m).zIndex, 10);
      const rank = Number.isNaN(z) ? 0 : z;
      // `>=` so that, among equals, the LAST one wins: later in the list is
      // later in the document, which is what sits on top when z-indexes tie.
      if (rank >= topZ) {
        topZ = rank;
        top = m;
      }
    }
    top?.classList.remove('open');
  });
}


/** Shared managed form dialog, using the same shell as the resource editor.
 * Capture owns its close events so the legacy delegated closer cannot discard
 * a dirty or saving form. Failed writes retain the original controls and values.
 */
export function managedModal(id: string, title: string, content: HTMLElement, wide = false, trackDirty = true) {
  const backdrop = document.createElement('div');
  backdrop.id = id; backdrop.className = 'rtr-modal-bg';
  backdrop.innerHTML = `<div class="rtr-modal managed-modal ${wide ? 'subscriber-wide-modal' : 'pmodal'}" role="dialog" aria-modal="true" aria-labelledby="${id}-title" tabindex="-1"><div class="rtr-modal-hdr"><h2 id="${id}-title" class="rtr-modal-title"></h2><button type="button" class="rtr-modal-close" data-managed-close aria-label="Close">×</button></div><div class="rtr-modal-body"></div><div class="rtr-modal-footer"><p data-managed-error role="alert" class="text-break"></p><div data-managed-discard hidden><p>Discard unsaved changes?</p><button type="button" class="sbtn sbtn-danger" data-managed-discard>Discard changes</button> <button type="button" class="sbtn sbtn-outline" data-managed-keep>Keep editing</button></div><button type="button" class="sbtn sbtn-outline" data-managed-close>Cancel</button></div></div>`;
  backdrop.querySelector('h2')!.textContent = title;
  const body = backdrop.querySelector<HTMLElement>('.rtr-modal-body')!;
  body.appendChild(content);
  if(content instanceof HTMLFormElement){
    const footer=backdrop.querySelector<HTMLElement>('.rtr-modal-footer')!;
    const actions=Array.from(content.children).filter((n):n is HTMLButtonElement=>n instanceof HTMLButtonElement&&(n.type==='submit'||n.id==='subscriberNameCancel'));
    const internalCancel=actions.find(n=>n.type==='button');if(internalCancel)footer.querySelector('[data-managed-close]')?.remove();
    content.classList.add('rtr-modal-form');
    const fields=document.createElement('div');fields.className='rtr-modal-body';
    while(content.firstChild)fields.appendChild(content.firstChild);
    body.replaceWith(content);content.append(fields,footer);
    for(const action of actions)footer.appendChild(action);
  }
  document.body.appendChild(backdrop);
  let opener: HTMLElement | null = null, dirty = false, busy = false;
  let closed = (): void => {};
  let inert: {node: HTMLElement; value: boolean}[] = [];
  const discard = backdrop.querySelector<HTMLElement>('div[data-managed-discard]')!;
  const error = backdrop.querySelector<HTMLElement>('[data-managed-error]')!;
  const focusables = () => Array.from(backdrop.querySelectorAll<HTMLElement>('button,input,select,textarea,a[href],[tabindex="0"]')).filter(n => !n.closest('[hidden]') && !(n as HTMLInputElement).disabled);
  function close(force = false): boolean {
    if (busy) return false;
    if (dirty && !force) { discard.hidden = false; discard.querySelector<HTMLElement>('button')?.focus(); return false; }
    const wasOpen=backdrop.classList.contains('open');backdrop.classList.remove('open');
    for (const item of inert) item.node.inert = item.value;
    inert = []; if(wasOpen)closed();opener?.focus();return true;
  }
  backdrop.addEventListener('input', () => { if(trackDirty)dirty = true; });
  backdrop.addEventListener('change', () => { if(trackDirty)dirty = true; });
  backdrop.addEventListener('click', e => {
    const target = e.target;
    if (!(target instanceof HTMLElement)) return;
    if (target.closest('[data-managed-close]') || target === backdrop) { e.preventDefault(); e.stopImmediatePropagation(); close(); }
    if (target.closest('button[data-managed-discard]')) close(true);
    if (target.closest('[data-managed-keep]')) { discard.hidden = true; focusables()[1]?.focus(); }
  }, true);
  const keydown = (e: KeyboardEvent): void => {
    if (!backdrop.classList.contains('open')) return;
    if (e.key === 'Escape') { e.preventDefault(); e.stopImmediatePropagation(); close(); }
    if (e.key === 'Tab') {
      const items = focusables(), first = items[0], last = items[items.length - 1];
      if(items.length===0){e.preventDefault();backdrop.querySelector<HTMLElement>('[role=dialog]')?.focus();return;}
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus(); }
    }
  };
  document.addEventListener('keydown', keydown, true);
  return {
    destroy(): void { close(true); document.removeEventListener('keydown', keydown, true); backdrop.remove(); },
    open(): void {
      if (backdrop.classList.contains('open')) return;
      opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      dirty = false; error.textContent = ''; discard.hidden = true; content.hidden = false;
      inert = Array.from(document.body.children).filter((n): n is HTMLElement => n instanceof HTMLElement && n !== backdrop).map(node => ({node, value: node.inert}));
      for (const item of inert) item.node.inert = true;
      backdrop.classList.add('open');
      (content.querySelector<HTMLElement>('input:not([type="hidden"]),select,button') || backdrop.querySelector<HTMLElement>('[role="dialog"]'))?.focus();
    },
    close,
    onClose(callback: () => void): void { closed = callback; },
    busy(value: boolean): void { busy = value; backdrop.querySelectorAll<HTMLButtonElement>('[data-managed-close],button[data-managed-discard]').forEach(n => { n.disabled = value; }); backdrop.querySelector('[role="dialog"]')?.setAttribute('aria-busy', String(value)); },
    error(message: string): void { error.textContent = message; },
    dirty(): void { dirty = true; },
    isOpen(): boolean { return backdrop.classList.contains('open'); },
  };
}
