// Integrated smoke: real Hub binary, cookies, CSRF, CAS, persistence and Chromium.
import assert from 'node:assert/strict';
import { mkdtemp, writeFile, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { createServer } from 'node:net';
import { spawn } from 'node:child_process';
import { chromium } from 'playwright';

const root = resolve(import.meta.dirname, '../../..');
const dir = await mkdtemp(join(tmpdir(), 'mcp-hub-live-'));
const allocator = createServer();
await new Promise(r => allocator.listen(0, '127.0.0.1', r));
const port = allocator.address().port;
await new Promise(r => allocator.close(r));
const origin = `http://127.0.0.1:${port}`;
const configPath = join(dir, 'config.json');
const args = ['  padded  ', '', 'normal', 'line1\nline2', ''];
await writeFile(configPath, JSON.stringify({version:1,hub:{listen:`127.0.0.1:${port}`,admin:{enabled:true,token:'live-smoke-admin'}},mcpServers:{original:{enabled:false,type:'stdio',command:'node',args,env:{API_KEY:'original-secret'}}}}));
const child = spawn(process.env.MCP_HUB_BINARY || join(root, 'dist/mcp-hub'), ['serve','--config',configPath], {stdio:['ignore','pipe','pipe']});
let spawnError;
child.on('error', error => { spawnError = error; });
let logs=''; child.stdout.on('data',d=>logs+=d); child.stderr.on('data',d=>logs+=d);
let browser;
try {
 const deadline=Date.now()+15000;
 while(true){
  if(spawnError) throw spawnError;
  if(child.exitCode!==null) throw Error(`Hub exited: ${logs}`);
  try{if((await fetch(`${origin}/healthz`)).ok)break;}catch{}
  if(Date.now()>deadline)throw Error(`Hub readiness timeout: ${logs}`);
  await new Promise(r=>setTimeout(r,100));
 }
 browser=await chromium.launch({headless:true});
 const context=await browser.newContext();
 const page=await context.newPage();
 const errors=[]; page.on('pageerror',e=>errors.push(e.message));
 await page.goto(`${origin}/admin/`);
 await page.locator('#token').fill('live-smoke-admin');
 await page.locator('#loginButton').click();
 await page.locator('[data-action="edit"]').waitFor();
 const cookies=await context.cookies();
 assert(cookies.some(c=>c.httpOnly&&c.sameSite==='Strict'));
 await page.locator('[data-action="edit"]').click();
 await page.locator('#jsonTab').click();
 assert.deepEqual(JSON.parse(await page.locator('#serverJSON').inputValue()).args,args);
 await page.locator('#formTab').click();
 await page.locator('#saveServer').click();
 await page.waitForFunction(()=>!document.querySelector('#editor').open);
 let saved=JSON.parse(await readFile(configPath,'utf8'));
 assert.deepEqual(saved.mcpServers.original.args,args);
 assert.equal(saved.mcpServers.original.env.API_KEY,'original-secret');
 await page.locator('[data-action="duplicate"]').click();
 await page.locator('#saveServer').click();
 assert.match(await page.locator('#editorError').textContent(),/重新填写/);
 await page.locator('#envRows .kv-value').fill('replacement-secret');
 await page.locator('#saveServer').click();
 await page.waitForFunction(()=>!document.querySelector('#editor').open);
 saved=JSON.parse(await readFile(configPath,'utf8'));
 const copied=Object.entries(saved.mcpServers).find(([id])=>id!=='original');
 assert(copied); assert.equal(copied[1].env.API_KEY,'replacement-secret');
 assert.deepEqual(copied[1].args,args);
 const csrfRejected=await context.request.put(`${origin}/api/admin/v1/servers/original`,{data:saved.mcpServers.original});
 assert.equal(csrfRejected.status(),403);
 saved.hub.admin.token='rotated-live-admin';
 await writeFile(configPath,JSON.stringify(saved));
 await page.waitForFunction(()=>document.querySelector('#health').textContent==='需重启',null,{timeout:20000});
 assert.match(await page.locator('#healthDetail').textContent(),/认证|安全/);
 assert.deepEqual(errors,[]);
 console.log('PASS real Hub + Chromium: login cookie flags, edit/JSON argument roundtrip, secret preservation, duplicate replacement persisted, missing-CSRF rejection, no page errors');
} finally {
 await browser?.close();
 if(child.pid && child.exitCode===null){
  const exited=new Promise(r=>child.once('exit',r));
  child.kill('SIGTERM');
  const killTimer=setTimeout(()=>child.kill('SIGKILL'),5000);
  await exited; clearTimeout(killTimer);
 }
 await rm(dir,{recursive:true,force:true});
}
