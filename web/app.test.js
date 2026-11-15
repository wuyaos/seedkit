// Node-based tests for the upload-host upgrade in web/static/app.js.
// Run with: node --test web/app.test.js
//
// Scope: frontend only (per task boundary, no Go files touched, no real network
// calls — api.post/api.get are monkey-patched per test). Exercises the Alpine
// `app` data object exported additively via `module.exports` at the bottom of
// app.js (browser-safe: browsers never define `module`, so the served app is
// unaffected).
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');

// app.js's top-level `const app = {...}` object literal reads localStorage directly
// in several state initializers (ui.theme, sort, home, pet position) — it's a
// browser-only global that doesn't exist under plain Node. Stub the minimal
// Storage interface so `require()`-ing the file doesn't throw; tests never rely
// on persisted values, so an always-empty store is sufficient and keeps this
// additive/browser-safe (real browsers ignore this entirely).
if (typeof global.localStorage === 'undefined') {
  global.localStorage = { getItem() { return null; }, setItem() {}, removeItem() {} };
}
if (typeof global.sessionStorage === 'undefined') {
  global.sessionStorage = { getItem() { return null; }, setItem() {}, removeItem() {} };
}
// Same reasoning for `document`/`Alpine`/`matchMedia`: app.js's bottom registration
// line (`document.addEventListener('alpine:init', ...)`) and a `matchMedia(...)
// .addEventListener(...)` call inside init() assume a browser. Stub just enough
// for module-load + the methods under test to run; nothing here changes browser
// behavior (browsers already have real implementations).
if (typeof global.document === 'undefined') {
  global.document = { addEventListener() {} };
}
if (typeof global.Alpine === 'undefined') {
  global.Alpine = { data() {} };
}
if (typeof global.matchMedia === 'undefined') {
  global.matchMedia = () => ({ matches: false, addEventListener() {} });
}

const mod = require(path.join(__dirname, 'static', 'app.js'));
const { app, api, UPLOAD_HOSTS, UPLOAD_DEFAULTS, UPLOAD_SECRET_FIELD, SHOT_STATUS_LABEL, fmtSize, fmtDur } = mod;

// Build a fresh call-context per test: prototype-chain to `app` for methods,
// but give it its own plain-object state so mutations (this.cfg = ..., this.set.err = ...)
// never leak into `app` itself or across tests.
function ctx(state = {}) {
  const c = Object.create(app);
  Object.assign(c, {
    cfg: {}, sh: { count: '', lang: '', upload: '', compress: '' }, tasks: [], trayOpen: false,
    toasts: [], _toastLog: [],
  }, state);
  // toast()/copy() touch the DOM/clipboard in the browser; stub them so tests
  // stay local-mock-only (no real upload, no real clipboard) and so we can
  // assert on what the UI *would* have shown the user.
  c.toast = function (msg, err) { this._toastLog.push({ msg, err: !!err }); };
  c.copy = function (text, msg) { this._copyLog = this._copyLog || []; this._copyLog.push({ text, msg }); this.toast(msg); };
  return c;
}

test('UPLOAD_HOSTS covers the full shared-contract host list, in order', () => {
  assert.deepEqual(UPLOAD_HOSTS.map((h) => h.v), ['picgo', 'pixhost', 'imgbb', 'freeimage', 'agsv', 'pterclub']);
});

test('fmtSize: representative byte/KB/MB/GB formatting', () => {
  assert.equal(fmtSize(0), '0 B');
  assert.equal(fmtSize(512), '512 B');
  assert.equal(fmtSize(1536), '1.50 KB');
  assert.equal(fmtSize(5 * 1024 * 1024), '5.00 MB');
  assert.equal(fmtSize(null), '');
});

test('fmtDur: seconds to h:mm:ss / mm:ss', () => {
  assert.equal(fmtDur(65), '01:05');
  assert.equal(fmtDur(3661), '1:01:01');
  assert.equal(fmtDur(0), '00:00');
});

test('shotStatus: legacy bare-url shots are treated as success without upload_status', () => {
  const c = ctx();
  assert.equal(c.shotStatus({ url: 'https://x/1.png' }), 'success');
  assert.equal(c.shotStatus({}), 'pending');
  assert.equal(c.shotStatus({ upload_status: 'failed', url: '' }), 'failed');
  assert.equal(c.shotStatus({ upload_status: 'cached', url: 'https://x/2.png' }), 'cached');
});

