import { hotspotDestination } from '../subscriber-handoff';

const body = document.body;
const link = document.getElementById('subscriberPortalLink');
if (link instanceof HTMLAnchorElement) {
  try {
    const mode = body.dataset.failed === 'yes' ? 'failure' : body.dataset.mode || 'account';
    const target = hotspotDestination(mode, body.dataset.portal || '', link.href, body.dataset.return || '', body.dataset.original || '');
    link.href = target;
    location.replace(target);
  } catch {
    // The fixed rendered link remains usable when a browser cannot redirect.
    const status = document.getElementById('handoffStatus');
    if (status) status.textContent = 'Select Continue to subscriber portal to open your account.';
  }
}
