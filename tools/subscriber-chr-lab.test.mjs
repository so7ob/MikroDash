import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import * as fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { nativePortalRules } from './subscriber-chr-network.mjs';

const command = fileURLToPath(new URL('./subscriber-chr-lab.mjs', import.meta.url));
const invoke = args => spawnSync(process.execPath, [command, ...args], { encoding: 'utf8', timeout: 5000 });

function verdict(chain, packet) {
  for (const command of nativePortalRules('mikrodash-subscriber-chr-0123abcd').nftables) {
    const rule = command.add.rule;
    if (rule?.chain !== chain) continue;
    let matches = true;
    for (const expression of rule.expr) {
      if (expression.match) {
        const { left, op, right } = expression.match;
        const actual = left.meta ? packet[left.meta.key] : packet[`${left.payload.protocol}.${left.payload.field}`];
        matches &&= op === '==' ? actual === right : actual !== right;
      } else if (matches && 'drop' in expression) return 'drop';
      else if (matches && 'accept' in expression) return 'accept';
    }
  }
  return 'accept'; // The owned base chains preserve unrelated host traffic.
}

test('native CHR portal policy exposes only its TLS listener to lab clients', () => {
  for (const address of ['198.51.100.10', '127.0.0.1', '192.168.253.128']) {
    for (const port of [22, 80, 3082, 5432, 8090, 9444, 13083]) {
      const expected = address === '198.51.100.10' && port === 9444 ? 'accept' : 'drop';
      assert.equal(verdict('input', { iifname: 'md-lab-br', l4proto: 'tcp', 'ip.daddr': address, 'tcp.dport': port }), expected);
    }
  }
  assert.equal(verdict('input', { iifname: 'ens37', l4proto: 'tcp', 'ip.daddr': '127.0.0.1', 'tcp.dport': 3082 }), 'accept');
});

test('native CHR policy preserves bridge traffic and refuses host routing bypass', () => {
  assert.equal(verdict('forward', { iifname: 'md-lab-br', oifname: 'md-lab-br' }), 'accept');
  for (const oifname of ['ens33', 'ens37', 'docker0']) assert.equal(verdict('forward', { iifname: 'md-lab-br', oifname }), 'drop');
  assert.equal(verdict('forward', { iifname: 'ens37', oifname: 'docker0' }), 'accept');
  assert.throws(() => nativePortalRules('operator-router'), /Owned CHR identity/);
});

test('CHR lifecycle rejects missing action and relative private path', () => {
  for (const args of [[], ['check', './relative'], ['unknown', '/synthetic']]) {
    const result = invoke(args);
    assert.equal(result.status, 1);
    assert.match(result.stderr, /Usage/);
  }
});

test('CHR ownership check refuses an unrelated live process without signaling it', t => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'mikrodash-chr-tool-'));
  fs.chmodSync(directory, 0o700);
  t.after(() => fs.rmSync(directory, { recursive: true }));
  const state = { pid: process.pid, name: 'mikrodash-subscriber-chr-0123abcd', namespace: 'md-subscriber-lab',
    interfaces: ['md-lab-br', 'md-lab-tap', 'md-lab-host'], apiPort: 18728, disconnectPort: 13799,
    disk: '/dev/shm/mikrodash-subscriber-chr-lab/run-0123abcd/disk.img', console: path.join(directory, 'console.sock'),
    startTicks: 'synthetic', args: process.argv };
  fs.writeFileSync(path.join(directory, 'runtime.json'), JSON.stringify(state), { mode: 0o600 });
  const result = invoke(['check', directory]);
  assert.equal(result.status, 1);
  assert.match(result.stderr, /Owned QEMU process proof failed/);
  assert.doesNotThrow(() => process.kill(process.pid, 0));
});

test('CHR lifecycle check rejects public manifests, symlinks and directories', t => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'mikrodash-chr-tool-'));
  fs.chmodSync(directory, 0o700);
  t.after(() => fs.rmSync(directory, { recursive: true }));
  const file = path.join(directory, 'runtime.json');
  fs.writeFileSync(file, '{}', { mode: 0o644 });
  // A restrictive caller umask must not turn this rejection fixture private.
  fs.chmodSync(file, 0o644);
  assert.equal(fs.statSync(file).mode & 0o777, 0o644);
  let result = invoke(['check', directory]);
  assert.equal(result.status, 1);
  assert.match(result.stderr, /Private regular lab file required/);
  fs.unlinkSync(file);
  fs.symlinkSync('/etc/passwd', file);
  result = invoke(['check', directory]);
  assert.equal(result.status, 1);
  assert.match(result.stderr, /Private regular lab file required/);
  fs.chmodSync(directory, 0o755);
  result = invoke(['check', directory]);
  assert.equal(result.status, 1);
  assert.match(result.stderr, /Private non-symlink directory required/);
});
