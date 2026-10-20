import test from 'node:test';
import assert from 'node:assert/strict';
import {spawn,spawnSync} from 'node:child_process';
import {once} from 'node:events';
import * as fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {assertRuntimeStopped,hash,startTicks,verifyApp} from './subscriber-chr-proof.mjs';
const script=fileURLToPath(new URL('./subscriber-chr-runtime.mjs',import.meta.url));

test('CHR runtime refuses unscoped lifecycle arguments',()=>{
 for(const args of [[],['unknown','/tmp/unused'],['start','/tmp/unused'],['stop','relative'],['check','/tmp/unused','extra']]) {
  const result=spawnSync(process.execPath,[script,...args],{encoding:'utf8'});assert.equal(result.status,1);assert.match(result.stderr,/Usage:/);
 }
});

test('CHR VM teardown refuses a recorded live app without signalling it',()=>{
 const directory=fs.mkdtempSync(path.join(os.tmpdir(),'mikrodash-chr-runtime-'));fs.chmodSync(directory,0o700);
 try {
  assert.doesNotThrow(()=>assertRuntimeStopped(directory));
  fs.writeFileSync(path.join(directory,'app-owned.json'),JSON.stringify({pid:process.pid}),{mode:0o600});
  assert.throws(()=>assertRuntimeStopped(directory),/Stop the separately owned/);assert.ok(process.kill(process.pid,0));
 } finally {fs.rmSync(directory,{recursive:true});}
});

test('CHR process proof isolates PID, argv and configuration fences',async()=>{
 const directory=fs.mkdtempSync(path.join(os.tmpdir(),'mikrodash-chr-process-'));fs.chmodSync(directory,0o700);
 let child;
 try {
  const program=path.join(directory,'synthetic-process.cjs'),config=path.join(directory,'runtime-config.json'),fixture={data:path.join(directory,'runtime-data')};
  fs.writeFileSync(program,"process.stdout.write('ready');setInterval(()=>{},1000);",{mode:0o600});fs.writeFileSync(config,'{}',{mode:0o600});
  const args=[program,'-listen','127.0.0.1:13083','-data',fixture.data,'-subscriber-config',config];
  child=spawn(process.execPath,args,{stdio:['ignore','pipe','pipe']});await once(child.stdout,'data');
  const app={pid:child.pid,startTicks:startTicks(child.pid),argv:[process.execPath,...args],binarySHA256:hash(fs.readFileSync(`/proc/${child.pid}/exe`)),configSHA256:hash(fs.readFileSync(config))};
  assert.doesNotThrow(()=>verifyApp(directory,app,fixture));
  assert.throws(()=>verifyApp(directory,{...app,startTicks:(BigInt(app.startTicks)+1n).toString()},fixture));
  assert.throws(()=>verifyApp(directory,{...app,argv:[...app.argv,'unexpected']},fixture));
  fs.writeFileSync(config,'{"edited":true}');assert.throws(()=>verifyApp(directory,app,fixture));
  assert.doesNotThrow(()=>verifyApp(directory,app,fixture,false),'configuration drift must not strand an exactly owned process');
 } finally {if(child?.pid){child.kill('SIGTERM');await once(child,'exit');}fs.rmSync(directory,{recursive:true});}
});
