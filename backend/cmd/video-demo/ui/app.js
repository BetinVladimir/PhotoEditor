'use strict';
// DEMO studio UI: library -> editor -> delivery, plus the client viewer (#/v/<share>.<secret>).
const TPS = 705600000, FPS = 25, FRAME = TPS / FPS;
const $ = (s, r = document) => r.querySelector(s);
const ticks = sec => Math.round(sec * FPS) * FRAME;
const secs = t => t / TPS;
const fmt = t => { const s = secs(t); return Math.floor(s / 60) + ':' + (s % 60).toFixed(2).padStart(5, '0'); };
const store = { get: (k, d) => { try { return JSON.parse(localStorage.getItem(k)) ?? d; } catch { return d; } }, set: (k, v) => { try { localStorage.setItem(k, JSON.stringify(v)); } catch { /* private mode */ } } };

function h(tag, props = {}, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(props)) {
    if (k === 'class') el.className = v; else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (v === true) el.setAttribute(k, ''); else if (v !== false && v != null) el.setAttribute(k, v);
  }
  for (const k of kids.flat()) if (k != null && k !== false) el.append(k.nodeType ? k : document.createTextNode(String(k)));
  return el;
}

let ctx, base, view = 0;
function toast(msg, ms = 4500) { const t = $('#toast'); t.textContent = msg; t.hidden = false; clearTimeout(toast.id); toast.id = setTimeout(() => t.hidden = true, ms); }

async function api(method, path, body, raw) {
  const headers = {};
  if (body !== undefined && !(body instanceof Blob)) headers['Content-Type'] = 'application/json';
  if (method !== 'GET') headers['Idempotency-Key'] = crypto.randomUUID();
  const r = await fetch(path.startsWith('/api') ? path : base + path, { method, headers, body: body instanceof Blob ? body : body === undefined ? undefined : JSON.stringify(body), credentials: 'same-origin' });
  if (raw) return r;
  const text = await r.text();
  let data = {}; try { data = text ? JSON.parse(text) : {}; } catch { /* non-JSON body */ }
  if (!r.ok) throw Object.assign(new Error(data.code || 'HTTP_' + r.status), { status: r.status, data });
  return data;
}
const fail = e => { console.error(e); toast(e.data?.code || e.message); };

// ---------------------------------------------------------------- library
async function sha256(file) {
  const d = await crypto.subtle.digest('SHA-256', await file.arrayBuffer());
  return [...new Uint8Array(d)].map(b => b.toString(16).padStart(2, '0')).join('');
}
function put(url, file, onProgress) {
  return new Promise((res, rej) => {
    const x = new XMLHttpRequest(); x.open('PUT', url);
    x.setRequestHeader('Content-Type', file.type || 'application/octet-stream');
    x.upload.onprogress = e => e.lengthComputable && onProgress(e.loaded / e.total);
    x.onload = () => x.status === 202 ? res(JSON.parse(x.responseText)) : rej(Object.assign(new Error('upload'), { data: JSON.parse(x.responseText || '{}') }));
    x.onerror = () => rej(new Error('network')); x.send(file);
  });
}
const listMedia = async () => (await api('GET', `/projects/${ctx.project_id}/video/media`)).items;

