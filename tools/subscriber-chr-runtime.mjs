// Starts/stops only a normal MikroDash process using the owned CHR fixture.
import {spawn,execFileSync} from 'node:child_process';
import {randomBytes} from 'node:crypto';
import * as fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {setTimeout as delay} from 'node:timers/promises';
import {hash,readPrivateJSON,runtimeFixture,runtimeIdentity,startTicks,verifyApp,verifyRuntimeListeners} from './subscriber-chr-proof.mjs';

const root=path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const [action,directory,binary,...extra]=process.argv.slice(2);
if(!['start','stop','check'].includes(action)||!directory||!path.isAbsolute(directory)||extra.length||(action==='start'?(!binary||!path.isAbsolute(binary)):binary)) {
 console.error('Usage: node tools/subscriber-chr-runtime.mjs start|stop|check /absolute/private/lab [absolute-binary-for-start]');process.exit(1);
}
process.umask(0o077);
const receiptPath=path.join(directory,'app-owned.json');
const save=(file,value)=>fs.writeFileSync(file,JSON.stringify(value,null,2),{mode:0o600,flag:'wx'});
const token=()=>randomBytes(6).toString('hex');
const checkVM=()=>execFileSync(process.execPath,[path.join(root,'tools/subscriber-chr-lab.mjs'),'check',directory],{stdio:'pipe',timeout:10000});
try {
 checkVM();const {fixture}=action==='stop'?runtimeIdentity(directory):runtimeFixture(directory);
 if(action==='check') {
  const app=readPrivateJSON(directory,'app-owned.json');verifyApp(directory,app,fixture);verifyRuntimeListeners(app.pid);
  console.log('Owned MikroDash runtime, VM and four isolated listeners verified.');
 } else if(action==='stop') {
  const app=readPrivateJSON(directory,'app-owned.json');verifyApp(directory,app,fixture,false);
  // Recheck PID reuse immediately before signalling. Never signal another app.
  if(startTicks(app.pid)!==app.startTicks)throw Error('Runtime process identity changed');
  process.kill(app.pid,'SIGTERM');
  let ended=false;
  for(let i=0;i<100;i++) {
   try {
    const stat=fs.readFileSync(`/proc/${app.pid}/stat`,'utf8'), fields=stat.slice(stat.lastIndexOf(')')+1).trim().split(/\s+/);
    if(fields[0]==='Z'||fields[19]!==app.startTicks){ended=true;break;}
   } catch(e) {if(e.code==='ENOENT'){ended=true;break;}throw e;}
   await delay(100);
  }
  if(!ended)throw Error('Owned app has not stopped; VM and receipt retained');
  const archive=path.join(directory,`app-stopped-${app.pid}-${token()}.json`);fs.renameSync(receiptPath,archive);
  save(path.join(directory,`runtime-stop-${token()}.json`),{synthetic:true,pid:app.pid,startTicks:app.startTicks,at:new Date().toISOString(),receipt:archive});
  console.log('Stopped only the owned MikroDash runtime; ledger, VM and private evidence retained.');
 } else {
  if(process.env.DATA_SECRET)throw Error('Use an independent fixture data secret; DATA_SECRET must be unset');
  const file=fs.lstatSync(binary);if(!file.isFile()||file.isSymbolicLink())throw Error('Regular built MikroDash binary required');
  const header=Buffer.alloc(4),fd=fs.openSync(binary,'r');try{fs.readSync(fd,header,0,4,0);}finally{fs.closeSync(fd);}
  if(!header.equals(Buffer.from([0x7f,0x45,0x4c,0x46])))throw Error('Linux MikroDash ELF required');
  if(fs.existsSync(receiptPath)) {
   const old=readPrivateJSON(directory,'app-owned.json');if(!Number.isSafeInteger(old.pid)||old.pid<=1||fs.existsSync(`/proc/${old.pid}`))throw Error('Existing app receipt requires inspection/stop; no takeover');
   fs.renameSync(receiptPath,path.join(directory,`app-retired-${old.pid}-${token()}.json`));
  }
  const args=['-listen','127.0.0.1:13083','-data',fixture.data,'-web',path.join(root,'web/dist'),'-static',path.join(root,'web/public'),'-geo',path.join(root,'.temp/geo'),'-no-pool','-subscriber-config',path.join(directory,'runtime-config.json')];
  const log=path.join(directory,`runtime-app-${token()}.log`),logFD=fs.openSync(log,'wx',0o600);
  const child=spawn(binary,args,{cwd:root,detached:true,stdio:['ignore',logFD,logFD]});fs.closeSync(logFD);let failure;child.on('error',e=>{failure=e;});child.unref();
  await delay(100);if(failure||!child.pid)throw Error('MikroDash failed to spawn; inspect retained runtime log');
  const app={pid:child.pid,startTicks:startTicks(child.pid),argv:[binary,...args],binarySHA256:hash(fs.readFileSync(binary)),configSHA256:hash(fs.readFileSync(path.join(directory,'runtime-config.json'))),log};
  try {verifyApp(directory,app,fixture);save(receiptPath,app);} catch(e) {
   // This child was created here; an exact identity check is still required.
   verifyApp(directory,app,fixture);process.kill(app.pid,'SIGTERM');throw e;
  }
  let ready=false;
  for(let i=0;i<100;i++) {
   verifyApp(directory,app,fixture);
   try {verifyRuntimeListeners(app.pid);const response=await fetch('http://127.0.0.1:13083/api/auth/status',{signal:AbortSignal.timeout(1000)});await response.body?.cancel();if(response.ok){ready=true;break;}}catch{}
   await delay(100);
  }
  if(!ready)throw Error('Owned runtime readiness not confirmed; inspect/stop using the retained ownership receipt');
  console.log(`Owned MikroDash ${action} ready, pid ${app.pid}; private runtime log retained.`);
 }
} catch(e) {console.error(e.message);process.exitCode=1;}