test('shotStatusLabel: maps through SHOT_STATUS_LABEL with a safe fallback', () => {
  const c = ctx();
  assert.equal(c.shotStatusLabel({ upload_status: 'failed' }), SHOT_STATUS_LABEL.failed);
  assert.equal(c.shotStatusLabel({ url: 'https://x/1.png' }), SHOT_STATUS_LABEL.success);
  assert.equal(c.shotStatusLabel({ upload_status: 'weird-unknown' }), 'weird-unknown');
});

test('uploaded()/pending(): status-aware, back-compat with legacy url-only shots', () => {
  const c = ctx();
  const r = { data: { shots: [
    { path: 'a.png', url: 'https://x/a.png' },                                   // legacy: success
    { path: 'b.png', upload_status: 'failed', upload_error: 'timeout' },         // new: failed
    { path: 'c.png', upload_status: 'cached', url: 'https://x/c.png' },          // new: cached
    { path: 'd.png' },                                                          // new: pending
  ] } };
  assert.deepEqual(c.uploaded(r).map((s) => s.path), ['a.png', 'c.png']);
  assert.deepEqual(c.pending(r).map((s) => s.path), ['b.png', 'd.png']);
});

test('copyLinks/copyImg: fixed BBCode formats, in the uploaded-only set, with a partial-success suffix', () => {
  const c = ctx();
  const r = { data: { shots: [
    { path: 'a.png', url: 'https://x/a.png' },
    { path: 'b.png', upload_status: 'failed' },
  ] } };
  c.copyLinks(r);
  assert.equal(c._copyLog[0].text, 'https://x/a.png');
  assert.match(c._copyLog[0].msg, /仅成功项 1\/2/);
  c.copyImg(r);
  assert.equal(c._copyLog[1].text, '[img]https://x/a.png[/img]');
  assert.match(c._copyLog[1].msg, /仅成功项 1\/2/);
});

test('copyLinks/copyImg: no partial-success suffix when every shot succeeded', () => {
  const c = ctx();
  const r = { data: { shots: [{ path: 'a.png', url: 'https://x/a.png' }] } };
  c.copyLinks(r);
  assert.doesNotMatch(c._copyLog[0].msg, /仅成功项/);
});

test('copyThumb: links to the original image (s.url), not a separate page_url, and requires EVERY successful item to have a real thumb_url', () => {
  const c = ctx();
  const ok = { data: { shots: [
    { path: 'a.png', url: 'https://x/a.png', thumb_url: 'https://x/a_th.png', page_url: 'https://x/p/a' },
  ] } };
  assert.equal(c.allHaveThumb(ok), true);
  c.copyThumb(ok);
  // [url=] must wrap the original image URL, never page_url (page_url is a separate,
  // optional field and must not silently replace the image link).
  assert.equal(c._copyLog[0].text, '[url=https://x/a.png][img]https://x/a_th.png[/img][/url]');

  const mixed = { data: { shots: [
    { path: 'a.png', url: 'https://x/a.png', thumb_url: 'https://x/a_th.png' },
    { path: 'b.png', url: 'https://x/b.png' }, // success but no real thumb
  ] } };
  // Thumbnail mode is disabled entirely (not "copy only the ones that have a thumb")
  // whenever any successful item lacks a real thumb_url.
  assert.equal(c.allHaveThumb(mixed), false);
  c.copyThumb(mixed);
  assert.equal(c._copyLog.length, 1); // no second entry was appended
  assert.equal(c._toastLog.at(-1).err, true);
  assert.match(c._toastLog.at(-1).msg, /并非所有成功项都有缩略图/);
});

test('copyThumb: no items uploaded at all -> disabled, warns, copies nothing', () => {
  const c = ctx();
  const r = { data: { shots: [{ path: 'a.png' }] } };
  assert.equal(c.allHaveThumb(r), false);
  c.copyThumb(r);
  assert.equal(c._copyLog, undefined);
  assert.equal(c._toastLog.at(-1).err, true);
});

test('copyLinks/copyImg: no-op (no toast) when nothing uploaded yet', () => {
  const c = ctx();
  const r = { data: { shots: [{ path: 'a.png' }] } };
  c.copyLinks(r); c.copyImg(r);
  assert.equal(c._toastLog.length, 0);
});

test('backfillUpload: fills missing host objects + concurrency on an old config, keeps existing values', () => {
  const c = ctx();
  const draft = { upload: { default: 'picgo', picgo: { endpoint: 'https://e', api_key: '••••1234', album: 'x' }, pixhost: { content_type: 1, max_th: 350 } } };
  c.backfillUpload(draft);
  assert.equal(draft.upload.picgo.api_key, '••••1234'); // untouched
  assert.equal(draft.upload.pixhost.domain, 'to');       // backfilled default
  assert.equal(draft.upload.pixhost.content_type, 1);    // untouched
  assert.equal(draft.upload.concurrency, 3);
  for (const host of Object.keys(UPLOAD_DEFAULTS)) {
    assert.ok(draft.upload[host], `expected upload.${host} to be backfilled`);
  }
  assert.equal(draft.upload.imgbb.api_key, '');
});