async function libraryView(root, token) {
  const grid = h('div', { class: 'grid' }), prog = h('div', { class: 'bar', hidden: true }, h('i')), note = h('span', { class: 'mute small' });
  const input = h('input', { type: 'file', accept: 'video/*,audio/*', id: 'file' });
  input.addEventListener('change', async () => {
    const f = input.files[0]; if (!f) return;
    try {
      note.textContent = 'Hashing…'; const sha = await sha256(f);
      const kind = f.type.startsWith('audio/') ? 'audio' : 'video';
      const up = await api('POST', `/projects/${ctx.project_id}/video/uploads`, { name: f.name, kind, bytes: f.size, sha256: sha });
      prog.hidden = false; note.textContent = 'Uploading…';
      await put(`${base}/video/uploads/${up.upload_id}/content`, f, p => prog.firstChild.style.width = (p * 100) + '%');
      note.textContent = 'Uploaded — analysing…'; prog.hidden = true; input.value = ''; refresh();
    } catch (e) { prog.hidden = true; note.textContent = ''; fail(e); }
  });
  root.append(h('section', { class: 'card' }, h('h2', {}, 'Upload'), h('div', { class: 'row' }, input, note), prog),
    h('section', { class: 'card' }, h('h2', {}, 'Media library'), grid));
  const player = h('video', { controls: true, hidden: true });
  root.append(h('section', { class: 'card', id: 'pv', hidden: true }, h('h2', {}, 'Preview (proxy)'), player));
  async function refresh() {
    if (token !== view) return;
    let items; try { items = await listMedia(); } catch (e) { return fail(e); }
    grid.replaceChildren(...(items.length ? items.map(card) : [h('p', { class: 'mute' }, 'Nothing here yet — upload a video above.')]));
    if (items.some(m => m.state === 'probing')) setTimeout(refresh, 1500);
  }
  function card(m) {
    const p = m.probe || {};
    return h('div', { class: 'media' },
      m.has_filmstrip ? h('div', { class: 'film', style: `background-image:url(${base}/video/media/${m.id}/filmstrip)` }) : h('div', { class: 'film' }),
      h('div', { class: 'body' }, h('span', { class: 'name' }, m.name), h('div', { class: 'row' }, h('span', { class: 'badge ' + m.state }, m.state + (m.reject_code ? ' · ' + m.reject_code : '')),
        p.duration_ticks ? h('span', { class: 'mute small' }, fmt(p.duration_ticks), p.width ? ` · ${p.width}×${p.height}` : '', p.has_audio ? ' · audio' : '') : null),
        m.has_proxy ? h('button', { onclick: () => { $('#pv').hidden = false; player.hidden = false; player.src = `${base}/video/media/${m.id}/proxy`; player.play().catch(() => 0); } }, 'Preview') : null,
        m.state === 'ready' ? h('button', { onclick: async () => { try { await api('DELETE', `/video/media/${m.id}`); toast('Removed'); refresh(); } catch (e) { fail(e); } } }, 'Remove') : null));
  }
  refresh();
}

// ---------------------------------------------------------------- editor
const E = { seq: null, doc: null, media: {}, sel: null, rev: 0, dirty: false, n: 0 };
const newDoc = id => ({ schema_version: 1, sequence_id: id, base_revision: 0, timebase: { ticks_per_second: TPS },
  video: { fps: { num: FPS, den: 1 }, width: 1280, height: 720, color: { space: 'rec709', range: 'limited' }, background: '#000000' }, audio: { sample_rate: 48000, channels: 2 },
  tracks: [{ id: 'V1', kind: 'video', clips: [] }], markers: [], master: { effects: [], audio: { loudness: { target_lufs: -14, true_peak_db: -1 } } }, rights: { music_declared: false } });
const clips = () => E.doc.tracks[0].clips;
const dur = c => c.src_out - c.src_in;
function relayout() { let at = 0; for (const c of clips()) { c.at = at; at += dur(c); } }
const total = () => clips().reduce((s, c) => s + dur(c), 0);

async function loadSequence() {
  let id = store.get('seq', null), s = null;
  if (id) { try { s = await api('GET', `/video/sequences/${id}`); } catch (e) { if (e.status !== 404) throw e; } }
  if (!s) { id = (await api('POST', `/projects/${ctx.project_id}/video/sequences`, { name: 'Main', fps_num: FPS, fps_den: 1, width: 1280, height: 720 })).sequence_id; store.set('seq', id); s = await api('GET', `/video/sequences/${id}`); }
  E.seq = s; E.rev = s.head_revision; E.doc = s.doc || newDoc(id); E.dirty = false; E.sel = null;
}

