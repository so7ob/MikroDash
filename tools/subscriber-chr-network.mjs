// Private native-TLS portal exposure for the disposable CHR network only.
export const nativePortalTable = 'md_subscriber_lab';
export const nativePortalAddress = '198.51.100.10/24';

export function nativePortalRules(owner) {
  if (!/^mikrodash-subscriber-chr-[0-9a-f]{8}$/.test(owner)) throw new Error('Owned CHR identity required');
  const table = { family: 'inet', table: nativePortalTable };
  const iface = { match: { op: '==', left: { meta: { key: 'iifname' } }, right: 'md-lab-br' } };
  return { nftables: [
    { add: { table: { family: 'inet', name: nativePortalTable, comment: owner } } },
    { add: { chain: { ...table, name: 'input', type: 'filter', hook: 'input', prio: -10, policy: 'accept' } } },
    { add: { rule: { ...table, chain: 'input', expr: [iface,
      { match: { op: '==', left: { payload: { protocol: 'ip', field: 'daddr' } }, right: '198.51.100.10' } },
      { match: { op: '==', left: { meta: { key: 'l4proto' } }, right: 'tcp' } },
      { match: { op: '==', left: { payload: { protocol: 'tcp', field: 'dport' } }, right: 9444 } },
      { accept: null }] } } },
    { add: { rule: { ...table, chain: 'input', expr: [iface, { drop: null }] } } },
    { add: { chain: { ...table, name: 'forward', type: 'filter', hook: 'forward', prio: -10, policy: 'accept' } } },
    { add: { rule: { ...table, chain: 'forward', expr: [iface,
      { match: { op: '!=', left: { meta: { key: 'oifname' } }, right: 'md-lab-br' } },
      { drop: null }] } } },
  ] };
}
