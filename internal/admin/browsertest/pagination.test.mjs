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
    if (!['index.html', 'app.js', 'app-core.mjs', 'app.css', 'enhancements.css'].includes(name)) return res.writeHead(404).end();
    res.setHeader('Content-Type', name.endsWith('html') ? 'text/html' : name.endsWith('css') ? 'text/css' : 'text/javascript');
    res.end(await readFile(new URL(`../web/${name}`, import.meta.url)));
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  origin = `http://127.0.0.1:${server.address().port}`;
  browser = await chromium.launch({ headless: true });
});
after(async () => { await browser?.close(); if (server) await new Promise(resolve => server.close(resolve)); });

function fixtures() {
  const mcpServers = {};
  const servers = [];
  for (let i = 1; i <= 15; i++) {
    const id = `service-${String(i).padStart(2, '0')}`;
    mcpServers[id] = { type: 'stdio', command: i === 15 ? 'special-search-command' : 'node', args: [`server-${i}.mjs`] };
    servers.push({ id, state: 'ready', publishedToolCount: 1, activeRevision: 1 });
  }
  const recentCalls = Array.from({ length: 45 }, (_, index) => ({
    tool: `tool-${index + 1}`,
    serverId: `service-${String((index % 15) + 1).padStart(2, '0')}`,
    outcome: index % 4 === 0 ? 'error' : 'success',
    durationMs: index + 1,
    time: new Date(Date.UTC(2026, 8, 8, 0, index)).toISOString(),
  }));
  return { mcpServers, servers, recentCalls };
}

async function setup(t) {
  const page = await browser.newPage();
  t.after(() => page.close());
  let authenticated = false;
  const data = fixtures();
  await page.route('**/api/admin/v1/**', route => {
    const path = new URL(route.request().url()).pathname;
    const respond = body => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body), headers: { ETag: '"pagination-fixture"' } });
    if (path.endsWith('/auth/login')) { authenticated = true; return respond({ csrfToken: 'pagination-fixture' }); }
    if (!authenticated) return route.fulfill({ status: 401, body: '{}' });
    if (path.endsWith('/auth/me')) return respond({ csrfToken: 'pagination-fixture' });
    if (path.endsWith('/config')) return respond({ mcpServers: data.mcpServers });
    if (path.endsWith('/tokens')) return respond({ tokens: [{ index: 0, token: 'pagination-token-00000000000000000000000001', legacy: true }] });
    if (path.endsWith('/status')) return respond({ version: 'test', servers: data.servers, recentCalls: data.recentCalls });
    return respond({});
  });
  await page.goto(origin);
  await page.locator('#token').fill('admin');
  await page.locator('#loginButton').click();
  await page.locator('#app').waitFor({ state: 'visible' });
  await page.locator('.server-card').first().waitFor();
  return page;
}

test('downstream list searches and paginates without losing configured services', async t => {
  const page = await setup(t);
  assert.equal(await page.locator('.server-card').count(), 12);
  assert.match(await page.locator('#serverPagination').textContent(), /第 1\/2 页/);
  await page.locator('#serverPagination button', { hasText: '下一页' }).click();
  assert.equal(await page.locator('.server-card').count(), 3);
  assert.match(await page.locator('#serverPagination').textContent(), /第 2\/2 页/);

  await page.locator('#serverSearch').fill('special-search-command');
  assert.equal(await page.locator('.server-card').count(), 1);
  assert.match(await page.locator('.server-card h3').textContent(), /service-15/);
  assert.match(await page.locator('#serverSummary').textContent(), /1 个匹配/);
});

test('recent calls paginate after search and outcome filters', async t => {
  const page = await setup(t);
  assert.equal(await page.locator('#calls tr').count(), 20);
  assert.match(await page.locator('#callPagination').textContent(), /第 1\/3 页/);
  await page.locator('#callPagination button', { hasText: '下一页' }).click();
  assert.equal(await page.locator('#calls tr').count(), 20);
  await page.locator('#callPagination button', { hasText: '下一页' }).click();
  assert.equal(await page.locator('#calls tr').count(), 5);

  await page.locator('#callOutcome').selectOption('error');
  assert.ok(await page.locator('#calls tr').count() > 0);
  assert.ok(await page.locator('#calls tr').count() <= 20);
  for (const badge of await page.locator('#calls .badge').allTextContents()) assert.notEqual(badge, '成功');
});