async function editorView(root, token) {
  try { await loadSequence(); const items = await listMedia(); E.media = Object.fromEntries(items.map(m => [m.id, m])); } catch (e) { return fail(e); }
  if (token !== view) return;
  const lib = h('div'), tl = h('div', { id: 'tl' }), sel = h('div'), issues = h('div'), info = h('span', { class: 'mute small' });
  const pv = h('video', { controls: true }), renderBox = h('div');
  const encoder = h('select', {}, h('option', { value: 'libx264' }, 'CPU (libx264)'), h('option', { value: 'h264_nvenc' }, 'GPU (NVENC)'));
  encoder.value = ctx.encoder;
  const renderBtn = h('button', { class: 'primary', onclick: render }, 'Render'), saveBtn = h('button', { onclick: () => save() }, 'Save');
  root.append(h('div', { class: 'layout' },
    h('section', { class: 'card' }, h('h2', {}, 'Library'), lib),
    h('section', { class: 'card' }, h('h2', {}, 'Preview'), pv, h('h3', {}, 'Selected clip'), sel)),
    h('section', { class: 'card' }, h('div', { class: 'row' }, h('h2', { class: 'grow' }, 'Timeline'), info, saveBtn, encoder, renderBtn), tl, issues, renderBox));

  function paint() {
    relayout();
    const px = 90, T = total();
    lib.replaceChildren(...Object.values(E.media).filter(m => m.kind === 'video' && m.state === 'ready').map(m =>
      h('div', { class: 'row', style: 'margin-bottom:.4rem' }, h('span', { class: 'grow name' }, m.name), h('span', { class: 'mute small' }, fmt(m.probe.duration_ticks)),
        h('button', { onclick: () => { const c = { id: 'c' + (++E.n) + '_' + Math.random().toString(36).slice(2, 6), source: { kind: 'video', media_id: m.id, sha256: m.source_sha256 }, at: 0, src_in: 0, src_out: Math.floor(m.probe.duration_ticks / FRAME) * FRAME, fit: 'fit', effects: [] }; clips().push(c); E.sel = c.id; E.dirty = true; paint(); } }, 'Add'))));
    if (!lib.children.length) lib.append(h('p', { class: 'mute' }, 'No ready videos — upload one in Library.'));
    tl.replaceChildren(...clips().map(c => h('div', { class: 'clip' + (c.id === E.sel ? ' sel' : ''), style: `left:${secs(c.at) * px + 6}px;width:${Math.max(24, secs(dur(c)) * px - 4)}px`,
      onclick: () => { E.sel = c.id; paint(); const m = E.media[c.source.media_id]; if (m?.has_proxy) { pv.src = `${base}/video/media/${m.id}/proxy#t=${secs(c.src_in)}`; } } }, E.media[c.source.media_id]?.name ?? c.source.media_id, h('br'), fmt(dur(c)))));
    tl.style.minWidth = '100%'; info.textContent = `${clips().length} clips · ${fmt(T)} · ${E.dirty ? 'unsaved' : 'saved r' + E.rev}`;
    renderBtn.disabled = !clips().length;
    const c = clips().find(x => x.id === E.sel), i = clips().indexOf(c);
    sel.replaceChildren(...(c ? [clipForm(c, i)] : [h('p', { class: 'mute' }, 'Select a clip on the timeline.')]));
  }
  function clipForm(c, i) {
    const m = E.media[c.source.media_id], full = m.probe.duration_ticks;
    const num = (label, val, set, min, max) => h('label', { class: 'small' }, label + ' ', h('input', { type: 'number', step: 0.04, min, max, value: val, style: 'width:6rem', onchange: e => { const v = parseFloat(e.target.value); if (!Number.isNaN(v)) { set(v); E.dirty = true; paint(); } } }));
    const mv = d => h('button', { onclick: () => { const j = i + d; if (j < 0 || j >= clips().length) return; [clips()[i], clips()[j]] = [clips()[j], clips()[i]]; E.dirty = true; paint(); } }, d < 0 ? '←' : '→');
    return h('div', { class: 'row' }, num('In (s)', secs(c.src_in).toFixed(2), v => c.src_in = Math.min(ticks(v), c.src_out - FRAME), 0, secs(full)),
      num('Out (s)', secs(c.src_out).toFixed(2), v => c.src_out = Math.min(Math.max(ticks(v), c.src_in + FRAME), Math.floor(full / FRAME) * FRAME), 0, secs(full)),
      h('label', { class: 'small' }, 'Fit ', (() => { const s = h('select', { onchange: e => { c.fit = e.target.value; E.dirty = true; paint(); } }, h('option', { value: 'fit' }, 'fit'), h('option', { value: 'fill' }, 'fill')); s.value = c.fit; return s; })()),
      num('Gain (dB)', c.audio?.gain_db ?? 0, v => { c.audio = { gain_db: v, fade_in: 0, fade_out: 0, mute: false }; }, -60, 24),
      mv(-1), mv(1), h('button', { onclick: () => { clips().splice(i, 1); E.sel = null; E.dirty = true; paint(); } }, 'Delete'));
  }
  async function save() {
    issues.replaceChildren(); relayout();
    try {
      const r = await api('PUT', `/video/sequences/${E.seq.id}/document`, { expected_revision: E.rev, document: E.doc });
      E.rev = r.revision; E.dirty = false; paint(); return true;
    } catch (e) {
      if (e.status === 422 && e.data.issues) issues.replaceChildren(h('ul', { class: 'issues' }, e.data.issues.slice(0, 8).map(i => h('li', {}, `${i.rule} ${i.pointer} ${i.code}`))));
      else if (e.status === 409) toast(`Someone saved revision ${e.data.head_revision}; reload the editor.`);
      else fail(e);
      return false;
    }
  }
  async function render() {
    if (E.dirty && !(await save())) return;
    renderBtn.disabled = true;
    try {
      const t = await api('POST', `/video/sequences/${E.seq.id}/renders`, { revision: E.rev, preset: { name: 'web', encoder: encoder.value } });
      store.set('render', t.render_id); watch(t.render_id);
    } catch (e) { fail(e); renderBtn.disabled = false; }
  }
  async function watch(id) {
    const bar = h('i'), line = h('div', { class: 'row' }); renderBox.replaceChildren(h('h3', {}, 'Render'), line, h('div', { class: 'bar' }, bar));
    for (;;) {
      if (token !== view) return;
      let r; try { r = await api('GET', `/video/renders/${id}`); } catch (e) { return fail(e); }
      bar.style.width = ((r.state === 'ready' ? 1 : r.progress || 0) * 100) + '%';
      line.replaceChildren(h('span', { class: 'badge ' + r.state }, r.state), h('span', { class: 'mute small' }, r.state === 'ready' ? '' : r.stage || '', r.error_code ? ' ' + r.error_code : ''));
      if (r.state === 'ready') {
        renderBtn.disabled = false;
        renderBox.append(h('video', { controls: true, src: `${base}/video/renders/${id}/content` }), h('div', { class: 'row small' }, `${r.width}×${r.height} · ${fmt(r.duration_ticks)} · ${(r.output_bytes / 1048576).toFixed(1)} MiB · ${r.encoder}`,
          h('a', { href: `${base}/video/renders/${id}/content?download=1` }, 'Download'), h('a', { href: '#/delivery' }, 'Deliver →')));
        return;
      }
      if (r.state === 'failed' || r.state === 'cancelled' || r.job_state === 'failed') { renderBtn.disabled = false; return toast('Render failed: ' + (r.error_code || r.state)); }
      await new Promise(res => setTimeout(res, 1000));
    }
  }
  paint();
  const last = store.get('render', null); if (last) { api('GET', `/video/renders/${last}`).then(r => { if (r.sequence_id === E.seq.id && r.state !== 'failed') watch(last); }).catch(() => 0); }
}

