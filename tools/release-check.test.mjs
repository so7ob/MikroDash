import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const script = fileURLToPath(new URL('./release-check.mjs', import.meta.url));
const notes = '## [1.2.3] - Synthetic release\n\n### Fixed\n\n- Synthetic regression.\n\n## [1.2.2] - Older\n\n- Older notes.\n';

function fixture(t, { version = '1.2.3', changelog = notes } = {}) {
  const cwd = mkdtempSync(join(tmpdir(), 'mikrodash-release-test-'));
  t.after(() => rmSync(cwd, { recursive: true, force: true }));
  const env = { ...process.env, GIT_AUTHOR_NAME: 'Release Fixture', GIT_AUTHOR_EMAIL: 'fixture@example.invalid',
    GIT_COMMITTER_NAME: 'Release Fixture', GIT_COMMITTER_EMAIL: 'fixture@example.invalid' };
  const git = (...args) => execFileSync('git', ['-c', 'commit.gpgsign=false', '-c', 'tag.gpgsign=false', ...args],
    { cwd, env, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
  git('init', '-b', 'main');
  mkdirSync(join(cwd, 'internal/server'), { recursive: true });
  writeFileSync(join(cwd, 'internal/server/health_api.go'), `package server\nconst AppVersion = "${version}"\n`);
  writeFileSync(join(cwd, 'CHANGELOG.md'), changelog);
  git('add', '.');
  git('commit', '-m', 'synthetic fixture');
  git('update-ref', 'refs/remotes/origin/main', 'HEAD');
  const run = (tag = 'v1.2.3') => spawnSync(process.execPath, [script, tag], { cwd, env, encoding: 'utf8' });
  return { cwd, git, run };
}
function refused(result, reason) {
  assert.equal(result.status, 1, result.stdout + result.stderr);
  assert.match(result.stderr, reason);
  assert.equal(result.stdout, '', 'a rejected release must not produce notes');
}

for (const annotated of [false, true]) {
  test(`accepts ${annotated ? 'annotated' : 'lightweight'} tag on main and extracts only its notes`, (t) => {
    const { git, run } = fixture(t);
    git('tag', ...(annotated ? ['-a', 'v1.2.3', '-m', 'synthetic tag'] : ['v1.2.3']));
    const result = run();
    assert.equal(result.status, 0, result.stderr);
    assert.equal(result.stdout, '## Synthetic release\n\n### Fixed\n\n- Synthetic regression.\n');
  });
}

test('accepts an already verified main commit after main advances', (t) => {
  const { git, run } = fixture(t);
  git('tag', 'v1.2.3');
  git('commit', '--allow-empty', '-m', 'later main');
  git('update-ref', 'refs/remotes/origin/main', 'HEAD');
  git('checkout', 'v1.2.3');
  assert.equal(run().status, 0);
});

test('rejects a tag on develop that has not reached main', (t) => {
  const { git, run } = fixture(t);
  git('switch', '-c', 'develop');
  git('commit', '--allow-empty', '-m', 'unreleased change');
  git('tag', 'v1.2.3');
  refused(run(), /not on origin\/main/);
});

test('rejects a different checkout from the tag', (t) => {
  const { git, run } = fixture(t);
  git('tag', 'v1.2.3');
  git('commit', '--allow-empty', '-m', 'different checkout');
  git('update-ref', 'refs/remotes/origin/main', 'HEAD');
  refused(run(), /checkout does not match/);
});

test('rejects a missing main reference instead of trusting a shallow checkout', (t) => {
  const { git, run } = fixture(t);
  git('tag', 'v1.2.3');
  git('update-ref', '-d', 'refs/remotes/origin/main');
  refused(run(), /not on origin\/main/);
});

test('rejects an AppVersion mismatch', (t) => {
  const { git, run } = fixture(t, { version: '1.2.4' });
  git('tag', 'v1.2.3');
  refused(run(), /does not match AppVersion/);
});

for (const [label, changelog, reason] of [
  ['missing entry', notes.replace('[1.2.3]', '[1.2.4]'), /exactly one/],
  ['duplicate entry', notes + '\n' + notes, /exactly one/],
  ['missing description', '## [1.2.3]\n\n- Change.\n', /needs a description/],
  ['empty notes', '## [1.2.3] - Release\n\n### Fixed\n\n## [1.2.2] - Older\n\n- Old.\n', /nonempty notes/],
]) {
  test(`rejects ${label}`, (t) => {
    const { git, run } = fixture(t, { changelog });
    git('tag', 'v1.2.3');
    refused(run(), reason);
  });
}

test('rejects malformed or missing tags', (t) => {
  const { run } = fixture(t);
  for (const tag of ['', 'vfoo.bar.baz', 'v1.2', 'v01.2.3', 'main']) {
    refused(run(tag), /expected a version tag/);
  }
  refused(run(), /cannot read release metadata/);
});

test('uses the committed version and notes even if the working files change', (t) => {
  const { cwd, git, run } = fixture(t);
  git('tag', 'v1.2.3');
  writeFileSync(join(cwd, 'internal/server/health_api.go'), 'changed worktree');
  writeFileSync(join(cwd, 'CHANGELOG.md'), 'changed worktree');
  const result = run();
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /Synthetic regression/);
});
