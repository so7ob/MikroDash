#!/usr/bin/env node
// Development-only disposable CHR lifecycle. No operator store or physical NIC.
import { spawn, execFileSync } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import * as fs from 'node:fs';
import net from 'node:net';
import path from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';

const root = '/dev/shm/mikrodash-subscriber-chr-lab';
const names = ['md-lab-br', 'md-lab-tap', 'md-lab-host'];
const namespace = 'md-subscriber-lab';
const [action, directory] = process.argv.slice(2);
if (!['start', 'stop', 'check'].includes(action) || !directory || !path.isAbsolute(directory) || (action !== 'check' && process.geteuid() !== 0)) {
  console.error('Usage (Linux; start/stop require root): node tools/subscriber-chr-lab.mjs start|stop|check /absolute/private/directory');
  process.exit(1);
}
const statePath = path.join(directory, 'runtime.json');
const ip = (...args) => execFileSync('ip', args, { timeout: 5000, stdio: ['ignore', 'pipe', 'pipe'] });
const save = (file, value) => fs.writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600, flag: 'wx' });
const startTicks = pid => {
  const stat = fs.readFileSync(`/proc/${pid}/stat`, 'utf8');
  return stat.slice(stat.lastIndexOf(')') + 1).trim().split(/\s+/)[19];
};

function privateFile(file) {
  const info = fs.lstatSync(file);
  if (!info.isFile() || (info.mode & 0o777) !== 0o600 || info.size > 65536) throw new Error('Private regular lab file required');
  return JSON.parse(fs.readFileSync(file, 'utf8'));
}

function vmArgs(state) {
  return [`${root}/root/usr/bin/qemu-system-x86_64`, '-name', state.name, '-machine', 'pc,accel=tcg', '-cpu', 'max',
    '-m', '768', '-smp', '1', '-rtc', 'base=utc', '-display', 'none', '-monitor', 'none',
    '-serial', `unix:${state.console},server=on,wait=off`, '-L', `${root}/root/usr/share/qemu`,
    '-bios', `${root}/root/usr/share/seabios/bios-256k.bin`, '-drive', `file=${state.disk},format=raw,if=ide`,
    '-netdev', 'user,id=wan,hostfwd=tcp:127.0.0.1:18728-:8728,hostfwd=udp:127.0.0.1:13799-:3799',
    '-device', 'e1000,netdev=wan,mac=02:00:00:00:54:11,romfile=',
    '-netdev', 'tap,id=client,ifname=md-lab-tap,script=no,downscript=no',
    '-device', 'e1000,netdev=client,mac=02:00:00:00:54:12,romfile='];
}

function ownedProcess(state) {
  if (!/^mikrodash-subscriber-chr-[0-9a-f]{8}$/.test(state.name) || state.namespace !== namespace ||
      JSON.stringify(state.interfaces) !== JSON.stringify(names) || state.apiPort !== 18728 || state.disconnectPort !== 13799 ||
      path.dirname(path.dirname(state.disk)) !== root || !/^run-[0-9a-f]{8}$/.test(path.basename(path.dirname(state.disk))) ||
      path.basename(state.disk) !== 'disk.img' || state.console !== path.join(directory, 'console.sock')) throw new Error('Unexpected lab ownership state');
  const command = fs.readFileSync(`/proc/${state.pid}/cmdline`).toString().replace(/\0$/, '').split('\0');
  if (!state.startTicks || startTicks(state.pid) !== state.startTicks || JSON.stringify(command) !== JSON.stringify(state.args) || JSON.stringify(command) !== JSON.stringify(vmArgs(state)) ||
      command[0] !== `${root}/root/usr/bin/qemu-system-x86_64` || command[command.indexOf('-name') + 1] !== state.name ||
      !command.includes(`file=${state.disk},format=raw,if=ide`)) throw new Error('Owned QEMU process proof failed');
}

