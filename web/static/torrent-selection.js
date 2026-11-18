/* Exact torrent selection drafts. No disk writes or uploads. */
function torrentTree(files, name) {
  const root = { id: '', label: name, dir: true, depth: 0, children: [], ancestors: [], total: 0, checked: 0, size: 0 };
  const dirs = new Map([['', root]]);
  for (const f of files) {
    const parts = f.path.split('/'); let parent = root;
    for (let i = 0; i < parts.length - 1; i++) {
      const id = parts.slice(0, i + 1).join('/');
      if (!dirs.has(id)) { const d = { id, label: parts[i], dir: true, depth: i + 1, children: [], ancestors: [...parent.ancestors, parent.id], total: 0, checked: 0, size: 0 }; dirs.set(id, d); parent.children.push(d); }
      parent = dirs.get(id);
    }
    parent.children.push({ id: f.path, label: parts.at(-1), dir: false, depth: parts.length, ancestors: [...parent.ancestors, parent.id], total: f.reason ? 0 : 1, checked: 0, size: f.size, reason: f.reason, file: f });
  }
  const nodes = [];
  function walk(d) { nodes.push(d); if (d.children) { d.children.sort((a, b) => Number(b.dir) - Number(a.dir) || a.label.localeCompare(b.label, 'zh', { numeric: true })); for (const c of d.children) walk(c); } }
  walk(root); return nodes;
}
const torrentSelection = {
  ts: null,
  torContent(tor, path) { return tor?.content?.source === path ? tor.content : null; },
  torContentDesc(tor, path) {
    const c = this.torContent(tor, path);
    return c ? `已选 ${c.paths.length} / ${c.available} 个文件 · ${this.fmtSize(c.bytes)}（手动排除 ${c.available - c.paths.length} 个文件）` : '全部文件（应用默认排除规则）';
  },
  torrentPayload(tor, path) {
    const c = this.torContent(tor, path); if (!c) return {};
    const body = { Selection: { snapshot: c.snapshot, paths: [...c.paths] } };
    if (c.mode !== 'follow') body.Exclude = c.mode === 'none' ? [] : [...c.exclude];
    return body;
  },
  resetTorContent(tor) { if (tor) tor.content = null; },
  async openTorContent(entry, tor) {
    if (!entry || !tor) return;
    this.closeTorContent();
    const c = this.torContent(tor, entry.path);
    const s = { source: entry.path, name: entry.name, target: tor, mode: c?.mode || 'follow', rules: (c?.exclude || []).join('\n'), query: '', busy: false, err: '', dirty: false, snapshot: '', files: [], nodes: [], selected: new Set(c?.paths || []), excluded: new Set(c?.excluded || []), expanded: new Set(['']), limit: 500, controller: null, isDir: !!entry.isDir, valid: false };
    this.ts = s;
    await this.scanTorContent(true);
  },
  closeTorContent() { if (this.ts?.controller) this.ts.controller.abort(); this.ts = null; },
  dirtyTorContent() { if (!this.ts) return; this.ts.controller?.abort(); this.ts.dirty = true; this.ts.valid = false; this.ts.busy = false; },
  async scanTorContent(initial = false) {
    const s = this.ts; if (!s) return;
    s.controller?.abort(); const controller = new AbortController(); s.controller = controller;
    s.busy = true; s.err = ''; s.valid = false;
    const body = { Path: s.source };
    const exclude = s.rules.split(/\r?\n/).map(x => x.trim()).filter(Boolean);
    if (s.mode !== 'follow') body.Exclude = s.mode === 'none' ? [] : exclude;
    try {
      const response = await fetch('/api/torrent/preview', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), signal: controller.signal });
      const data = await response.json();
      if (this.ts !== s || s.controller !== controller || controller.signal.aborted) return;
      if (!response.ok) { if (response.status === 401) this.st.authed = false; throw new Error(data.error || '扫描失败'); }
      // Preserve manual exclusions, including exclusions currently hidden by rules.
      if (!initial) for (const f of s.files) if (!f.reason && !s.selected.has(f.path)) s.excluded.add(f.path);
      s.files = data.files || []; s.snapshot = data.snapshot; s.isDir = data.isDir;
      s.selected = new Set(s.files.filter(f => !f.reason && !s.excluded.has(f.path)).map(f => f.path));
      s.nodes = torrentTree(s.files, s.name); s.limit = 500; s.dirty = false; s.valid = true;
      this.refreshTorCounts();
    } catch (e) { if (this.ts === s && s.controller === controller && e.name !== 'AbortError') s.err = e.message; }
    finally { if (this.ts === s && s.controller === controller) s.busy = false; }
  },
  refreshTorCounts() {
    const s = this.ts; if (!s) return;
    const map = new Map(s.nodes.filter(n => n.dir).map(n => { n.total = 0; n.checked = 0; n.size = 0; return [n.id, n]; }));
    for (const n of s.nodes) if (!n.dir) {
      n.checked = !n.reason && s.selected.has(n.id) ? 1 : 0;
      for (const id of n.ancestors) { const d = map.get(id); d.total += n.total; d.checked += n.checked; if (!n.reason) d.size += n.size; }
    }
  },
  toggleTorNode(node, checked) {
    const s = this.ts; if (!s || !s.valid || s.busy || node.reason) return;
    for (const f of s.files) if (!f.reason && (node.dir ? node.id === '' || f.path.startsWith(node.id + '/') : f.path === node.id)) {
      if (checked) { s.selected.add(f.path); s.excluded.delete(f.path); } else { s.selected.delete(f.path); s.excluded.add(f.path); }
    }
    this.refreshTorCounts();
  },
  expandTorNode(n) { if (!this.ts || !n.dir) return; const e = this.ts.expanded; e.has(n.id) ? e.delete(n.id) : e.add(n.id); },
  torRows() {
    const s = this.ts; if (!s) return [];
    const q = s.query.trim().toLocaleLowerCase(); let visible;
    if (q) { const matches = new Set(['']); for (const n of s.nodes) if (n.id.toLocaleLowerCase().includes(q)) { matches.add(n.id); n.ancestors.forEach(a => matches.add(a)); } visible = s.nodes.filter(n => matches.has(n.id)); }
    else visible = s.nodes.filter(n => n.ancestors.every(id => s.expanded.has(id)));
    return visible.slice(0, s.limit);
  },
  torSelectionBytes() { const s = this.ts; return s ? s.files.reduce((n, f) => n + (s.selected.has(f.path) && !f.reason ? f.size : 0), 0) : 0; },
  restoreTorContent() {
    const s = this.ts; if (!s || !s.valid || s.busy) return;
    s.excluded = new Set(); s.selected = new Set(s.files.filter(f => !f.reason).map(f => f.path)); this.refreshTorCounts();
  },
  applyTorContent() {
    const s = this.ts;
    if (!s?.valid || s.dirty || s.busy || !s.selected.size) return false;
    // A main-pane source may have changed while the picker was open.
    const active = s.target === this.tor ? this.cur : this.qk.e;
    if (active?.path !== s.source) { s.err = '制种源已切换，请关闭后重新选择'; return false; }
    const paths = s.files.filter(f => !f.reason && s.selected.has(f.path)).map(f => f.path);
    s.target.content = { source: s.source, snapshot: s.snapshot, paths, mode: s.mode, exclude: s.mode === 'custom' ? s.rules.split(/\r?\n/).map(x => x.trim()).filter(Boolean) : [], excluded: [...s.excluded], available: s.files.filter(f => !f.reason).length, bytes: this.torSelectionBytes() };
    this.closeTorContent(); return true;
  },
};
if (typeof module !== 'undefined') module.exports = { torrentSelection, torrentTree };
