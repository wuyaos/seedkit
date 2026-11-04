/* seedkit frontend — Alpine.js, no build step. */
const api = {
  async req(method, url, body) {
    let r;
    try {
      r = await fetch(url, {
        method, headers: body ? { 'Content-Type': 'application/json' } : {},
        body: body ? JSON.stringify(body) : undefined, credentials: 'same-origin',
      });
    } catch (e) {
      if (e instanceof TypeError) throw new Error('无法连接服务，请确认服务正在运行后重试；若刚提交操作，请先查看任务栏，避免重复提交。');
      throw e;
    }
    if (r.status === 401) { api.onUnauth?.(); throw new Error('未登录'); }
    const txt = await r.text();
    let data = null; try { data = txt ? JSON.parse(txt) : null; } catch { data = txt; }
    if (!r.ok) throw new Error((data && data.error) || r.statusText || `HTTP ${r.status}`);
    return data;
  },
  onUnauth: null,
  get: (u) => api.req('GET', u), post: (u, b) => api.req('POST', u, b || {}),
  put: (u, b) => api.req('PUT', u, b), del: (u) => api.req('DELETE', u),
};

const FILE_COLLATOR = new Intl.Collator('zh', { numeric: true });
const VIDEO = new Set(['video', 'bdmv', 'iso']);
const fmtSize = (n) => {
  if (n == null) return ''; if (n < 1024) return n + ' B';
  const u = ['KB', 'MB', 'GB', 'TB']; let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
  return n.toFixed(n >= 100 ? 0 : n >= 10 ? 1 : 2) + ' ' + u[i];
};
const pad = (n) => String(n).padStart(2, '0');
const fmtTime = (s, full) => {
  if (!s) return ''; const d = new Date(s); if (isNaN(d)) return '';
  const now = new Date();
  if (!full && d.getFullYear() === now.getFullYear()) return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
};
const fmtDur = (s) => { s = Math.round(s || 0); const h = Math.floor(s / 3600), m = Math.floor(s % 3600 / 60), x = s % 60; return (h ? h + ':' : '') + pad(m) + ':' + pad(x); };
const ago = (s) => {
  const d = (Date.now() - new Date(s)) / 1000;
  if (d < 60) return '刚刚'; if (d < 3600) return Math.floor(d / 60) + ' 分钟前';
  if (d < 86400) return Math.floor(d / 3600) + ' 小时前'; if (d < 172800) return '昨天';
  return fmtTime(s);
};
const KIND_ICON = { dir: 'vendor/lucide.svg?v=bf70ed4509c6#folder', bdmv: 'vendor/lucide.svg?v=bf70ed4509c6#disc-3', iso: 'vendor/lucide.svg?v=bf70ed4509c6#disc-3', video: 'vendor/lucide.svg?v=bf70ed4509c6#film', image: 'vendor/lucide.svg?v=bf70ed4509c6#image', text: 'vendor/lucide.svg?v=bf70ed4509c6#file-text', archive: 'vendor/lucide.svg?v=bf70ed4509c6#archive', torrent: 'vendor/lucide.svg?v=bf70ed4509c6#magnet', other: 'vendor/lucide.svg?v=bf70ed4509c6#file' };
const KIND_LABEL = { dir: '目录', bdmv: '蓝光原盘', iso: 'ISO 镜像', video: '视频', image: '图片', text: '文本', archive: '压缩包', torrent: '种子', other: '文件' };
const OP_ICON = { mediainfo: 'vendor/lucide.svg?v=bf70ed4509c6#info', bdinfo: 'vendor/lucide.svg?v=bf70ed4509c6#disc-3', screenshot: 'vendor/lucide.svg?v=bf70ed4509c6#camera', torrent: 'vendor/lucide.svg?v=bf70ed4509c6#magnet' };
const OP_NAME = { mediainfo: 'MediaInfo', bdinfo: 'BDInfo', screenshot: '截图', torrent: '种子' };