async function stop() {
  const state = privateFile(statePath);
  ownedProcess(state);
  const ports = fs.readdirSync('/sys/class/net/md-lab-br/brif').sort();
  if (JSON.stringify(ports) !== JSON.stringify(['md-lab-host', 'md-lab-tap'])) throw new Error('Unexpected bridge member; refusing cleanup');
  process.kill(state.pid, 'SIGTERM');
  for (let i = 0; i < 50 && fs.existsSync(`/proc/${state.pid}`); i++) await delay(100);
  if (fs.existsSync(`/proc/${state.pid}`)) throw new Error('QEMU still running; refusing network cleanup');
  ip('netns', 'delete', namespace);
  ip('link', 'delete', 'md-lab-tap');
  ip('link', 'delete', 'md-lab-br');
  save(path.join(directory, 'stopped.json'), { at: new Date().toISOString(), name: state.name });
  console.log('Stopped only the owned QEMU and isolated lab network; private evidence retained.');
}

async function initializeConsole(socketPath, password) {
  for (let i = 0; i < 300 && !fs.existsSync(socketPath); i++) await delay(100);
  const socket = net.createConnection(socketPath);
  let received = '';
  let failure;
  socket.setEncoding('utf8');
  socket.on('data', text => { received = (received + text).slice(-65536); });
  socket.on('error', err => { failure = err; });
  async function expect(text) {
    for (let i = 0; i < 600; i++) {
      if (failure || socket.destroyed) throw new Error('Owned lab console unavailable');
      const at = received.indexOf(text);
      if (at >= 0) { received = received.slice(at + text.length); return; }
      await delay(100);
    }
    throw new Error(`Owned lab console did not reach ${text}`);
  }
  try {
    socket.write('\r');
    await expect('Login:'); socket.write('admin\r');
    await expect('Password:'); socket.write('\r');
    await expect('[Y/n]:'); socket.write('n\r');
    await expect('new password>'); socket.write(`${password}\r`);
    await expect('repeat new password>'); socket.write(`${password}\r`);
    await expect('Password changed');
  } finally {
    socket.destroy();
  }
}

