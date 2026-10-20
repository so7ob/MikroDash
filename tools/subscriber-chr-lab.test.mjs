import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import * as fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const command = fileURLToPath(new URL('./subscriber-chr-lab.mjs', import.meta.url));
const invoke = args => spawnSync(process.execPath, [command, ...args], { encoding: 'utf8', timeout: 5000 });

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
