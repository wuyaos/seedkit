const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const source = fs.readFileSync(path.join(__dirname, '../web/static/app.js'), 'utf8');

async function page(storage) {
  const sandbox = {
    localStorage: { getItem: k => storage.get(k) ?? null, setItem: (k, v) => storage.set(k, v) },
    document: { addEventListener() {} },
    matchMedia: () => ({ addEventListener() {} }), addEventListener() {},
  };
  vm.createContext(sandbox);
  vm.runInContext(source + '\nglobalThis.testApp = app;', sandbox);
  const a = sandbox.testApp;
  a.applyUI = () => {}; a.status = async () => {}; a.$watch = () => {}; a.loadRes = () => {};
  a.$refs = {}; a.$nextTick = fn => fn(); a.run = () => {};
  await a.init();
  a.st.tools = { ffmpeg: { found: true } };
  return a;
}
function select(a, kind, isDir = false) {
  a.cur = { kind, path: 'media/test', isDir }; a.onCur();
}
(async () => {
  const storage = new Map();
  const a = await page(storage);
  select(a, 'video');
  assert.equal(a.ops.mediainfo, true);
  assert.equal(a.ops.screenshot, true);
  a.toggleOp('screenshot'); a.toggleOp('torrent');
  assert.equal(a.ops.screenshot, false); assert.equal(a.ops.torrent, false);
  select(a, 'video');
  assert.equal(a.ops.screenshot, false); assert.equal(a.ops.torrent, false);
  select(a, 'bdmv', true);
  assert.equal(a.ops.bdinfo, true);
  a.toggleOp('bdinfo');
  select(a, 'video');
  const saved = storage.get('sk.ops');
  a.toggleOp('bdinfo'); // disabled operations cannot overwrite preferences
  assert.equal(storage.get('sk.ops'), saved);
  select(a, 'bdmv', true);
  assert.equal(a.ops.bdinfo, false);
  const b = await page(storage); // simulate refresh
  select(b, 'video');
  assert.equal(b.ops.screenshot, false); assert.equal(b.ops.torrent, false);
  select(b, 'bdmv', true); assert.equal(b.ops.bdinfo, false);
  b.ctx.e = { kind: 'video', path: 'media/quick' };
  b.quick('screenshot'); // context-menu quick run is temporary
  assert.equal(storage.get('sk.ops'), saved);
  select(b, 'video'); assert.equal(b.ops.screenshot, false);
  for (const raw of ['{broken', 'null', '[]', '{"screenshot":"false","unknown":true}']) {
    const c = await page(new Map([['sk.ops', raw]]));
    select(c, 'video'); assert.equal(c.ops.screenshot, true);
  }
  console.log('PASS: task preferences survive file changes and refresh; disabled/quick actions and invalid storage are safe');
})().catch(e => { console.error(e); process.exitCode = 1; });
