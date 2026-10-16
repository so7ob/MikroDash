// Validate a checked-out release tag before publication; stdout is its release notes.
// Read version/notes from the tagged commit, never from uncommitted working files.
import { execFileSync } from 'node:child_process';

function git(...args) {
  return execFileSync('git', args, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
}
function requireRelease(condition, message) {
  if (!condition) throw new Error(message);
}

try {
  const tag = process.argv[2] ?? '';
  requireRelease(/^v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/.test(tag),
    'expected a version tag vX.Y.Z');
  const version = tag.slice(1);
  const commit = git('rev-parse', '--verify', `refs/tags/${tag}^{commit}`);
  requireRelease(commit === git('rev-parse', 'HEAD'), 'checkout does not match the release tag');
  try {
    git('merge-base', '--is-ancestor', commit, 'refs/remotes/origin/main');
  } catch {
    throw new Error('release tag is not on origin/main (fetch full history before checking)');
  }
  const source = git('show', `${commit}:internal/server/health_api.go`);
  const appVersion = source.match(/^const AppVersion = "([^"]+)"$/m)?.[1];
  requireRelease(appVersion === version, 'release tag does not match AppVersion');

  const changelog = git('show', `${commit}:CHANGELOG.md`);
  const headings = [...changelog.matchAll(/^## \[([^\]]+)\][^\r\n]*$/gm)];
  const matching = headings.filter((heading) => heading[1] === version);
  requireRelease(matching.length === 1, 'CHANGELOG must contain exactly one matching release entry');
  const heading = matching[0];
  const description = heading[0].slice(`## [${version}]`.length).replace(/^\s*[—–:-]\s*/, '').trim();
  requireRelease(description.length > 0, 'release entry needs a description');
  const next = headings.find((candidate) => candidate.index > heading.index);
  const body = changelog.slice(heading.index + heading[0].length, next?.index).trim();
  requireRelease(body.split('\n').some((line) => line.trim() && !/^\s*#/.test(line)),
    'release entry needs nonempty notes');
  process.stdout.write(`## ${description}\n\n${body}\n`);
} catch (error) {
  // Git errors can include command output; only our fixed validation messages are public.
  const message = error instanceof Error && !('status' in error)
    ? error.message : 'cannot read release metadata from git';
  process.stderr.write(`release-check: ${message}\n`);
  process.exitCode = 1;
}
