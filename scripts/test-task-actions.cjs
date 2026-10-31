const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, '../web/static/app.js'), 'utf8');
function fixture() {
  const calls = [], sandbox = { localStorage: { getItem: () => null }, TypeError,
    document: { addEventListener() {} },
    fetch: async (url, opts) => {
      calls.push({url, body: JSON.parse(opts.body)});
      if (url === '/api/media/bdinfo') return { ok:false, status:400, text:async ()=>'"unsupported"' };
      return { ok:true, status:202, text:async ()=>JSON.stringify({id:String(calls.length),kind:url.split('/').pop()}) };
    } };
  vm.createContext(sandbox);
  vm.runInContext(source + '\nglobalThis.testApp = app;', sandbox);
  const a = sandbox.testApp;
  a.$refs = {}; a.$nextTick = fn => fn(); a.toast = () => {}; a.petSay = () => {};
  a.st.tools = {ffmpeg:{found:true}};
  a.ctx.e = {kind:'video',path:'media/one.mkv',name:'one.mkv'};
  return {a,calls,sandbox};
}
(async () => {
  const {a,calls,sandbox} = fixture(), oldOps = JSON.stringify(a.ops);
  a.quick('screenshot');
  assert.equal(calls.length,0); assert.equal(a.qk.op,'screenshot');
  a.qk.sh.count = 2; a.qk.sh.upload = 'no'; a.qk.sh.compress = '0';
  assert.equal(a.sh.count,''); // isolated draft; cancel must not change normal parameters
  a.qk.op = ''; assert.equal(calls.length,0);
  a.quick('screenshot'); a.qk.sh.count = 2; a.qk.sh.upload = 'no'; a.qk.sh.compress = '0';
  a.cur = {kind:'video',path:'media/other.mkv'}; // submit the captured target, not a later selection
  await a.confirmQuick();
  assert.equal(calls.length,1); assert.equal(calls[0].url,'/api/screenshot');
  assert.equal(calls[0].body.Path,'media/one.mkv'); assert.equal(calls[0].body.Count,2);
  assert.equal(calls[0].body.Upload,false); assert.equal(calls[0].body.Compress,false);
  assert.equal(a.qk.op,''); assert.equal(JSON.stringify(a.ops),oldOps);
  a.quick('torrent'); a.qk.tor.private = '0'; a.qk.tor.source = 'TEST';
  await a.confirmQuick(); assert.equal(calls.length,2);
  assert.equal(calls[1].body.Private,false); assert.equal(calls[1].body.Source,'TEST');
  a.cur = {kind:'bdmv',path:'media/disc'};
  assert.equal(await a.run(['mediainfo','bdinfo']),false);
  assert.ok(a.tasks.some(t=>t.kind==='info')); // partial success still appears
  let release;
  sandbox.fetch = () => new Promise(resolve => { release = () => resolve({ok:true,status:202,text:async ()=>' {"id":"slow"}'}); });
  a.quick('torrent'); const first = a.confirmQuick();
  assert.equal(a.qk.busy,true); assert.equal(a.submitting,true);
  await a.confirmQuick(); release(); await first; // duplicate confirmation is ignored
  assert.equal(a.qk.op,''); assert.equal(a.submitting,false);
  sandbox.fetch = async () => { throw new TypeError('network down'); };
  a.quick('torrent'); await a.confirmQuick();
  assert.equal(a.qk.op,'torrent'); assert.ok(a.qk.err); assert.equal(a.qk.busy,false);
  console.log('PASS: quick task drafts, cancellation, captured paths, overrides, partial failure, duplicate submits and retry state');
})().catch(e=>{console.error(e); process.exitCode=1;});
