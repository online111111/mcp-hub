import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { chromium } from 'playwright';

let browser, server, origin;
before(async () => {
  server = createServer(async (req, res) => {
    const pathname = new URL(req.url, 'http://localhost').pathname;
    const name = pathname === '/' ? 'index.html' : pathname.replace(/^\/admin\//, '');
    if (!['index.html', 'app.js', 'app-core.mjs', 'app.css'].includes(name)) {
      res.writeHead(404).end(); return;
    }
    res.setHeader('Content-Type', name.endsWith('html') ? 'text/html' : name.endsWith('css') ? 'text/css' : 'text/javascript');
    res.end(await readFile(new URL(`../web/${name}`, import.meta.url)));
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  origin = `http://127.0.0.1:${server.address().port}`;
  browser = await chromium.launch({ headless: true });
});
after(async () => { await browser?.close(); await new Promise(resolve => server.close(resolve)); });

// Real Chromium and production DOM/modules; only the admin API boundary is stubbed.
async function setup(t, source) {
  const page = await browser.newPage();
  t.after(() => page.close());
  const writes = [], errors = [];
  const apiState = { authenticated: false };
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/admin/v1/**', async route => {
    const req = route.request(), path = new URL(req.url()).pathname;
    const respond = (body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body), headers: { ETag: '"revision-1"' } });
    if (path.endsWith('/auth/login')) {
      if (req.postDataJSON().token !== 'admin-token') return respond({ error: 'invalid token' }, 401);
      apiState.authenticated = true; return respond({ csrfToken: 'csrf-test' });
    }
    if (!apiState.authenticated) return respond({}, 401);
    if (path.endsWith('/auth/me')) return respond({ csrfToken: 'csrf-test' });
    if (path.endsWith('/config')) return respond({ mcpServers: { original: source } });
    if (path.endsWith('/status')) return respond({ servers: [], recentCalls: [] });
    if (req.method() === 'PUT') {
      writes.push({ body: req.postDataJSON(), headers: req.headers(), path }); return respond({});
    }
    return respond({});
  });
  await page.goto(origin);
  await page.locator('#token').fill('admin-token');
  await page.locator('#loginButton').click();
  await page.locator('[data-action="edit"]').waitFor();
  return { page, writes, errors, apiState };
}

test('clicking the active JSON tab keeps unsaved JSON; malformed JSON cannot switch', async t => {
  const { page } = await setup(t, { type: 'stdio', command: 'node' });
  await page.locator('[data-action="edit"]').click();
  await page.locator('#jsonTab').click();
  const draft = '{"type":"stdio","command":"other","args":[""]}';
  await page.locator('#serverJSON').fill(draft);
  await page.locator('#jsonTab').click();
  assert.equal(await page.locator('#serverJSON').inputValue(), draft);
  await page.locator('#serverJSON').fill('{');
  await page.locator('#formTab').click();
  assert.equal(await page.locator('#jsonEditor').isVisible(), true);
  assert.match(await page.locator('#editorError').textContent(), /JSON/);
  await page.locator('#cancelEditor').click();
  await page.locator('[data-action="edit"]').click();
  assert.equal(await page.locator('#command').inputValue(), 'node');
});

test('expired login closes the modal and exposes the login form', async t => {
  const { page, apiState } = await setup(t, { type: 'stdio', command: 'node' });
  await page.locator('[data-action="edit"]').click();
  apiState.authenticated = false;
  await page.locator('#saveServer').click();
  await page.locator('#login').waitFor({ state: 'visible' });
  assert.equal(await page.locator('#editor').evaluate(el => el.open), false);
  await page.locator('#token').fill('bad-token');
  await page.locator('#loginButton').click();
  await page.waitForFunction(() => document.querySelector('#loginError').textContent.length > 0);
  assert.equal(await page.locator('#loginButton').isEnabled(), true);
  await page.locator('#token').fill('admin-token');
  await page.locator('#loginButton').click();
  await page.locator('#app').waitFor({ state: 'visible' });
});

test('duplicate key errors stay in the editor when switching to JSON', async t => {
  const { page, errors } = await setup(t, { type: 'stdio', command: 'node', env: { SAME: 'one' } });
  await page.locator('[data-action="edit"]').click();
  await page.locator('#addEnv').click();
  await page.locator('#envRows .kv-key').last().fill('SAME');
  await page.locator('#jsonTab').click();
  assert.match(await page.locator('#editorError').textContent(), /重复/);
  assert.equal(await page.locator('#formEditor').isVisible(), true);
  assert.deepEqual(errors, []);
});

const secret = '__MCP_HUB_SECRET_SET__';
for (const [field, source, rowSelector] of [
  ['env', { type: 'stdio', command: 'node', env: { API_KEY: secret } }, '#envRows'],
  ['headers', { type: 'streamable_http', url: 'https://example.com/mcp', headers: { Authorization: secret } }, '#headerRows'],
]) {
  test(`copy ${field} secrets requires replacement in form and JSON`, async t => {
    const { page, writes } = await setup(t, source);
    await page.locator('[data-action="duplicate"]').click();
    assert.match(await page.locator(`${rowSelector} .kv-secret-label`).textContent(), /重新填写/);
    await page.locator('#saveServer').click();
    assert.match(await page.locator('#editorError').textContent(), /重新填写/);
    assert.equal(writes.length, 0);
    await page.locator('#jsonTab').click();
    await page.locator('#saveServer').click();
    assert.match(await page.locator('#editorError').textContent(), /重新填写/);
    assert.equal(writes.length, 0);
    await page.locator('#formTab').click();
    await page.locator(`${rowSelector} .kv-value`).fill('new-secret');
    await page.locator('#saveServer').click();
    await page.waitForFunction(() => !document.querySelector('#editor').open);
    assert.deepEqual(writes[0].body[field], Object.fromEntries(Object.keys(source[field]).map(key => [key, 'new-secret'])));
  });
}

const args = ['  padded  ', '', 'normal', 'line1\nline2', 'windows\r\nline', ''];
test('stdio arguments survive form, JSON and save with per-item editing', async t => {
  const { page, writes, errors } = await setup(t, { type: 'stdio', command: 'node', args });
  await page.locator('[data-action="edit"]').click();
  await page.locator('#jsonTab').click();
  assert.deepEqual(JSON.parse(await page.locator('#serverJSON').inputValue()).args, args);
  await page.locator('#formTab').click();
  assert.equal(await page.locator('#argRows textarea').count(), args.length);
  await page.locator('#argRows textarea').nth(2).fill(' changed\nargument ');
  await page.locator('#addArg').click();
  await page.locator('#saveServer').click();
  await page.waitForFunction(() => !document.querySelector('#editor').open);
  assert.deepEqual(writes[0].body.args, [args[0], args[1], ' changed\nargument ', ...args.slice(3), '']);
  assert.equal(writes[0].headers['x-csrf-token'], 'csrf-test');
  assert.equal(writes[0].headers['if-match'], '"revision-1"');
  assert.deepEqual(errors, []);
});