// ---------------------------------------------------------------- delivery (owner)
async function deliveryView(root) {
  const rid = store.get('render', null), out = h('div'), shares = store.get('shares', []);
  let render = null; if (rid) { try { render = await api('GET', `/video/renders/${rid}`); } catch { /* none */ } }
  const name = h('input', { type: 'text', value: 'Wedding film', class: 'grow' }), dl = h('input', { type: 'checkbox', id: 'dl' });
  const go = h('button', { class: 'primary', disabled: !(render && render.state === 'ready'), onclick: async () => {
    go.disabled = true;
    try {
      const d = (await api('POST', `/projects/${ctx.project_id}/video/deliveries`, { name: name.value })).delivery_id;
      await api('POST', `/video/deliveries/${d}/renders`, { render_id: rid, title: name.value });
      await api('POST', `/video/deliveries/${d}/publish`, { published: true });
      const s = await api('POST', `/video/deliveries/${d}/shares`, { label: 'client', download: dl.checked, expires_in_hours: 72 });
      shares.unshift({ id: s.share_id, link: `${location.origin}/#/v/${s.share_id}.${s.secret}`, dl: dl.checked, at: new Date().toISOString() }); store.set('shares', shares.slice(0, 10)); showShares();
    } catch (e) { fail(e); } finally { go.disabled = false; }
  } }, 'Publish & create link');
  root.append(h('section', { class: 'card' }, h('h2', {}, 'Deliver the latest render'),
    render ? h('p', { class: 'mute' }, `Render ${render.state} · ${render.width || '?'}×${render.height || '?'} · ${render.duration_ticks ? fmt(render.duration_ticks) : ''}`) : h('p', { class: 'mute' }, 'No render yet — render in the editor first.'),
    h('div', { class: 'row' }, name, h('label', { class: 'small' }, dl, ' allow download'), go)), h('section', { class: 'card' }, h('h2', {}, 'Client links'), out));
  // The server lists deliveries and links (never secrets); a link is clickable only in the browser that created it.
  async function showShares() {
    let items = [];
    try { items = (await api('GET', `/projects/${ctx.project_id}/video/deliveries`)).items; } catch (e) { fail(e); }
    const known = new Map(shares.map(x => [x.id, x]));
    const rows = items.flatMap(d => d.shares.map(x => ({ ...x, delivery: d, local: known.get(x.id) })));
    out.replaceChildren(...(rows.length ? rows.map(x => {
      const gone = x.revoked || (x.expires_at && new Date(x.expires_at) < new Date());
      return h('div', { class: 'link', style: 'margin-bottom:.5rem' },
        x.local ? h('input', { type: 'text', readonly: true, value: x.local.link, onfocus: e => e.target.select() }) : h('span', { class: 'grow mute small' }, `${x.delivery.name} — link secret was shown once, create a new link`),
        h('span', { class: 'small mute' }, `${x.download ? 'download' : 'view only'} · ${x.revoked ? 'revoked' : gone ? 'expired' : x.delivery.state === 'published' ? 'active' : 'delivery unpublished'}`),
        gone ? '' : h('button', { onclick: async () => { try { await api('DELETE', `/video/shares/${x.id}`); toast('Link revoked'); showShares(); } catch (e) { fail(e); } } }, 'Revoke'));
    }) : [h('p', { class: 'mute' }, 'No links yet.')]));
  }
  showShares();
}