const app = {
  // ---- state ----
  ready: false,
  st: { authed: false, needsSetup: false, version: '', tools: null, zscale: false },
  cfg: {},
  lg: { u: '', p: '', err: '', busy: false },
  ui: { theme: localStorage.getItem('sk.theme') || 'system', glass: localStorage.getItem('sk.glass') || 'frosted', pet: localStorage.getItem('sk.pet') !== '0' },
  pop: false,
  roots: [], cwd: '', entries: [], loading: false, filter: '', sort: { k: ['name','size','modTime'].includes(localStorage.getItem('sk.sortKey')) ? localStorage.getItem('sk.sortKey') : 'name', d: localStorage.getItem('sk.sortDir') === '-1' ? -1 : 1 },
  home: localStorage.getItem('sk.home') || '', pathEdit: false, pathDraft: '', _dirSeq: 0,
  sel: new Set(), cur: null, anchor: null,
  ctx: { show: false, x: 0, y: 0, e: null },
  ops: { mediainfo: true, bdinfo: false, screenshot: true, torrent: false }, exp: '',
  opPrefs: {},
  submitting: false, qk: { op: '', e: null, sh: null, tor: null, busy: false, err: '' },
  bd: { list: [], playlist: '', summaryOnly: false, loading: false, err: '' },
  sh: { count: '', lang: '', upload: '', compress: '' },
  tor: { announce: '', source: '', piece: '', private: '', comment: '' },
  res: {}, tab: '', hidx: {}, bdv: 'quick', txt: { body: null, trunc: false },
  tasks: [], trayOpen: true, tlog: '',
  dl: { kind: '', name: '', dest: '', tree: [], err: '', busy: false },
  set: { show: false, c: null, roots: [], err: '', busy: false, nav: 'roots', homeDraft: '', homeBusy: false, homeErr: '', showKey: false },
  pk: { show: false, target: null, tree: [], err: '' },
  recent: { show: false, list: [] },
  toasts: [], lb: '',
  pet: { clip: 'idle', say: '', timer: null, pos: JSON.parse(localStorage.getItem('sk.petPos') || 'null'), drag: null },
  _es: null,

  // ---- lifecycle ----
  async init() {
    this.applyUI();
    try {
      const saved = JSON.parse(localStorage.getItem('sk.ops') || '{}');
      for (const k of Object.keys(OP_NAME)) if (typeof saved?.[k] === 'boolean') this.opPrefs[k] = saved[k];
    } catch {}
    matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => this.applyUI());
    addEventListener('resize', () => this.petClamp());
    api.onUnauth = () => { this.st.authed = false; };
    await this.status(); this.ready = true;
    this.$watch('st.authed', (v) => { if (v) this.boot(); else this.teardown(); });
    if (this.st.authed) this.boot();
    this.$watch('cur', () => this.onCur());
    this.$watch('tab', () => { this.bdv = 'quick'; });
  },
  async status() {
    try { Object.assign(this.st, await api.get('/api/status')); } catch (e) { this.toast(e.message, true); }
  },
  async boot() {
    try {
      const [roots, cfg, tasks] = await Promise.all([api.get('/api/fs/roots'), api.get('/api/settings'), api.get('/api/tasks')]);
      this.roots = roots || []; this.cfg = cfg || {}; this.tasks = tasks || [];
      this.sh.count = ''; if (!await this.go(sessionStorage.getItem('sk.cwd') ?? this.home)) await this.go('');
      this.sse();
      this.petMood();
    } catch (e) { this.toast(e.message, true); }
  },
  teardown() { if (this._es) { this._es.close(); this._es = null; } this.cur = null; this.sel = new Set(); },
  sse() {
    if (this._es) this._es.close();
    const es = new EventSource('/api/tasks/events');
    es.addEventListener('task', (ev) => {
      const t = JSON.parse(ev.data).task; const i = this.tasks.findIndex((x) => x.id === t.id);
      const before = i >= 0 ? this.tasks[i].status : null;
      if (i >= 0) this.tasks.splice(i, 1, t); else this.tasks.unshift(t);
      if (before !== t.status && (t.status === 'done' || t.status === 'failed')) this.onTaskEnd(t);
      this.petMood();
    });
    es.onerror = () => { es.close(); this._es = null; setTimeout(() => { if (this.st.authed) this.sse(); }, 3000); };
    this._es = es;
  },
  onTaskEnd(t) {
    if (t.status === 'failed') { this.toast(`${t.title}：${t.error}`, true); this.petSay('sigh', '失败了…看看日志？'); }
    else {
      if (t.kind === 'fileop') { this.refresh(); this.toast(t.title + ' 完成'); }
      else if (t.kind !== 'upload') this.petSay('cheer', t.title + ' 完成');
      if (this.cur) this.loadRes();
    }
  },

  // ---- auth ----
  async login() {
    this.lg.busy = true; this.lg.err = '';
    try {
      await api.post(this.st.needsSetup ? '/api/auth/setup' : '/api/auth/login', { Username: this.lg.u, Password: this.lg.p });
      this.lg.p = ''; await this.status();
    } catch (e) { this.lg.err = e.message; } finally { this.lg.busy = false; }
  },
  async logout() { try { await api.post('/api/auth/logout'); } catch {} this.st.authed = false; },

  // ---- ui prefs ----
  setUI(k, v) { this.ui[k] = v; localStorage.setItem('sk.' + k, k === 'pet' ? (v ? '1' : '0') : v); this.applyUI(); },
  applyUI() {
    const dark = this.ui.theme === 'dark' || (this.ui.theme === 'system' && matchMedia('(prefers-color-scheme: dark)').matches);
    const h = document.documentElement;
    if (dark) h.dataset.theme = 'dark'; else delete h.dataset.theme;
    if (this.ui.glass !== 'frosted') h.dataset.glass = this.ui.glass; else delete h.dataset.glass;
  },
  closeAll() { this.ctx.show = false; this.pop = false; this.dl.kind = ''; this.lb = ''; this.recent.show = false; this.pk.show = false; },
  toast(msg, err) { const id = Date.now() + Math.random(); this.toasts.push({ id, msg, err }); setTimeout(() => { this.toasts = this.toasts.filter((t) => t.id !== id); }, err ? 6000 : 2500); },
  async copy(text, msg) {
    try { await navigator.clipboard.writeText(text); this.toast(msg || '已复制'); }
    catch { const ta = document.createElement('textarea'); ta.value = text; document.body.appendChild(ta); ta.select(); document.execCommand('copy'); ta.remove(); this.toast(msg || '已复制'); }
  },
  toolPill() {
    const f = this.st.tools?.ffmpeg; if (!f?.found) return '无 ffmpeg';
    return 'ffmpeg ' + (f.version || '') + (this.st.zscale ? ' · zscale' : '');
  },

  // ---- files ----
  crumbs() { if (!this.cwd) return []; const parts = this.cwd.split('/'); return parts.map((n, i) => ({ name: n, path: parts.slice(0, i + 1).join('/') })); },
  normalizePath(p) { return String(p || '').trim().split('\\').join('/').split('/').filter(Boolean).join('/'); },
  editPath() { this.pathEdit = true; this.pathDraft = this.cwd; this.$nextTick(() => { this.$refs.pathInput?.focus(); this.$refs.pathInput?.select(); }); },
  async jumpPath() { if (await this.go(this.pathDraft)) this.pathEdit = false; },
  setHome(p = this.cwd) { this.home = this.normalizePath(p); localStorage.setItem('sk.home', this.home); this.set.homeDraft = this.home; this.set.homeErr = ''; this.toast(this.home ? 'Home 已设为 ' + this.home : 'Home 已恢复为根目录列表'); },
  async applyHome() {
    const s = this.set, p = this.normalizePath(s.homeDraft); s.homeBusy = true; s.homeErr = '';
    try {
      if (p) await api.get('/api/fs/list?path=' + encodeURIComponent(p));
      if (this.set === s) this.setHome(p);
    } catch (e) { s.homeErr = '无法设置 Home：' + e.message; }
    finally { s.homeBusy = false; }
  },
  async go(p, refreshOnly = false) {
    p = this.normalizePath(p); const seq = ++this._dirSeq; this.loading = true;
    try {
      let entries;
      if (!p) {
        const roots = await api.get('/api/fs/roots') || [];
        if (seq === this._dirSeq) this.roots = roots;
        // Only one root: the root list adds no information, enter it directly
        // so the manager opens on its subdirectories.
        if (roots.length === 1) { if (seq !== this._dirSeq) return false; return this.go(roots[0].name); }
        entries = roots.map(r => ({ name:r.name, path:r.name, isDir:true, kind:'dir', size:0, modTime:'' }));
      }
      else entries = await api.get('/api/fs/list?path=' + encodeURIComponent(p)) || [];
      if (seq !== this._dirSeq) return false;
      this.entries = entries; this.cwd = p; this.ctx.show = false;
      if (!refreshOnly) { this.sel = new Set(); this.cur = null; this.anchor = null; this.filter = ''; }
      else { this.sel = new Set([...this.sel].filter(p => entries.some(e => e.path === p))); this.cur = entries.find(e => e.path === this.cur?.path) || null; }
      sessionStorage.setItem('sk.cwd', p); return true;
    } catch (e) { if (seq === this._dirSeq) this.toast('无法打开目录：' + e.message, true); return false; }
    finally { if (seq === this._dirSeq) this.loading = false; }
  },
  refresh() { return this.go(this.cwd, true); },
  up() { const i = this.cwd.lastIndexOf('/'); this.go(i < 0 ? '' : this.cwd.slice(0, i)); },
  shown() {
    const f = this.filter.trim().toLowerCase();
    let l = f ? this.entries.filter((e) => e.name.toLowerCase().includes(f)) : this.entries.slice();
    const { k, d } = this.sort;
    l.sort((a, b) => { if (a.isDir !== b.isDir) return a.isDir ? -1 : 1; let x = a[k], y = b[k]; if (k === 'name') return d * FILE_COLLATOR.compare(x, y); return d * ((x > y) - (x < y)); });
    return l;
  },
  sortBy(k, choose = false) { if (this.sort.k === k && !choose) this.sort.d = -this.sort.d; else this.sort = { k, d: k === 'name' ? 1 : -1 }; localStorage.setItem('sk.sortKey', this.sort.k); localStorage.setItem('sk.sortDir', this.sort.d); this.anchor = null; },
  icon(e) { return KIND_ICON[e.kind] || 'vendor/lucide.svg?v=bf70ed4509c6#file'; },
  kindLabel(e) { return KIND_LABEL[e.kind] || '文件'; },
  pick(e, ev) {
    if (ev.shiftKey && this.anchor) {
      const l = this.shown(); const a = l.findIndex((x) => x.path === this.anchor), b = l.findIndex((x) => x.path === e.path);
      if (a < 0 || b < 0) this.sel = new Set([e.path]);
      else {
        const [s, t] = a < b ? [a, b] : [b, a]; const ns = new Set(ev.ctrlKey || ev.metaKey ? this.sel : []);
        for (let i = s; i <= t; i++) ns.add(l[i].path); this.sel = ns;
      }
    } else if (ev.ctrlKey || ev.metaKey) { this.toggle(e); this.anchor = e.path; }
    else { this.sel = new Set([e.path]); this.anchor = e.path; }
    this.cur = e;
  },
  toggle(e) { const ns = new Set(this.sel); ns.has(e.path) ? ns.delete(e.path) : ns.add(e.path); this.sel = ns; this.anchor = e.path; if (!this.cur || !ns.has(this.cur.path)) this.cur = this.entries.find(x => ns.has(x.path)) || null; },
  toggleAll() { const l = this.shown(); const all = l.length && l.every(e => this.sel.has(e.path)); this.sel = all ? new Set() : new Set(l.map(e => e.path)); this.cur = all ? null : l[0] || null; this.anchor = null; },
  open(e) { if (e.isDir && e.kind !== 'bdmv') this.go(e.path); else this.cur = e; },
  menu(e, ev) { if (!this.sel.has(e.path)) { this.sel = new Set([e.path]); } this.cur = e; this.ctx = { show: true, x: Math.min(ev.clientX, innerWidth - 180), y: Math.min(ev.clientY, innerHeight - 330), e }; },
  download() {
    for (const p of this.sel) { const a = document.createElement('a'); a.href = '/api/fs/download?path=' + encodeURIComponent(p); a.download = ''; a.click(); }
    this.ctx.show = false;
  },
  async dlg(kind) {
    if (kind !== 'mkdir' && !this.sel.size) return;
    this.dl = { kind, name: kind === 'rename' ? this.entries.find((e) => this.sel.has(e.path))?.name || '' : '', dest: '', tree: [], err: '', busy: false };
    if (kind === 'move' || kind === 'copy') this.dl.tree = this.roots.map((r) => ({ name: r.name, path: r.name, depth: 0, open: false, loaded: false }));
    this.$nextTick(() => this.$refs.dlName?.select());
  },
  async treeExpand(n) {
    if (n.loaded) { n.open = !n.open; if (!n.open) this.dl.tree = this.dl.tree.filter((x) => !(x.path.startsWith(n.path + '/'))); else await this.treeLoad(n); return; }
    await this.treeLoad(n);
  },
  async treeLoad(n) {
    try {
      const kids = (await api.get('/api/fs/list?path=' + encodeURIComponent(n.path)) || []).filter((e) => e.isDir && e.kind !== 'bdmv');
      const i = this.dl.tree.indexOf(n); n.open = true; n.loaded = true;
      this.dl.tree.splice(i + 1, 0, ...kids.map((k) => ({ name: k.name, path: k.path, depth: n.depth + 1, open: false, loaded: false })));
    } catch (e) { this.dl.err = e.message; }
  },
  // ---- settings directory picker (absolute server paths) ----
  async pkOpen(target, start) {
    this.pk = { show: true, target, tree: [], err: '' };
    try {
      const d = await api.get('/api/fs/browse?path=' + encodeURIComponent(start || ''));
      this.pk.tree = [{ name: d.path, path: d.path, depth: 0, open: false, loaded: false, root: true }, ...d.dirs.map((x) => ({ name: x.name, path: x.path, depth: 1, open: false, loaded: false, parent: d.path }))];
    } catch (e) { this.pk.err = e.message; }
  },
  async pkExpand(n) {
    if (n.loaded) { n.open = !n.open; if (!n.open) this.pk.tree = this.pk.tree.filter((x) => !x.path.startsWith(n.path + '/')); return; }
    try {
      const d = await api.get('/api/fs/browse?path=' + encodeURIComponent(n.path));
      const i = this.pk.tree.indexOf(n); n.open = true; n.loaded = true;
      this.pk.tree.splice(i + 1, 0, ...d.dirs.map((k) => ({ name: k.name, path: k.path, depth: n.depth + 1, open: false, loaded: false, parent: n.path })));
    } catch (e) { this.pk.err = e.message; }
  },
  // Map an absolute path back to a virtual "<root>/<rel>" path when possible
  // (Home must be virtual); otherwise keep the absolute path as-is.
  pkVirtual(abs) {
    for (const r of this.roots) {
      const rp = (r.path || '').replace(/[\\/]+$/, '');
      if (rp && (abs === rp || abs.startsWith(rp + '/') || abs.startsWith(rp + '\\'))) return r.name + abs.slice(rp.length).replace(/\\/g, '/');
    }
    return abs;
  },
  pkChoose(n) {
    const val = this.pk.target === 'home' ? this.pkVirtual(n.path) : n.path;
    if (this.pk.target === 'home') { this.set.homeDraft = val; }
    else if (this.pk.target === 'output') { this.set.c.paths.output = val; }
    else if (typeof this.pk.target === 'number') { this.set.roots[this.pk.target].path = val; }
    this.pk.show = false;
  },
  async dlOk() {
    const d = this.dl; d.busy = true; d.err = '';
    try {
      const paths = [...this.sel];
      if (d.kind === 'mkdir') await api.post('/api/fs/mkdir', { Path: this.cwd + '/' + d.name });
      else if (d.kind === 'rename') await api.post('/api/fs/rename', { Path: paths[0], Name: d.name });
      else if (d.kind === 'delete') await api.post('/api/fs/delete', { Paths: paths });
      else { await api.post(d.kind === 'move' ? '/api/fs/move' : '/api/fs/copy', { Paths: paths, Dest: d.dest }); this.toast('已加入任务队列'); }
      const kind = d.kind; d.kind = ''; if (kind !== 'move' && kind !== 'copy') { this.sel = new Set(); if (this.cur && paths.includes(this.cur.path)) this.cur = null; }
      await this.refresh();
    } catch (e) { d.err = e.message; } finally { d.busy = false; }
  },

  // ---- inspector ----
  toggleOp(op) {
    if (!this.can(op)) return;
    this.ops[op] = !this.ops[op]; this.opPrefs[op] = this.ops[op];
    localStorage.setItem('sk.ops', JSON.stringify(this.opPrefs));
  },
  can(op) { return this.cur ? this.canFor(this.cur, op) : false; },
  canFor(e, op) {
    if (!e) return false;
    switch (op) {
      case 'mediainfo': return VIDEO.has(e.kind);
      case 'bdinfo': return e.kind === 'bdmv' || e.kind === 'iso';
      case 'screenshot': return VIDEO.has(e.kind) && !!this.st.tools?.ffmpeg?.found;
      case 'torrent': return true;
    }
  },
  runCount() { return ['mediainfo', 'bdinfo', 'screenshot', 'torrent'].filter((k) => this.ops[k] && this.can(k)).length; },
  shotDesc() {
    const c = this.cfg.screenshot || {}; const n = this.sh.count || c.count || 3;
    const sub = this.sh.lang ? (this.sh.lang === 'none' ? '无字幕' : '字幕 ' + this.sh.lang) : (c.subtitles ? '字幕' : '无字幕');
    const up = this.sh.upload ? (this.sh.upload === 'no' ? '不上传' : '上传 ' + this.sh.upload) : ('上传 ' + (this.cfg.upload?.default || 'picgo'));
    return `${n} 张 · ${sub} · ${up}`;
  },
  torDesc() {
    const c = this.cfg.torrent || {}; const priv = this.tor.private ? this.tor.private === '1' : c.private;
    const ps = this.tor.piece || c.piece_size; return (priv ? '私有' : '公开') + ' · ' + (ps ? fmtSize(2 ** ps) + ' 分块' : '自动分块') + (c.announce || this.tor.announce ? '' : ' · 未设 tracker');
  },
  onCur() {
    this.exp = ''; this.bd = { list: [], playlist: '', summaryOnly: false, loading: false, err: '' }; this.txt = { body: null, trunc: false };
    this.res = {}; this.tab = ''; this.hidx = {};
    if (!this.cur) return;
    const k = this.cur.kind;
    this.ops = { mediainfo: VIDEO.has(k) && k !== 'bdmv' && k !== 'iso', bdinfo: k === 'bdmv' || k === 'iso', screenshot: VIDEO.has(k), torrent: this.cur.isDir || k === 'video' || k === 'iso', ...this.opPrefs };
    this.loadRes(); const p = this.cur.path;
    if (k === 'text' && this.cur.size < (2 << 20)) api.get('/api/fs/text?path=' + encodeURIComponent(p)).then(r => { if (this.cur?.path === p) this.txt = { body:r.text, trunc:r.truncated }; }).catch(() => {});
  },
  async loadPlaylists() {
    if (!this.cur || this.bd.list.length || this.bd.loading) return; const state = this.bd, p = this.cur.path; state.loading = true; state.err = '';
    try { const r = await api.get('/api/media/playlists?path=' + encodeURIComponent(p)); if (this.cur?.path === p && this.bd === state) state.list = r.playlists || []; }
    catch (e) { if (this.bd === state) state.err = e.message; } finally { state.loading = false; }
  },
  async run(only = null, entry = this.cur, sh = this.sh, tor = this.tor) {
    if (!entry || this.submitting) return false;
    const p = entry.path, jobs = [], active = only || Object.keys(this.ops).filter(k => this.ops[k]);
    this.submitting = true;
    try {
      if (active.includes('mediainfo') && this.canFor(entry, 'mediainfo')) jobs.push(api.post('/api/media/info', { Path: p }));
      if (active.includes('bdinfo') && this.canFor(entry, 'bdinfo')) jobs.push(api.post('/api/media/bdinfo', { Path: p, Playlist: this.bd.playlist, SummaryOnly: this.bd.summaryOnly }));
      if (active.includes('screenshot') && this.canFor(entry, 'screenshot')) {
        const b = { Path: p, Count: sh.count || 0, Language: sh.lang, Upload: sh.upload !== 'no', Host: sh.upload && sh.upload !== 'no' ? sh.upload : '' };
        if (sh.compress) b.Compress = sh.compress === '1';
        jobs.push(api.post('/api/screenshot', b));
      }
      if (active.includes('torrent') && this.canFor(entry, 'torrent')) {
        const b = { Path: p, Announce: tor.announce, Source: tor.source, Comment: tor.comment, PieceSize: tor.piece ? +tor.piece : 0 };
        if (tor.private) b.Private = tor.private === '1';
        jobs.push(api.post('/api/torrent', b));
      }
      if (!jobs.length) return false;
      const settled = await Promise.allSettled(jobs), ts = settled.filter(r => r.status === 'fulfilled').map(r => r.value);
      for (const t of ts) if (!this.tasks.find(x => x.id === t.id)) this.tasks.unshift(t);
      if (ts.length) { this.trayOpen = true; this.toast('已提交 ' + ts.length + ' 个任务'); this.petSay('busy', '开工！'); }
      const failed = settled.filter(r => r.status === 'rejected');
      if (failed.length) this.toast(failed.map(r => r.reason.message).join('；'), true);
      return !failed.length;
    } catch (e) { this.toast(e.message, true); return false; }
    finally { this.submitting = false; }
  },
  quick(op) {
    const e = this.ctx.e; this.ctx.show = false;
    if (!e || this.submitting || !this.canFor(e, op)) return;
    this.cur = e;
    if (op === 'screenshot' || op === 'torrent') {
      this.qk = { op, e: { ...e }, sh: { ...this.sh }, tor: { ...this.tor }, busy: false, err: '' };
      this.$nextTick(() => this.$refs.quickFirst?.focus());
    } else this.$nextTick(() => this.run([op], e));
  },
  async confirmQuick() {
    const q = this.qk; if (!q.op || q.busy || this.submitting) return;
    q.busy = true; q.err = '';
    const ok = await this.run([q.op], q.e, q.sh, q.tor);
    q.busy = false;
    if (ok) q.op = ''; else q.err = '提交失败，请检查提示与工具状态后重试。';
  },

  // ---- results ----
  async loadRes() {
    if (!this.cur) return; const p = this.cur.path;
    try {
      const r = await api.get('/api/results?path=' + encodeURIComponent(p)); if (!this.cur || this.cur.path !== p) return;
      this.res = r || {};
      for (const k of Object.keys(this.res)) if (this.hidx[k] == null) this.hidx[k] = 0;
      if (!this.tab || !this.res[this.tab]?.length) this.tab = ['screenshot', 'torrent', 'bdinfo', 'mediainfo'].find((k) => this.res[k]?.length) || '';
    } catch (e) { this.toast(e.message, true); }
  },
  hasRes() { return Object.values(this.res).some((l) => l.length); },
  rec() { const l = this.res[this.tab]; if (!l?.length) return null; return l[Math.min(+this.hidx[this.tab] || 0, l.length - 1)]; },
  kindIcon(k) { return OP_ICON[k] || 'vendor/lucide.svg?v=bf70ed4509c6#file'; }, kindName(k) { return OP_NAME[k] || k; },
  uploaded(r) { return (r.data.shots || []).filter((s) => s.url); },
  pending(r) { return (r.data.shots || []).filter((s) => !s.url); },
  async reupload(r) { try { const t = await api.post('/api/screenshot/reupload', { ID: r.id, Host: this.sh.upload && this.sh.upload !== 'no' ? this.sh.upload : '' }); this.tasks.unshift(t); this.trayOpen = true; } catch (e) { this.toast(e.message, true); } },
  async delRes(r, files) {
    if (files && !confirm('同时删除产物文件（截图 / 种子）？\n确定 = 一并删除，取消 = 仅删记录')) files = false;
    try { await api.del(`/api/results/${r.id}${files ? '?files=1' : ''}`); await this.loadRes(); } catch (e) { this.toast(e.message, true); }
  },
  async openRecent() { this.recent.show = true; try { this.recent.list = await api.get('/api/results/recent?n=60') || []; } catch (e) { this.toast(e.message, true); } },
  async jumpTo(r) {
    this.recent.show = false; const dir = r.path.includes('/') ? r.path.slice(0, r.path.lastIndexOf('/')) : '';
    if (dir !== this.cwd) await this.go(dir);
    const e = this.entries.find((x) => x.path === r.path);
    if (e) { this.sel = new Set([e.path]); this.cur = e; this.$nextTick(() => { this.tab = r.kind; }); } else this.toast('文件已不在原位置', true);
  },
  locate(t) {
    const m = /· (.+)$/.exec(t.title); const name = m ? m[1] : '';
    const r = Object.values(this.res).flat().find((x) => x.task === t.id);
    if (r) { this.tab = r.kind; return; }
    api.get('/api/results/recent?n=200').then((l) => { const x = (l || []).find((y) => y.task === t.id); if (x) this.jumpTo(x); else this.toast('找不到对应产物' + (name ? '：' + name : ''), true); });
  },

  // ---- tasks ----
  isEnd(t) { return t.status === 'done' || t.status === 'failed' || t.status === 'cancelled'; },
  trayCount() { const run = this.tasks.filter((t) => t.status === 'running' || t.status === 'queued').length, done = this.tasks.filter((t) => this.isEnd(t)).length; return (run ? `${run} 进行中 · ` : '') + `${done} 已结束`; },
  statusLabel(t) {
    if (t.status === 'queued') return '排队中';
    const dur = t.started && t.finished ? Math.round((new Date(t.finished) - new Date(t.started)) / 1000) : null;
    return (t.status === 'cancelled' ? '已取消' : t.status === 'failed' ? '失败' : ago(t.finished || t.created)) + (dur != null && t.status === 'done' ? ` · ${dur} 秒` : '');
  },
  async cancel(t) { try { await api.post(`/api/tasks/${t.id}/cancel`); } catch (e) { this.toast(e.message, true); } },
  async delTask(t) { try { await api.del(`/api/tasks/${t.id}`); this.tasks = this.tasks.filter((x) => x.id !== t.id); } catch (e) { this.toast(e.message, true); } },
  async clearDone() { const ends = this.tasks.filter((t) => this.isEnd(t)); await Promise.all(ends.map((t) => api.del(`/api/tasks/${t.id}`).catch(() => {}))); this.tasks = this.tasks.filter((t) => !this.isEnd(t)); },

  // ---- settings ----
  async openSettings() {
    try {
      const c = await api.get('/api/settings'); this.cfg = c;
      this.set = { show: true, c: JSON.parse(JSON.stringify(c)), roots: Object.entries(c.paths.roots || {}).map(([name, path]) => ({ name, path })), err: '', busy: false, nav: 'roots', homeDraft: this.home, homeBusy: false, homeErr: '' };
    } catch (e) { this.toast(e.message, true); }
  },
  navTo(id) { this.set.nav = id; this._navLock = Date.now() + 600; this.$refs.db?.querySelector(`[data-sec="${id}"]`)?.scrollIntoView({ block: 'start', behavior: 'smooth' }); },
  secScroll() {
    if ((this._navLock || 0) > Date.now()) return; const db = this.$refs.db; if (!db) return;
    const top = db.getBoundingClientRect().top + 40; let cur = null;
    for (const s of db.querySelectorAll('.sec')) if (s.getBoundingClientRect().top <= top) cur = s.dataset.sec;
    if (db.scrollTop + db.clientHeight >= db.scrollHeight - 2) cur = 'srv';
    if (cur) this.set.nav = cur;
  },
  async saveSettings() {
    const s = this.set; s.busy = true; s.err = '';
    try {
      const roots = {}; for (const r of s.roots) if (r.name.trim() && r.path.trim()) roots[r.name.trim()] = r.path.trim();
      s.c.paths.roots = roots;
      for (const k of ['subtitles', 'compress']) s.c.screenshot[k] = s.c.screenshot[k] === true || s.c.screenshot[k] === 'true';
      s.c.torrent.private = s.c.torrent.private === true || s.c.torrent.private === 'true';
      const r = await api.put('/api/settings', s.c);
      this.toast(r.restartRequired ? '已保存，监听地址变更需重启' : '已保存');
      s.show = false; await this.status(); this.cfg = await api.get('/api/settings'); this.roots = await api.get('/api/fs/roots'); await this.refresh();
    } catch (e) { s.err = e.message; } finally { s.busy = false; }
  },

  // ---- mascot ----
  petClip(c) { if (this.pet.clip === c) return; this.pet.clip = c; this.$nextTick(() => { const v = this.$refs.pet; if (v) { v.load(); v.play().catch(() => {}); } }); },
  petMood() { if (this.pet.timer) return; const busy = this.tasks.some((t) => t.status === 'running'); this.petClip(busy ? (this.tasks.some((t) => t.status === 'running' && t.kind === 'bdinfo') ? 'think' : 'busy') : 'idle'); },
  // draggable mascot: position persisted in localStorage; click without movement = greet; dblclick = reset
  petStyle() { const p = this.pet.pos; return p ? `left:${p.x}px;top:${p.y}px;right:auto;bottom:auto` : ''; },
  petDown(ev) {
    if (ev.button !== 0) return; const r = ev.currentTarget.getBoundingClientRect();
    this.pet.drag = { sx: ev.clientX, sy: ev.clientY, ox: r.left, oy: r.top, w: r.width, h: r.height, moved: false };
    ev.currentTarget.setPointerCapture(ev.pointerId);
  },
  petMove(ev) {
    const d = this.pet.drag; if (!d) return; const dx = ev.clientX - d.sx, dy = ev.clientY - d.sy;
    if (!d.moved && Math.hypot(dx, dy) < 4) return; d.moved = true;
    this.pet.pos = this.petClampPos(d.ox + dx, d.oy + dy, d.w, d.h);
  },
  petUp() {
    const d = this.pet.drag; if (!d) return; this.pet.drag = null;
    if (d.moved) localStorage.setItem('sk.petPos', JSON.stringify(this.pet.pos)); else this.petSay('hi', '你好呀！');
  },
  petReset() { this.pet.pos = null; localStorage.removeItem('sk.petPos'); },
  petClampPos(x, y, w, h) { return { x: Math.round(Math.max(-w * 0.4, Math.min(innerWidth - w * 0.6, x))), y: Math.round(Math.max(-h * 0.3, Math.min(innerHeight - h * 0.6, y))) }; },
  petClamp() { const p = this.pet.pos, el = this.$refs.pet?.parentElement; if (!p || !el) return; this.pet.pos = this.petClampPos(p.x, p.y, el.offsetWidth, el.offsetHeight); },
  petSay(clip, text) {
    if (!this.ui.pet) return; clearTimeout(this.pet.timer); this.pet.timer = null;
    this.petClip(clip); this.pet.say = text;
    this.pet.timer = setTimeout(() => { this.pet.say = ''; this.pet.timer = null; this.petMood(); }, 4500);
  },

  fmtSize, fmtTime, fmtDur,
};

document.addEventListener('alpine:init', () => { Alpine.data('app', () => app); });