async function start() {
  if (fs.existsSync(statePath)) throw new Error('Existing runtime state; refuse to overwrite it');
  for (const name of names) if (fs.existsSync(`/sys/class/net/${name}`)) throw new Error('Existing lab interface; no takeover allowed');
  if (fs.existsSync(`/run/netns/${namespace}`)) throw new Error('Existing lab namespace; no takeover allowed');
  const image = `${root}/chr-7.24.5.img`;
  const imageInfo = fs.lstatSync(image);
  if (!imageInfo.isFile() || imageInfo.size !== 134217728) throw new Error('Expected extracted official CHR 7.24.5 image');
  const run = fs.mkdtempSync(`${root}/run-`);
  // The guard pins hexadecimal ownership tokens, independently of mkdtemp's suffix.
  const runPath = `${root}/run-${randomBytes(4).toString('hex')}`;
  fs.renameSync(run, runPath); fs.chmodSync(runPath, 0o700);
  const disk = `${runPath}/disk.img`;
  fs.copyFileSync(image, disk, fs.constants.COPYFILE_EXCL); fs.chmodSync(disk, 0o600);
  const name = `mikrodash-subscriber-chr-${randomBytes(4).toString('hex')}`;
  const consolePath = path.join(directory, 'console.sock');
  const args = vmArgs({ name, disk, console: consolePath });
  let state;
  const created = [];
  try {
    ip('netns', 'add', namespace); created.push(['netns', 'delete', namespace]);
    ip('link', 'add', 'md-lab-br', 'type', 'bridge'); created.push(['link', 'delete', 'md-lab-br']);
    ip('link', 'add', 'md-lab-host', 'type', 'veth', 'peer', 'name', 'md-lab-client'); created.push(['link', 'delete', 'md-lab-host']);
    ip('link', 'set', 'md-lab-client', 'netns', namespace);
    ip('link', 'set', 'md-lab-host', 'master', 'md-lab-br');
    for (const device of ['md-lab-host', 'md-lab-br']) ip('link', 'set', 'dev', device, 'addrgenmode', 'none');
    ip('link', 'set', 'md-lab-host', 'up'); ip('link', 'set', 'md-lab-br', 'up');
    ip('tuntap', 'add', 'dev', 'md-lab-tap', 'mode', 'tap'); created.push(['link', 'delete', 'md-lab-tap']);
    ip('link', 'set', 'md-lab-tap', 'master', 'md-lab-br'); ip('link', 'set', 'dev', 'md-lab-tap', 'addrgenmode', 'none'); ip('link', 'set', 'md-lab-tap', 'up');
    ip('-n', namespace, 'link', 'set', 'lo', 'up');
    ip('-n', namespace, 'link', 'set', 'md-lab-client', 'address', '02:00:00:00:54:02');
    ip('-n', namespace, 'address', 'add', '198.51.100.2/24', 'dev', 'md-lab-client');
    ip('-n', namespace, 'link', 'set', 'md-lab-client', 'up');
    ip('-n', namespace, 'route', 'add', 'default', 'via', '198.51.100.1');
    const fd = fs.openSync(path.join(directory, 'qemu.log'), 'wx', 0o600);
    const child = spawn(args[0], args.slice(1), { detached: true, stdio: ['ignore', fd, fd],
      env: { ...process.env, LD_LIBRARY_PATH: `${root}/root/usr/lib/x86_64-linux-gnu`, QEMU_MODULE_DIR: `${root}/root/usr/lib/x86_64-linux-gnu/qemu` } });
    fs.closeSync(fd);
    let spawnError;
    child.on('error', err => { spawnError = err; }); child.unref();
    await delay(200);
    if (spawnError || !child.pid) throw new Error('QEMU did not start');
    state = { pid: child.pid, name, disk, console: consolePath, namespace, interfaces: names, apiPort: 18728, disconnectPort: 13799, args, startTicks: startTicks(child.pid) };
    ownedProcess(state); save(statePath, state);
    const password = randomBytes(32).toString('base64url');
    save(path.join(directory, 'credentials.json'), { Username: 'admin', Password: password });
    save(path.join(directory, 'fixture.json'), { secret: randomBytes(32).toString('hex'), password: randomBytes(32).toString('base64url') });
    await initializeConsole(consolePath, password);
    console.log('Owned isolated CHR initialized. Run subscriberchr inspection before configuration.');
  } catch (err) {
    if (state) {
      ownedProcess(state); process.kill(state.pid, 'SIGTERM');
      for (let i = 0; i < 50 && fs.existsSync(`/proc/${state.pid}`); i++) await delay(100);
      if (fs.existsSync(`/proc/${state.pid}`)) throw new Error('Failed start left owned QEMU running; network retained for explicit inspection');
    }
    if (fs.existsSync('/sys/class/net/md-lab-br/brif') && fs.readdirSync('/sys/class/net/md-lab-br/brif').some(port => !names.includes(port))) {
      throw new Error('Unexpected bridge port; failed-start network retained for inspection');
    }
    const failures = [];
    for (const args of created.reverse()) { try { ip(...args); } catch { failures.push(args.join(' ')); } }
    if (failures.length) throw new Error(`Failed start: ${err.message}; cleanup also failed for: ${failures.join(', ')}`);
    throw err;
  }
}

try {
  if (fs.existsSync(directory)) {
    const info = fs.lstatSync(directory);
    if (!info.isDirectory() || (info.mode & 0o777) !== 0o700) throw new Error('Private non-symlink directory required');
  } else {
    if (action !== 'start') throw new Error('No private lab directory');
    fs.mkdirSync(directory, { mode: 0o700 });
  }
  if (action === 'check') {
    ownedProcess(privateFile(statePath));
    console.log('Owned QEMU process identity verified; no changes made.');
  } else {
    await (action === 'start' ? start() : stop());
  }
} catch (err) {
  console.error(err.message);
  process.exitCode = 1;
}
