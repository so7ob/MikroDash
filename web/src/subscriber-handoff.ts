import { connectionReference } from './subscriber-connection';

// RouterOS -esc values use form/URL escaping. Only the owned root return is
// accepted; an arbitrary dst cannot become an open redirect in the handoff.
export function hotspotReturn(escaped: string, origin: string): string {
  try {
    const destination = new URL(decodeURIComponent(escaped.replace(/\+/g, ' ')));
    const portal = new URL(origin);
    if (portal.protocol !== 'https:' || portal.username || portal.password || portal.search || portal.hash || portal.pathname !== '/') return '';
    if (destination.origin !== portal.origin || destination.pathname !== '/' || destination.username || destination.password || destination.hash || Array.from(destination.searchParams).length !== 1 || !connectionReference(destination.searchParams.get('connection'))) return '';
    return destination.href;
  } catch { return ''; }
}

export function hotspotDestination(mode: string, origin: string, entry: string, escapedReturn: string, escapedOriginal: string): string {
  const portal = new URL(origin);
  if (portal.protocol !== 'https:' || portal.username || portal.password || portal.search || portal.hash || portal.pathname !== '/') throw new Error('Invalid portal destination');
  const owned = hotspotReturn(escapedReturn, origin) || hotspotReturn(escapedOriginal, origin);
  if (mode === 'success') return owned || portal.href;
  if (mode === 'failure') { const target = new URL(owned || portal.href); target.searchParams.set('hotspot', 'failed'); return target.href; }
  if (mode === 'login') {
    const target = new URL(entry);
    if (target.origin === portal.origin && target.pathname === '/' && !target.username && !target.password && !target.hash && Array.from(target.searchParams).length === 1 && target.searchParams.has('connect')) return target.href;
    throw new Error('Invalid network entry');
  }
  return portal.href;
}
