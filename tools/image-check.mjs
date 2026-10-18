// Verify the registry result against the digest produced by this release build.
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const required = ['linux/amd64', 'linux/arm/v7', 'linux/arm64'];
export function verifyImage(manifest, expectedDigest) {
  if (!/^sha256:[a-f0-9]{64}$/.test(expectedDigest) || manifest?.digest !== expectedDigest)
    throw new Error('published tag does not match the release build digest');
  if (!Array.isArray(manifest.manifests) || manifest.manifests.length === 0)
    throw new Error('published image has no platform manifests');
  const platforms = manifest.manifests.filter((entry) => !(
    entry.platform?.os === 'unknown' && entry.platform?.architecture === 'unknown' &&
    entry.annotations?.['vnd.docker.reference.type'] === 'attestation-manifest'
  )).map((entry) => {
    const p = entry.platform ?? {};
    // OCI may spell arm64's default variant explicitly as v8.
    const variant = p.architecture === 'arm64' && p.variant === 'v8' ? '' : p.variant;
    return [p.os, p.architecture, variant].filter(Boolean).join('/');
  }).sort();
  if (JSON.stringify(platforms) !== JSON.stringify(required))
    throw new Error('published image must contain exactly linux/amd64, linux/arm64 and linux/arm/v7');
  return platforms;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const [digest, path] = process.argv.slice(2);
    const platforms = verifyImage(JSON.parse(readFileSync(path, 'utf8')), digest);
    console.log(`image-check: ${digest} ${platforms.join(', ')}`);
  } catch (error) {
    console.error(`image-check: ${error.message}`);
    process.exitCode = 1;
  }
}
