import { subscriberText, type SubscriberLanguage } from './subscriber-ar';
import type { SubscriberConnectionStatus } from './gen/payloads';
import type { PillTone } from './palette';

export function connectionReference(raw: string | null): string {
  return raw !== null && /^[a-f0-9]{64}$/.test(raw) ? raw : '';
}

export function portalEntry(search: string): { network: string; flowId: string; failed: boolean } {
  const query = new URLSearchParams(search);
  const flowId = query.getAll('connection').length === 1 ? connectionReference(query.get('connection')) : '';
  const network = query.getAll('connect').length === 1 ? query.get('connect') || '' : '';
  return { flowId, network: !flowId && network.length <= 128 ? network : '', failed: query.get('hotspot') === 'failed' };
}

export function connectionReason(reason: string, language: SubscriberLanguage = 'en'): string {
  for (const [prefix, label] of [['exhausted:', 'Allowance exhausted'], ['unknown:', 'Allowance data unavailable'], ['schedule:', 'Outside allowed hours'], ['insufficient-shared-budget:', 'Insufficient shared allowance']] as const) {
    if (reason.startsWith(prefix)) return `${subscriberText(label, language)}: ${reason.slice(prefix.length)}`;
  }
  const labels: Record<string, string> = {
    'accounting-reconciliation-required': 'Accounting reconciliation is required before another connection.',
    'authorization-reconciliation-required': 'The previous authorization needs reconciliation.',
    'subscription-expired': 'Your subscription has expired.',
    'subscription-unavailable': 'No active subscription is available.',
    'network-not-entitled': 'This network is not included in your subscription.',
    'account-suspended': 'Your account is suspended.',
    'session-limit': 'Your concurrent session limit has been reached.',
    'device-already-connected': 'This device already has an outstanding connection.',
    'authentication-routing-unavailable': 'Network sign-in requires administrator review. Try again after configuration is verified.',
    'authentication-not-authorized': 'HotSpot authentication was not authorized.',
    'authorization-denied': 'Internet admission was denied. Review your subscription or contact support.',
    'admission-context-missing': 'The network request could not be verified.',
    'connection-request-mismatch': 'The connection request no longer matches this device.',
    'authorization-expired': 'This authorization expired. Request a new Internet connection.',
    'account-unavailable': 'The account is not available for Internet access.',
    'delegation-unavailable': 'No safe network allowance is available.',
    'authorization-state-unavailable': 'The previous authorization could not be confirmed.',
    'speed-not-entitled': 'The selected speed is not included in this subscription.',
    'invalid-plan': 'The subscription configuration needs administrator review.',
    'authorization-unavailable': 'Authorization could not be completed. Try again or contact support.',
    'router-observation-stale': 'The last router observation is stale. Your last confirmed balances are retained.',
    'hotspot-confirmation-unavailable': 'The router has not confirmed this device. Your account remains available.',
    'connection-identity-unconfirmed': 'This device connection has not been confirmed by the router.',
    'connection-identity-changed': 'The connection identity changed and needs confirmation.',
    'waiting-for-hotspot-confirmation': 'Waiting for the router to confirm this device.',
    'ticket-expired': 'This connection request expired. Select Connect to internet to try again.',
    'session-ended': 'This Internet session has ended. Your remaining allowance is preserved.',
  };
  return subscriberText(labels[reason] || reason, language);
}

export function connectionView(status: SubscriberConnectionStatus | null, now: number, language: SubscriberLanguage = 'en'): { label: string; tone: PillTone; message: string; connected: boolean } {
  const t = (text: string) => subscriberText(text, language);
  if (!status) return { label: t('Internet connection not checked'), tone: 'muted', message: t('Select Connect to internet to authenticate this device. Other devices are listed separately.'), connected: false };
  const observed = Date.parse(status.observedAt);
  const fresh = Number.isFinite(observed) && now - observed <= 5000 && observed - now <= 1000;
  if (status.stage === 'connected' && fresh) return { label: t('Internet connected'), tone: 'ok', message: language === 'ar' ? `تم تأكيد اتصال هذا الجهاز على ${status.network}.` : `This device is confirmed on ${status.network}.`, connected: true };
  if (status.stage === 'connected') return { label: t('Internet connection unconfirmed'), tone: 'warn', message: t('The router observation is stale. Last confirmed balances remain available.'), connected: false };
  const stages: Record<string, { label: string; tone: PillTone; message: string }> = {
    requested: { label: t('Waiting for HotSpot'), tone: 'warn', message: t('Complete the secure HotSpot sign-in for this device.') },
    authorizing: { label: t('HotSpot authentication in progress'), tone: 'warn', message: t('Waiting for the network authorization decision.') },
    authorized: { label: t('Internet authorized; awaiting confirmation'), tone: 'warn', message: t('Authorization was accepted. The router has not yet confirmed this device.') },
    rejected: { label: t('Internet connection denied'), tone: 'err', message: t('Your portal account remains available.') },
    expired: { label: t('Connection request expired'), tone: 'warn', message: t('Select Connect to internet to request a new connection.') },
    disconnected: { label: t('Internet disconnected'), tone: 'muted', message: t('This session ended. Your remaining allowance is preserved.') },
    unconfirmed: { label: t('Internet connection unconfirmed'), tone: 'warn', message: t('The router has not confirmed this device.') },
  };
  const view = stages[status.stage] || stages.unconfirmed!;
  const reasons = status.reasons.map(reason => connectionReason(reason, language)).join(' ');
  return { ...view, message: reasons ? `${view.message} ${reasons}` : view.message, connected: false };
}