test('backfillUpload: never clobbers a real concurrency value already set', () => {
  const c = ctx();
  const draft = { upload: { concurrency: 7 } };
  c.backfillUpload(draft);
  assert.equal(draft.upload.concurrency, 7);
});

test('toggleSecret: reveals via the generalized /api/settings/upload-secret endpoint for every contract host', async () => {
  for (const host of Object.keys(UPLOAD_SECRET_FIELD)) {
    const field = UPLOAD_SECRET_FIELD[host];
    const calls = [];
    const realPost = api.post;
    api.post = async (url, body) => { calls.push([url, body]); return { key: 'REAL-SECRET' }; };
    try {
      const c = ctx({ set: { c: { upload: { [host]: { [field]: '••••abcd' } } }, showSecret: {}, secretBusy: {}, show: true, err: '' } });
      await c.toggleSecret(host);
      assert.deepEqual(calls, [['/api/settings/upload-secret', { Host: host }]]);
      assert.equal(c.set.c.upload[host][field], 'REAL-SECRET');
      assert.equal(c.set.showSecret[host], true);
      // toggling again just hides it, no second network call
      c.toggleSecret(host);
      assert.equal(c.set.showSecret[host], false);
      assert.equal(calls.length, 1);
    } finally { api.post = realPost; }
  }
});

test('toggleSecret: a non-masked (already-plain) value reveals without calling the API', async () => {
  const calls = [];
  const realPost = api.post;
  api.post = async (...a) => { calls.push(a); return { key: 'x' }; };
  try {
    const c = ctx({ set: { c: { upload: { picgo: { api_key: 'already-plain' } } }, showSecret: {}, secretBusy: {}, show: true, err: '' } });
    await c.toggleSecret('picgo');
    assert.equal(calls.length, 0);
    assert.equal(c.set.showSecret.picgo, true);
  } finally { api.post = realPost; }
});

test('reupload: retry-failed-only vs force-full-reupload send distinct {All,Force} payloads', async () => {
  const calls = [];
  const realPost = api.post;
  api.post = async (url, body) => { calls.push([url, body]); return { id: 't1' }; };
  try {
    const c = ctx({ sh: { upload: 'imgbb' } });
    await c.reupload({ id: 'r1' }, { all: false, force: false });
    await c.reupload({ id: 'r1' }, { all: true, force: true });
    assert.deepEqual(calls[0], ['/api/screenshot/reupload', { ID: 'r1', Host: 'imgbb', All: false, Force: false }]);
    assert.deepEqual(calls[1], ['/api/screenshot/reupload', { ID: 'r1', Host: 'imgbb', All: true, Force: true }]);
    assert.equal(c.tasks.length, 2);
  } finally { api.post = realPost; }
});

test('isEnd: terminal states include done/failed/cancelled', () => {
  const c = ctx();
  assert.equal(c.isEnd({ status: 'done' }), true);
  assert.equal(c.isEnd({ status: 'failed' }), true);
  assert.equal(c.isEnd({ status: 'cancelled' }), true);
  assert.equal(c.isEnd({ status: 'running' }), false);
  assert.equal(c.isEnd({ status: 'queued' }), false);
});

test('onTaskEnd: refreshes results (loadRes) for every terminal status, not just done — backend persists partial results on failure/cancellation', () => {
  for (const status of ['done', 'failed', 'cancelled']) {
    const c = ctx({ cur: { path: 'x.mkv' } });
    let loadResCalls = 0;
    c.loadRes = async function () { loadResCalls++; };
    c.petSay = function () {}; // avoid touching ui.pet/timers
    c.refresh = function () {};
    c.onTaskEnd({ id: 't1', title: '截图 · x.mkv', kind: 'screenshot', status, error: 'boom' });
    assert.equal(loadResCalls, 1, `expected loadRes() to run for status=${status}`);
  }
});

test('onTaskEnd: does not call loadRes when no file is currently selected', () => {
  const c = ctx({ cur: null });
  let loadResCalls = 0;
  c.loadRes = async function () { loadResCalls++; };
  c.onTaskEnd({ id: 't1', title: 't', kind: 'screenshot', status: 'cancelled' });
  assert.equal(loadResCalls, 0);
});
