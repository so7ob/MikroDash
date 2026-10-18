import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';
import { verifyImage } from './image-check.mjs';

const digest = `sha256:${'a'.repeat(64)}`;
function manifest() {
  return { digest, manifests: [
    { platform: { os: 'linux', architecture: 'amd64' } },
    { platform: { os: 'linux', architecture: 'arm64' } },
    { platform: { os: 'linux', architecture: 'arm', variant: 'v7' } },
    { platform: { os: 'unknown', architecture: 'unknown' },
      annotations: { 'vnd.docker.reference.type': 'attestation-manifest' } },
  ] };
}
test('accepts the release digest, all three platforms and provenance attestations', () => {
  assert.deepEqual(verifyImage(manifest(), digest), ['linux/amd64', 'linux/arm/v7', 'linux/arm64']);
  const m = manifest(); m.manifests[1].platform.variant = 'v8';
  assert.equal(verifyImage(m, digest).length, 3);
});
test('rejects a tag pointing to another image', () => {
  assert.throws(() => verifyImage(manifest(), `sha256:${'b'.repeat(64)}`), /build digest/);
});
test('rejects a missing or malformed expected build digest', () => {
  for (const expected of [undefined, '', 'sha256:abc'])
    assert.throws(() => verifyImage({ ...manifest(), digest: expected }, expected), /build digest/);
});
test('rejects an empty or missing platform set', () => {
  for (const manifests of [[], undefined])
    assert.throws(() => verifyImage({ digest, manifests }, digest), /no platform/);
});
test('rejects a missing ARMv7 build', () => {
  const m = manifest(); m.manifests.splice(2, 1);
  assert.throws(() => verifyImage(m, digest), /exactly/);
});
test('rejects a duplicate platform instead of counting it as another architecture', () => {
  const m = manifest(); m.manifests.push(m.manifests[0]);
  assert.throws(() => verifyImage(m, digest), /exactly/);
});
test('rejects the wrong ARM variant', () => {
  const m = manifest(); m.manifests[2].platform.variant = 'v6';
  assert.throws(() => verifyImage(m, digest), /exactly/);
});
test('does not hide an unknown platform unless it is a provenance attestation', () => {
  const m = manifest(); delete m.manifests[3].annotations;
  assert.throws(() => verifyImage(m, digest), /exactly/);
});

test('the CLI verifies the supplied registry file and refuses a mismatched tag', (t) => {
  const dir = mkdtempSync(join(tmpdir(), 'mikrodash-image-check-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const path = join(dir, 'manifest.json');
  writeFileSync(path, JSON.stringify(manifest()));
  const script = fileURLToPath(new URL('./image-check.mjs', import.meta.url));
  const run = (expected) => spawnSync(process.execPath, [script, expected, path], { encoding: 'utf8' });
  const good = run(digest);
  assert.equal(good.status, 0, good.stderr);
  assert.match(good.stdout, /linux\/arm\/v7/);
  const bad = run(`sha256:${'b'.repeat(64)}`);
  assert.equal(bad.status, 1);
  assert.match(bad.stderr, /build digest/);
});
