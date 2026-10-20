// Linux-only ownership checks shared by the disposable CHR development tools.
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import * as fs from 'node:fs';
import path from 'node:path';

export const hash=raw=>createHash('sha256').update(raw).digest('hex');
export function readPrivateJSON(directory,name) {
 const dir=fs.lstatSync(directory), file=path.join(directory,name), stat=fs.lstatSync(file);
 if(!dir.isDirectory()||(dir.mode&0o777)!==0o700||!stat.isFile()||(stat.mode&0o777)!==0o600||stat.size>65536)throw Error('Private regular lab metadata required');
 try{return JSON.parse(fs.readFileSync(file,'utf8'));}catch{throw Error('Invalid private lab metadata');}
}
export function startTicks(pid) {
 const stat=fs.readFileSync(`/proc/${pid}/stat`,'utf8');return stat.slice(stat.lastIndexOf(')')+1).trim().split(/\s+/)[19];
}
export function runtimeIdentity(directory) {
 const state=readPrivateJSON(directory,'runtime.json'), fixture=readPrivateJSON(directory,'runtime-fixture.json');
 assert.equal(state.nativePortal,true);assert.equal(fixture.vm,state.name);assert.equal(fixture.data,path.join(directory,'runtime-data'));
 return {state,fixture};
}
export function runtimeFixture(directory,requirePilot=false) {
 const {state,fixture}=runtimeIdentity(directory), cfg=readPrivateJSON(directory,'runtime-config.json');
 assert.ok(['shadow','pilot'].includes(cfg.mode));if(requirePilot)assert.equal(cfg.mode,'pilot');
 assert.equal(cfg.portalListen,'198.51.100.10:9444');assert.equal(cfg.portalOrigin,'https://portal.example.test:9444');
 assert.equal(cfg.authListen,'127.0.0.1:12812');assert.equal(cfg.accountingListen,'127.0.0.1:12813');assert.equal(cfg.clients.length,1);
 assert.equal(cfg.clients[0].routerId,fixture.routerId);assert.equal(cfg.clients[0].network,'C');assert.equal(cfg.clients[0].loginUrl,'https://login.example.test/login');
 assert.equal(cfg.clients[0].address,'127.0.0.1');assert.equal(cfg.clients[0].disconnectPort,13799);assert.equal(cfg.clients[0].calledStationId,'hs-lab');
 const routers=readPrivateJSON(directory,'runtime-data/routers.json');assert.equal(routers.length,1);assert.equal(routers[0].id,fixture.routerId);assert.equal(routers[0].host,'127.0.0.1');assert.equal(routers[0].port,18728);
 if(cfg.mode==='pilot') {
  assert.equal(cfg.allowCoA,true);assert.equal(cfg.allowDisconnect,true);assert.equal(cfg.localLimitsVerified,true);
  assert.equal(cfg.clients[0].capabilities.evidence,path.join(directory,'pilot-capability-review.json'));
  const review=readPrivateJSON(directory,'pilot-capability-review.json');assert.equal(review.vm,state.name);assert.equal(review.startTicks,state.startTicks);assert.equal(review.version,cfg.clients[0].capabilities.version);
  assert.ok(Object.keys(review.sha256).length>0,'capability evidence set must not be empty');
  for(const [name,digest] of Object.entries(review.sha256)) {assert.match(name,/^[a-z0-9-]+$/);assert.match(digest,/^[a-f0-9]{64}$/);assert.equal(hash(fs.readFileSync(path.join(directory,name+'.jsonl'))),digest);}
 } else {assert.equal(cfg.allowCoA,false);assert.equal(cfg.allowDisconnect,false);assert.equal(cfg.localLimitsVerified,false);}
 return {state,fixture,cfg};
}
export function verifyApp(directory,app,fixture,checkConfig=true) {
 assert.ok(Number.isSafeInteger(app.pid)&&app.pid>1);assert.equal(startTicks(app.pid),app.startTicks);
 const command=fs.readFileSync(`/proc/${app.pid}/cmdline`,'utf8').replace(/\0$/,'').split('\0');assert.deepEqual(command,app.argv);
 assert.equal(command[command.indexOf('-data')+1],fixture.data);assert.equal(command[command.indexOf('-subscriber-config')+1],path.join(directory,'runtime-config.json'));
 assert.equal(command[command.indexOf('-listen')+1],'127.0.0.1:13083');
 assert.equal(hash(fs.readFileSync(`/proc/${app.pid}/exe`)),app.binarySHA256);
 if(checkConfig)assert.equal(hash(fs.readFileSync(path.join(directory,'runtime-config.json'))),app.configSHA256);
}
export function verifyRuntimeListeners(pid) {
 const sockets=new Set(fs.readdirSync(`/proc/${pid}/fd`).flatMap(fd=>{try{return [fs.readlinkSync(`/proc/${pid}/fd/${fd}`)];}catch{return [];}}));
 for(const [file,address] of [['tcp','0100007F:331B'],['tcp','0A6433C6:24E4'],['udp','0100007F:320C'],['udp','0100007F:320D']]) {
  const rows=fs.readFileSync(`/proc/net/${file}`,'utf8').trim().split('\n').slice(1).map(row=>row.trim().split(/\s+/));
  assert.ok(rows.some(row=>row[1]===address&&(file==='udp'||row[3]==='0A')&&sockets.has(`socket:[${row[9]}]`)),`owned ${file} listener missing`);
 }
}
export function assertRuntimeStopped(directory) {
 const file=path.join(directory,'app-owned.json');if(!fs.existsSync(file))return;
 const app=readPrivateJSON(directory,'app-owned.json');
 if(!Number.isSafeInteger(app.pid)||app.pid<=1)throw Error('Invalid app ownership receipt');
 if(fs.existsSync(`/proc/${app.pid}`))throw Error('Stop the separately owned MikroDash runtime before stopping its VM');
}
