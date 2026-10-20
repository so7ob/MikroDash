import test from 'node:test';
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import * as fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
const script=fileURLToPath(new URL('./subscriber-chr-browser.mjs',import.meta.url));

test('CHR browser refuses unscoped commands before loading a browser',()=>{
 for(const args of [[],['unknown','/tmp/unused'],['journey','relative'],['exhausted','/tmp/unused','--unexpected'],['reconciling','/tmp/unused','--unexpected']]) {
  const result=spawnSync(process.execPath,[script,...args],{encoding:'utf8'});
  assert.equal(result.status,1);assert.match(result.stderr,/Usage:/);
 }
});

test('CHR browser refuses public or symlinked private state',()=>{
 const directory=fs.mkdtempSync(path.join(os.tmpdir(),'mikrodash-chr-browser-'));fs.chmodSync(directory,0o700);
 try {
  const manifest=path.join(directory,'runtime.json');fs.writeFileSync(manifest,'{}',{mode:0o644});
  let result=spawnSync(process.execPath,[script,'journey',directory],{encoding:'utf8'});assert.equal(result.status,1);assert.match(result.stderr,/Private regular lab metadata required/);
  fs.unlinkSync(manifest);const target=path.join(directory,'state.json');fs.writeFileSync(target,'{}',{mode:0o600});fs.symlinkSync(target,manifest);
  for(const action of ['exhausted','reconciling']) {
   result=spawnSync(process.execPath,[script,action,directory],{encoding:'utf8'});assert.equal(result.status,1);assert.match(result.stderr,/Private regular lab metadata required/);
  }
 } finally {fs.rmSync(directory,{recursive:true});}
});