// ---------------------------------------------------------------- viewer (client)
async function viewerView(root, hash) {
  $('#top').hidden = true;
  const [share, secret] = hash.split('.');
  const box = h('section', { class: 'card viewer' }); root.append(box);
  try {
    const r = await fetch('/api/v1/portal/sessions', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ share_id: share, secret, locale: 'en' }), credentials: 'same-origin' });
    if (!r.ok) throw new Error('This link is not valid or has expired.');
    const caps = (await r.json()).capabilities || {};
    const d = await (await fetch('/api/v1/portal/video/delivery', { credentials: 'same-origin' })).json();
    if (!d.items?.length) throw new Error('This delivery is not available.');
    box.append(h('h2', {}, d.name));
    for (const it of d.items) box.append(h('h3', {}, it.title), h('video', { controls: true, preload: 'metadata', src: `/api/v1/portal/video/renders/${it.render_id}/content` }),
      h('div', { class: 'row small mute' }, `${it.width}×${it.height} · ${fmt(it.duration_ticks)}`, caps.download ? h('a', { href: `/api/v1/portal/video/renders/${it.render_id}/content?download=1` }, 'Download') : null));
  } catch (e) { box.append(h('p', {}, e.message)); }
}

// ---------------------------------------------------------------- router
async function route() {
  const token = ++view, root = $('#main'); root.replaceChildren(); $('#top').hidden = false;
  const hash = location.hash.replace(/^#\/?/, '');
  if (hash.startsWith('v/')) return viewerView(root, hash.slice(2));
  try { ctx ??= await (await fetch('/api/v1/demo/context')).json(); base = `/api/v1/tenants/${ctx.tenant_id}`; } catch (e) { return fail(e); }
  const name = ['library', 'editor', 'delivery'].includes(hash) ? hash : 'library';
  document.querySelectorAll('#nav a').forEach(a => a.classList.toggle('on', a.dataset.view === name));
  $('#status').textContent = `encoder: ${ctx.encoder}`;
  ({ library: libraryView, editor: editorView, delivery: deliveryView })[name](root, token);
}
addEventListener('hashchange', route); route();
