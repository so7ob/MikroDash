import { el, esc } from '../dom';
import { fmtTs } from '../timefmt';

interface SetupReport {
  routerId: string;
  state: 'ready' | 'missing' | 'unknown';
  checkedAt: number;
  checks: { key: string; label: string; ready: boolean; required: boolean }[];
  canConfigure: boolean;
  templateUrl?: string;
  error?: string;
}

/** On-demand metadata check, independent of analysis queries and filters. */
export function mountActivitySetup(visible: () => boolean): {
  refresh(routerId: string, folder: string, force?: boolean): Promise<void>;
  stop(): void;
} {
  const panel = el('activitySetupPanel');
  const button = el<HTMLButtonElement>('activityCheckSetup');
  const prompt = el('activitySetupPrompt');
  const link = el<HTMLAnchorElement>('activitySetupLink');
  const status = el('activitySetupStatus');
  const checks = el('activitySetupChecks');
  let controller: AbortController | undefined;
  let sequence = 0;
  let key = '';
  let router = '', folder = '';

  function stop(): void {
    sequence++; controller?.abort(); key = ''; router = ''; folder = '';
    if (prompt) prompt.hidden = true;
    if (link) { link.hidden = true; link.removeAttribute('href'); }
    if (button) button.disabled = true;
    panel?.setAttribute('aria-busy', 'false');
    if (status) status.textContent = 'Choose a router to check its logging setup.';
    if (checks) checks.innerHTML = '';
  }

  async function refresh(routerId: string, logFolder: string, force = false): Promise<void> {
    if (!panel || !button || !prompt || !link || !status || !checks || !routerId || !visible()) return;
    const next = JSON.stringify([routerId, logFolder]);
    if (next === key && !force) return;
    key = next; router = routerId; folder = logFolder;
    const current = ++sequence;
    controller?.abort(); controller = new AbortController();
    prompt.hidden = true; link.hidden = true; link.removeAttribute('href');
    button.disabled = true; panel.setAttribute('aria-busy', 'true');
    status.textContent = 'Checking router logging setup…'; checks.innerHTML = '';
    try {
      const response = await fetch('/api/activity/setup?routerId=' + encodeURIComponent(routerId), {
        credentials: 'same-origin', signal: controller.signal,
      });
      const report = await response.json() as SetupReport;
      if (!response.ok || report.routerId !== routerId || !['ready', 'missing', 'unknown'].includes(report.state)) {
        throw new Error('Router setup could not be checked. Verify access and try again.');
      }
      if (current !== sequence || !visible()) return;
      const missing = report.state === 'missing';
      const canOpen = report.canConfigure === true && /^\/config-management\?/.test(report.templateUrl || '');
      const message = el('activitySetupPromptText');
      if (message) message.textContent = 'Activity logging setup is incomplete. ' + (canOpen
        ? 'Open the setup template to review and install the missing configuration.'
        : 'Ask an administrator to install the Activity logging template.');
      prompt.hidden = !missing;
      link.hidden = !missing || !canOpen;
      if (missing && canOpen) link.href = report.templateUrl!;
      status.textContent = report.state === 'ready'
        ? 'Required logging settings found. Synchronization reports USB availability and file-read access.'
        : missing ? 'Some required logging settings are missing or differ from this Activity setup.'
        : report.error || 'Router setup could not be checked.';
      status.textContent += report.checkedAt ? ' · Checked: ' + fmtTs(report.checkedAt) : '';
      checks.innerHTML = (report.checks || []).map(check => '<li><span class="pill pill-' +
        (check.ready ? 'ok' : check.required ? 'warn' : 'muted') + '">' +
        (check.ready ? 'Found' : check.required ? 'Missing' : 'Optional') + '</span><span>' + esc(check.label) + '</span></li>').join('');
    } catch (error) {
      if (current !== sequence || !visible()) return;
      status.textContent = error instanceof Error ? error.message : 'Router setup could not be checked.';
      // A failed read never becomes a recommendation to replace configuration.
      prompt.hidden = true;
    } finally {
      if (current === sequence) { button.disabled = false; panel.setAttribute('aria-busy', 'false'); }
    }
  }
  button?.addEventListener('click', () => { void refresh(router, folder, true); });
  return { refresh, stop };
}
