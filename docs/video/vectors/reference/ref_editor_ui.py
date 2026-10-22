"""Эталон (одноразовый): геометрия таймлайна, хит-тест, масштаб, привязка, автомат жестов, горячие клавиши,
цели команд, автомат воспроизведения, колёса/кривые/осциллограммы. Документ 23. Только стандартная библиотека."""
import copy
import math
from ref_editor_core import (TPS, ft, dur, end_of, doc_end, find_clip, find_track, clip_kind, src_ticks_at,
                             round_frame, compose_at, voice_span, video_layers_track)

RULER_H = 28
VIDEO_TRACK_H = 64
AUDIO_TRACK_H = 48
TRACK_GAP = 2
HEADER_W = 160
EDGE_PX = 6
TRANSITION_HANDLE_PX = 10
DRAG_THRESHOLD_PX = 4
SNAP_PX = 8
ZOOM_LADDER = [705600, 1411200, 3528000, 7056000, 14112000, 35280000, 70560000, 141120000, 352800000,
               705600000, 1411200000, 3528000000, 7056000000]
PHOTO_DEFAULT_TICKS = 5 * TPS
PRELOAD_LEAD_TICKS = 2 * TPS
LINK_REFRESH_AGE_MS = 45000
DECK_MAX_FAILURES = 3


# ---------------- FN-E-07 геометрия ----------------
def tick_to_x(t, view):
    return HEADER_W + (t - view['scroll']) // view['tpp']


def x_to_tick(x, view):
    return view['scroll'] + (x - HEADER_W) * view['tpp']


def layout_rows(doc):
    vids = [t for t in doc['tracks'] if t['kind'] == 'video'][::-1]
    auds = [t for t in doc['tracks'] if t['kind'] == 'audio']
    rows = []
    y = RULER_H
    for t in vids + auds:
        h = VIDEO_TRACK_H if t['kind'] == 'video' else AUDIO_TRACK_H
        rows.append({'track': t['id'], 'kind': t['kind'], 'y': y, 'h': h, 'locked': t.get('locked') is True})
        y += h + TRACK_GAP
    return rows


def row_at(doc, y):
    for r in layout_rows(doc):
        if r['y'] <= y < r['y'] + r['h']:
            return r
    return None


# ---------------- FN-E-08 хит-тест ----------------
def hit_test(doc, view, x, y):
    if x < 0 or y < 0:
        return {'kind': 'none'}
    if y < RULER_H:
        return {'kind': 'ruler', 'tick': x_to_tick(x, view)} if x >= HEADER_W else {'kind': 'none'}
    r = row_at(doc, y)
    if r is None:
        return {'kind': 'none'}
    if x < HEADER_W:
        return {'kind': 'track_header', 'track': r['track']}
    tr = doc['tracks'][find_track(doc, r['track'])]
    base = {'track': r['track'], 'locked': r['locked']}
    if y < r['y'] + r['h'] // 3:
        for c in tr['clips']:
            t_in = c.get('transition_in')
            if t_in and t_in['kind'] != 'cut' and abs(x - tick_to_x(c['at'], view)) <= TRANSITION_HANDLE_PX // 2:
                return dict(base, kind='transition', clip=c['id'])
    for c in tr['clips']:
        x0, x1 = tick_to_x(c['at'], view), tick_to_x(end_of(c), view)
        if x0 <= x < x1:
            e = min(EDGE_PX, (x1 - x0) // 3)
            if x < x0 + e and c['source']['kind'] != 'photo':
                return dict(base, kind='edge_in', clip=c['id'])
            if x >= x1 - e:
                return dict(base, kind='edge_out', clip=c['id'])
            return dict(base, kind='clip_body', clip=c['id'])
    return dict(base, kind='empty', tick=x_to_tick(x, view))


# ---------------- FN-E-09 масштаб и видимая область ----------------
def ladder_index(tpp):
    for i, v in enumerate(ZOOM_LADDER):
        if v >= tpp:
            return i
    return len(ZOOM_LADDER) - 1


def zoom_at(view, anchor_x, direction):
    i = ladder_index(view['tpp'])
    j = max(0, min(len(ZOOM_LADDER) - 1, i - direction))
    new_tpp = ZOOM_LADDER[j]
    anchor = x_to_tick(anchor_x, view)
    scroll = max(0, anchor - (anchor_x - HEADER_W) * new_tpp)
    return {'scroll': scroll, 'tpp': new_tpp, 'width': view['width']}


def zoom_to_fit(doc, width):
    e = doc_end(doc)
    for v in ZOOM_LADDER:
        if e <= v * width:
            return {'scroll': 0, 'tpp': v, 'width': width}
    return {'scroll': 0, 'tpp': ZOOM_LADDER[-1], 'width': width}


def visible_clips(doc, view):
    span = view['width'] * view['tpp']
    lo, hi = view['scroll'] - span, view['scroll'] + 2 * span
    out = []
    for t in doc['tracks']:
        for c in t['clips']:
            if c['at'] < hi and end_of(c) > lo:
                out.append(c['id'])
    return out


# ---------------- FN-E-10 привязка ----------------
def snap_time(c, anchors, thr, F):
    best = None
    for a in anchors:
        dd = abs(a - c)
        if dd <= thr and (best is None or dd < best[0] or (dd == best[0] and a < best[1])):
            best = (dd, a)
    return best[1] if best else round_frame(c, F)


def snap_anchors(doc, playhead, exclude=None):
    s = {0, playhead}
    for t in doc['tracks']:
        for c in t['clips']:
            if c['id'] != exclude:
                s.add(c['at'])
                s.add(end_of(c))
    for m in doc.get('markers', []):
        s.add(m['at'])
    return sorted(s)


def nearest(v, anchors, thr):
    best = None
    for a in anchors:
        dd = abs(a - v)
        if dd <= thr and (best is None or dd < best[0] or (dd == best[0] and a < best[1])):
            best = (dd, a)
    return best


def snap_move(raw, d, anchors, thr, F):
    s1 = nearest(raw, anchors, thr)
    s2 = nearest(raw + d, anchors, thr)
    if s1 and (not s2 or s1[0] <= s2[0]):
        return s1[1]
    if s2:
        return s2[1] - d
    return round_frame(raw, F)


# ---------------- FN-E-11 автомат жестов ----------------
def _out(mode, **kw):
    o = {'mode': mode, 'preview': None, 'command': None, 'selection': 'keep', 'playhead': None}
    o.update(kw)
    return o


def build_clip(item, at, cid, F):
    if item['kind'] == 'photo':
        return {'id': cid, 'source': {'kind': 'photo', 'asset_version_id': item['asset_version_id'],
                                      'sha256': item['sha256']},
                'at': at, 'duration': PHOTO_DEFAULT_TICKS, 'fit': 'fit', 'effects': []}
    c = {'id': cid, 'source': {'kind': item['kind'], 'media_id': item['media_id'], 'sha256': item['sha256']},
         'at': at, 'src_in': 0, 'src_out': (item['duration'] // F) * F, 'speed': {'num': 1, 'den': 1},
         'audio': {'gain_db': 0.0, 'fade_in': 0, 'fade_out': 0, 'mute': False}, 'effects': []}
    if item['kind'] == 'video':
        c['fit'] = 'fit'
    return c


def _clip(doc, cid):
    ti, ci = find_clip(doc, cid)
    return doc['tracks'][ti], doc['tracks'][ti]['clips'][ci]


def _snapping(ctx, ev):
    return ctx.get('snap', True) and not ev.get('shift', False)


def _preview(state, ev, ctx):
    doc, view, F = ctx['doc'], ctx['view'], ft(ctx['doc'])
    thr = SNAP_PX * view['tpp']
    tr, c = _clip(doc, state['clip'])
    m = state['mode']
    if m == 'move':
        d = dur(c)
        raw = x_to_tick(ev['x'], view) - state['grab']
        r = row_at(doc, ev['y'])
        tgt = tr['id']
        if r and r['kind'] == clip_kind(c) and not r['locked']:
            tgt = r['track']
        if _snapping(ctx, ev):
            at = snap_move(raw, d, snap_anchors(doc, ctx['playhead'], c['id']), thr, F)
        else:
            at = round_frame(raw, F)
        at = max(0, at)
        others = doc['tracks'][find_track(doc, tgt)]['clips']
        valid = not any(o['id'] != c['id'] and at < end_of(o) and o['at'] < at + d for o in others)
        return {'clip': c['id'], 'at': at, 'track': tgt, 'valid': valid}
    if m == 'trim':
        raw = x_to_tick(ev['x'], view)
        pos = snap_time(raw, snap_anchors(doc, ctx['playhead'], c['id']), thr, F) if _snapping(ctx, ev) \
            else round_frame(raw, F)
        lo, hi = trim_bounds(tr, c, state['edge'], ctx['src_dur'], F)
        pos = max(lo, min(hi, pos))
        if c['source']['kind'] == 'photo':
            return {'clip': c['id'], 'duration': pos - c['at']}
        return {'clip': c['id'], 'edge': state['edge'], 'pos': pos}
    if m == 'transition':
        half = abs(x_to_tick(ev['x'], view) - c['at'])
        ci = [o['id'] for o in tr['clips']].index(c['id'])
        lim = dur(c) // 2 if ci == 0 else min(dur(c), dur(tr['clips'][ci - 1])) // 2
        lim = (lim // F) * F
        return {'clip': c['id'], 'duration': max(min(2 * F, lim), min(lim, 2 * round_frame(half, F)))}
    return None


def trim_bounds(tr, c, edge, src_dur, F):
    """FN-E-11a: допустимый диапазон позиции края (включительно), кратный кадру."""
    ci = [o['id'] for o in tr['clips']].index(c['id'])
    prev_end = end_of(tr['clips'][ci - 1]) if ci > 0 else 0
    nxt = tr['clips'][ci + 1]['at'] if ci + 1 < len(tr['clips']) else None
    if c['source']['kind'] == 'photo':
        hi = nxt if nxt is not None else c['at'] + 3 * 3600 * TPS
        return c['at'] + F, (hi // F) * F
    n, dd = (c.get('speed') or {'num': 1, 'den': 1})['num'], (c.get('speed') or {'num': 1, 'den': 1})['den']
    if edge == 'in':
        earliest = c['at'] - (c['src_in'] * dd) // n
        lo = max(prev_end, -((-earliest) // F) * F, 0)
        return lo, end_of(c) - F
    latest = c['at'] + ((src_dur[c['source']['media_id']] - c['src_in']) * dd) // n
    hi = (latest // F) * F
    if nxt is not None:
        hi = min(hi, nxt)
    return c['at'] + F, hi


def _drop_preview(ev, ctx):
    doc, view, F = ctx['doc'], ctx['view'], ft(ctx['doc'])
    need = 'audio' if ev['item']['kind'] == 'audio' else 'video'
    r = row_at(doc, ev['y'])
    valid = bool(r and r['kind'] == need and not r['locked'] and ev['x'] >= HEADER_W)
    raw = x_to_tick(ev['x'], view)
    at = snap_time(raw, snap_anchors(doc, ctx['playhead']), SNAP_PX * view['tpp'], F) if ctx.get('snap', True) \
        else round_frame(raw, F)
    return {'track': r['track'] if (r and valid) else None, 'at': max(0, at), 'valid': valid}


def gesture(state, ev, ctx):
    doc, view = ctx['doc'], ctx['view']
    F = ft(doc)
    m = state['mode']
    et = ev['type']
    if et == 'cancel':
        return {'mode': 'idle'}, _out('idle')
    if et in ('bin_over', 'bin_drop', 'bin_leave'):
        if m not in ('idle', 'drop'):
            return state, _out(m)
        if et == 'bin_leave':
            return {'mode': 'idle'}, _out('idle')
        p = _drop_preview(ev, ctx)
        if et == 'bin_over':
            return {'mode': 'drop'}, _out('drop', preview=p)
        if not p['valid']:
            return {'mode': 'idle'}, _out('idle')
        cmd = {'op': 'add_clip', 'args': {'track': p['track'], 'clip': build_clip(ev['item'], p['at'],
                                                                                  ctx['new_ids'][0], F),
                                          'ripple': bool(ev.get('alt', False))}}
        return {'mode': 'idle'}, _out('idle', command=cmd)
    if m == 'idle':
        if et != 'down':
            return state, _out('idle')
        h = hit_test(doc, view, ev['x'], ev['y'])
        k = h['kind']
        if k == 'ruler':
            p = max(0, min(doc_end(doc), round_frame(h['tick'], F)))
            return {'mode': 'scrub'}, _out('scrub', playhead=p)
        if k == 'clip_body' and ctx.get('tool') == 'razor':
            if h['locked']:
                return state, _out('idle', selection=h['clip'])
            at = snap_time(x_to_tick(ev['x'], view), snap_anchors(doc, ctx['playhead']), SNAP_PX * view['tpp'], F) \
                if _snapping(ctx, ev) else round_frame(x_to_tick(ev['x'], view), F)
            cmd = {'op': 'split_clip', 'args': {'clip': h['clip'], 'at': at, 'new_id': ctx['new_ids'][0]}}
            return state, _out('idle', command=cmd, selection=h['clip'])
        if k in ('clip_body', 'edge_in', 'edge_out', 'transition'):
            s = {'mode': 'pressed', 'target': k, 'clip': h['clip'], 'x0': ev['x'], 'y0': ev['y'],
                 'locked': h['locked']}
            return s, _out('pressed', selection=h['clip'])
        if k == 'empty':
            return state, _out('idle', selection=None)
        return state, _out('idle')
    if m == 'scrub':
        if et == 'move':
            t = x_to_tick(ev['x'], view)
            return state, _out('scrub', playhead=max(0, min(doc_end(doc), round_frame(t, F))))
        if et == 'up':
            return {'mode': 'idle'}, _out('idle')
        return state, _out('scrub')
    if m == 'pressed':
        if et == 'up':
            return {'mode': 'idle'}, _out('idle')
        if et != 'move' or state['locked']:
            return state, _out('pressed')
        if max(abs(ev['x'] - state['x0']), abs(ev['y'] - state['y0'])) < DRAG_THRESHOLD_PX:
            return state, _out('pressed')
        tr, c = _clip(doc, state['clip'])
        tk = state['target']
        s = {'clip': c['id'], 'orig_track': tr['id']}
        if tk == 'clip_body':
            s.update(mode='move', grab=x_to_tick(state['x0'], view) - c['at'])
        elif tk in ('edge_in', 'edge_out'):
            s.update(mode='trim', edge='in' if tk == 'edge_in' else 'out')
        else:
            s.update(mode='transition')
        return s, _out(s['mode'], preview=_preview(s, ev, ctx))
    if m in ('move', 'trim', 'transition'):
        p = _preview(state, ev, ctx)
        if et == 'move':
            return state, _out(m, preview=p)
        if et != 'up':
            return state, _out(m, preview=p)
        tr, c = _clip(doc, state['clip'])
        cmd = None
        if m == 'move':
            if p['valid'] and (p['at'] != c['at'] or p['track'] != tr['id']):
                a = {'clip': c['id'], 'new_at': p['at']}
                if p['track'] != tr['id']:
                    a['track'] = p['track']
                cmd = {'op': 'move_clip', 'args': a}
        elif m == 'trim':
            if c['source']['kind'] == 'photo':
                if p['duration'] != c['duration']:
                    cmd = {'op': 'set_clip_props', 'args': {'clip': c['id'], 'patch': {'duration': p['duration']}}}
            else:
                cur = c['at'] if state['edge'] == 'in' else end_of(c)
                if p['pos'] != cur:
                    cmd = {'op': 'trim_edge', 'args': {'clip': c['id'], 'edge': state['edge'], 'pos': p['pos'],
                                                        'source_duration': ctx['src_dur'][c['source']['media_id']]}}
        else:
            ti = c['transition_in']
            if p['duration'] != ti['duration']:
                cmd = {'op': 'set_transition', 'args': {'clip': c['id'], 'transition': {
                    'kind': ti['kind'], 'duration': p['duration'], 'align': ti.get('align', 'center')}}}
        return {'mode': 'idle'}, _out('idle', command=cmd)
    return state, _out(m)


# ---------------- FN-E-12 горячие клавиши ----------------
SIMPLE = {' ': 'play_toggle', 'k': 'pause', 'l': 'shuttle_forward', 'j': 'step_back_second', 's': 'split',
          'c': 'tool_razor', 'v': 'tool_select', 'n': 'snap_toggle', 'm': 'add_marker', 'home': 'go_start',
          'end': 'go_end', 'escape': 'escape', '\\': 'zoom_fit', 'arrowup': 'prev_edit', 'arrowdown': 'next_edit'}
REPEATABLE = {'arrowleft', 'arrowright', '+', '=', '-', '_'}


def match_shortcut(ev):
    if ev.get('composing') or ev.get('editing') or ev.get('alt'):
        return None
    key = ev['key'].lower()
    mod = ev.get('ctrl') or ev.get('meta')
    sh = ev.get('shift', False)
    if mod:
        if key == 'z':
            return 'redo' if sh else 'undo'
        if key == 'y' and not sh:
            return 'redo'
        return None
    if ev.get('repeat') and key not in REPEATABLE:
        return None
    if key in ('delete', 'backspace'):
        return 'delete_ripple' if sh else 'delete'
    if key == 'arrowleft':
        return 'step_back_10' if sh else 'step_back_1'
    if key == 'arrowright':
        return 'step_fwd_10' if sh else 'step_fwd_1'
    if key in ('+', '='):
        return 'zoom_in'
    if key in ('-', '_'):
        return 'zoom_out'
    if key == 'i':
        return 'loop_clear_in' if sh else 'loop_in'
    if key == 'o':
        return 'loop_clear_out' if sh else 'loop_out'
    if sh:
        return None
    return SIMPLE.get(key)


# ---------------- FN-E-13 цели команд ----------------
def split_target(doc, selection, playhead):
    def inside(c):
        return c['at'] < playhead < end_of(c)
    if selection:
        loc = find_clip(doc, selection)
        if loc:
            tr = doc['tracks'][loc[0]]
            c = tr['clips'][loc[1]]
            if inside(c) and tr.get('locked') is not True:
                return c['id']
    for r in layout_rows(doc):
        if r['locked']:
            continue
        tr = doc['tracks'][find_track(doc, r['track'])]
        for c in tr['clips']:
            if inside(c):
                return c['id']
    return None


def adjacent_edit(doc, playhead, direction):
    edges = {0, doc_end(doc)}
    for t in doc['tracks']:
        for c in t['clips']:
            edges.add(c['at'])
            edges.add(end_of(c))
    if direction < 0:
        cand = [e for e in edges if e < playhead]
        return max(cand) if cand else playhead
    cand = [e for e in edges if e > playhead]
    return min(cand) if cand else playhead


# ---------------- FN-E-14 автомат воспроизведения ----------------
OP_ORDER = ['clock_stop', 'pause_media', 'unload', 'load', 'sync', 'layers', 'rate', 'play_media', 'audio_sync',
            'clock_start', 'notify']


def vis_span(doc, ti, ci, src_dur):
    return voice_span(doc, ti, ci, src_dur)


def track_needs(doc, ti, t, src_dur):
    tr = doc['tracks'][ti]
    act, up = [], []
    hidden = tr['kind'] == 'video' and tr.get('visible', True) is False
    for ci, c in enumerate(tr['clips']):
        if hidden and c['source']['kind'] == 'photo':
            continue
        s, e = vis_span(doc, ti, ci, src_dur)
        if s <= t < e:
            act.append((s, c['id']))
        elif t < s <= t + PRELOAD_LEAD_TICKS:
            up.append((s, c['id']))
    act.sort()
    up.sort()
    return [x[1] for x in act], [x[1] for x in (act + up)][:2]


def clip_start(doc, cid, src_dur):
    ti, ci = find_clip(doc, cid)
    return vis_span(doc, ti, ci, src_dur)[0]


def new_playback_state():
    return {'mode': 'paused', 't': 0, 'rate': 1, 'decks': {}, 'active': [], 'layers': [], 'loop': None}


def _deck_of(state, track, cid):
    for k in (track + '/a', track + '/b'):
        d = state['decks'].get(k)
        if d and d['clip'] == cid:
            return k
    return None


def _reconcile(state, ctx, now, fx):
    doc, src_dur = ctx['doc'], ctx['src_dur']
    t = state['t']
    tids = {tr['id'] for tr in doc['tracks']}
    for k in sorted(list(state['decks'])):
        if k.split('/')[0] not in tids:
            del state['decks'][k]
            fx.append({'op': 'unload', 'deck': k})
    active = []
    for ti, tr in enumerate(doc['tracks']):
        act, need = track_needs(doc, ti, t, src_dur)
        keys = [tr['id'] + '/a', tr['id'] + '/b']
        held = {state['decks'][k]['clip'] for k in keys if k in state['decks']}
        for cid in need:
            if cid in held:
                continue
            free = [k for k in keys if k not in state['decks']]
            stale = [k for k in keys if k in state['decks'] and state['decks'][k]['clip'] not in need]
            k = (free + stale)[0]
            state['decks'][k] = {'clip': cid, 'ready': False, 'failed': False, 'failures': 0, 'link_ms': now}
            ti2, ci2 = find_clip(doc, cid)
            c = doc['tracks'][ti2]['clips'][ci2]
            fx.append({'op': 'load', 'deck': k, 'clip': cid,
                       'src_ticks': src_ticks_at(c, max(t, clip_start(doc, cid, src_dur)))})
            held.add(cid)
        for cid in act:
            active.append(_deck_of(state, tr['id'], cid))
    active.sort()
    comp = compose_at(doc, t, src_dur)
    layers = []
    for z, L in enumerate(comp['layers']):
        e = {'deck': _deck_of(state, L['track'], L['clip']), 'z': z, 'opacity': L['opacity'],
             'brightness': L['brightness']}
        if 'photo' in L:
            e['photo'] = L['photo']
        layers.append(e)
    if layers != state['layers']:
        fx.append({'op': 'layers', 'layers': layers})
        state['layers'] = layers
    return active


def _media(state, ctx, k):
    loc = find_clip(ctx['doc'], state['decks'][k]['clip'])
    return loc is not None and ctx['doc']['tracks'][loc[0]]['clips'][loc[1]]['source']['kind'] != 'photo'


def _all_ready(state, active):
    return all(state['decks'][k]['ready'] for k in active)


def _sync(state, ctx, k):
    doc = ctx['doc']
    cid = state['decks'][k]['clip']
    ti, ci = find_clip(doc, cid)
    return {'op': 'sync', 'deck': k, 'src_ticks': src_ticks_at(doc['tracks'][ti]['clips'][ci], state['t'])}


def _start(state, ctx, active, fx):
    for k in active:
        if _media(state, ctx, k):
            fx.append(_sync(state, ctx, k))
            fx.append({'op': 'play_media', 'deck': k})
    fx.append({'op': 'audio_sync', 't': state['t']})
    fx.append({'op': 'clock_start', 't': state['t']})
    state['mode'] = 'playing'


def _stall(state, ctx, active, fx):
    fx.append({'op': 'clock_stop'})
    for k in active:
        if _media(state, ctx, k):
            fx.append({'op': 'pause_media', 'deck': k})
    state['mode'] = 'stalled'


def _finish(fx):
    seen = []
    for e in fx:
        if e not in seen:
            seen.append(e)
    return sorted(seen, key=lambda e: (OP_ORDER.index(e['op']), e.get('deck', '')))


def playback(state, ev, ctx):
    state = copy.deepcopy(state)
    doc, src_dur = ctx['doc'], ctx['src_dur']
    end = doc_end(doc)
    fx = []
    et = ev['type']
    now = ev.get('now', 0)
    if et == 'play':
        if state['t'] >= end:
            state['t'] = state['loop'][0] if state['loop'] else 0
        active = _reconcile(state, ctx, now, fx)
        if _all_ready(state, active):
            _start(state, ctx, active, fx)
        else:
            state['mode'] = 'stalled'
    elif et == 'pause':
        active = state['active']
        if state['mode'] in ('playing', 'stalled'):
            fx.append({'op': 'clock_stop'})
            for k in active:
                if _media(state, ctx, k):
                    fx.append({'op': 'pause_media', 'deck': k})
        state['mode'] = 'paused'
        active = _reconcile(state, ctx, now, fx)
    elif et == 'seek':
        was = state['mode']
        state['t'] = max(0, min(end, ev['t']))
        if was in ('playing', 'stalled'):
            fx.append({'op': 'clock_stop'})
            for k in state['active']:
                if k in state['decks'] and _media(state, ctx, k):
                    fx.append({'op': 'pause_media', 'deck': k})
        before = {k: v['link_ms'] for k, v in state['decks'].items()}
        active = _reconcile(state, ctx, now, fx)
        for k in active:
            d = state['decks'][k]
            fresh = k not in before or before[k] != d['link_ms'] or not d['ready']
            if not fresh and now - d['link_ms'] > LINK_REFRESH_AGE_MS:
                d.update(ready=False, link_ms=now)
                fx.append({'op': 'load', 'deck': k, 'clip': d['clip'], 'src_ticks': _sync(state, ctx, k)['src_ticks']})
            elif not fresh and _media(state, ctx, k):
                fx.append(_sync(state, ctx, k))
        if was in ('playing', 'stalled'):
            if _all_ready(state, active):
                _start(state, ctx, active, fx)
            else:
                state['mode'] = 'stalled'
        elif was == 'ended':
            state['mode'] = 'paused'
    elif et == 'tick':
        if state['mode'] != 'playing':
            return state, []
        if state['loop'] and ev['t'] >= state['loop'][1]:
            return playback(state, {'type': 'seek', 't': state['loop'][0], 'now': now}, ctx)
        if ev['t'] >= end:
            state['t'] = end
            fx.append({'op': 'clock_stop'})
            for k in state['active']:
                if k in state['decks'] and _media(state, ctx, k):
                    fx.append({'op': 'pause_media', 'deck': k})
            state['mode'] = 'ended'
            active = []
        else:
            state['t'] = ev['t']
            prev = set(state['active'])
            active = _reconcile(state, ctx, now, fx)
            for k in sorted(prev - set(active)):
                if k in state['decks'] and _media(state, ctx, k):
                    fx.append({'op': 'pause_media', 'deck': k})
            if not _all_ready(state, active):
                _stall(state, ctx, active, fx)
            else:
                for k in sorted(set(active) - prev):
                    if _media(state, ctx, k):
                        fx.append(_sync(state, ctx, k))
                        fx.append({'op': 'play_media', 'deck': k})
    elif et == 'deck_ready':
        d = state['decks'].get(ev['deck'])
        if d:
            d['ready'] = True
        active = state['active']
        if state['mode'] == 'stalled' and _all_ready(state, active):
            _start(state, ctx, active, fx)
    elif et == 'deck_error':
        k = ev['deck']
        d = state['decks'].get(k)
        active = state['active']
        if d:
            d['failures'] += 1
            if d['failures'] <= DECK_MAX_FAILURES:
                ti, ci = find_clip(doc, d['clip'])
                c = doc['tracks'][ti]['clips'][ci]
                d.update(ready=False, link_ms=now)
                fx.append({'op': 'load', 'deck': k, 'clip': d['clip'],
                           'src_ticks': src_ticks_at(c, max(state['t'], clip_start(doc, d['clip'], src_dur)))})
                if state['mode'] == 'playing' and k in active:
                    _stall(state, ctx, active, fx)
            else:
                d.update(ready=True, failed=True)
                fx.append({'op': 'notify', 'code': 'PLAYBACK_SOURCE_FAILED', 'clip': d['clip']})
                if state['mode'] == 'stalled' and _all_ready(state, active):
                    _start(state, ctx, active, fx)
    elif et == 'rate':
        if ev['rate'] in (1, 2, 4) and ev['rate'] != state['rate']:
            state['rate'] = ev['rate']
            fx.append({'op': 'rate', 'rate': ev['rate']})
        active = state['active']
    elif et == 'loop':
        state['loop'] = ev.get('range')
        active = state['active']
    elif et == 'doc':
        state['t'] = min(state['t'], end)
        active = _reconcile(state, ctx, now, fx)
        for k in active:
            if _media(state, ctx, k):
                fx.append(_sync(state, ctx, k))
    else:
        active = state['active']
    state['active'] = active
    return state, _finish(fx)


def summary(state):
    return {'mode': state['mode'], 't': state['t'], 'active': state['active'],
            'decks': {k: {'clip': v['clip'], 'ready': v['ready'], 'failed': v['failed'], 'failures': v['failures']}
                      for k, v in sorted(state['decks'].items())}}


# ---------------- FN-E-15 колёса ↔ CDL ----------------
PHI = [0.0, 2 * math.pi / 3, 4 * math.pi / 3]


def _d(w):
    x, y = w['x'], w['y']
    r = math.hypot(x, y)
    if r > 1:
        x, y, r = x / r, y / r, 1.0
    th = math.atan2(y, x)
    return [r * math.cos(th - p) for p in PHI]


def clamp(v, lo, hi):
    return max(lo, min(hi, v))


def wheels_to_cdl(lift, gamma, gain):
    dl, dg, dn = _d(lift), _d(gamma), _d(gain)
    vl, vg, vn = (clamp(w['v'], -1, 1) for w in (lift, gamma, gain))
    return {'offset': [clamp(0.1 * vl + 0.1 * dl[i], -1, 1) for i in range(3)],
            'power': [clamp(2 ** (-(0.5 * vg + 0.5 * dg[i])), 0.1, 4) for i in range(3)],
            'slope': [clamp(2 ** (vn + 0.5 * dn[i]), 0, 4) for i in range(3)]}


def _a_to_wheel(a, k):
    v = sum(a) / 3
    dd = [(x - v) / k for x in a]
    x = (2 / 3) * sum(dd[i] * math.cos(PHI[i]) for i in range(3))
    y = (2 / 3) * sum(dd[i] * math.sin(PHI[i]) for i in range(3))
    return {'x': x, 'y': y, 'v': v}


def cdl_to_wheels(cdl):
    lift = _a_to_wheel([o / 0.1 for o in cdl['offset']], 1.0)
    gamma = _a_to_wheel([-math.log2(p) / 0.5 for p in cdl['power']], 1.0)
    gain = _a_to_wheel([math.log2(s) for s in cdl['slope']], 0.5)
    return {'lift': lift, 'gamma': gamma, 'gain': gain}


# ---------------- FN-E-16 правка кривых ----------------
CURVE_MIN_GAP = 1 / 256


def curve_add(points, x, y):
    x, y = clamp(x, 0, 1), clamp(y, 0, 1)
    if len(points) >= 16:
        return {'error': 'CURVE_FULL'}
    if any(abs(p[0] - x) < CURVE_MIN_GAP for p in points):
        return {'error': 'CURVE_X_TAKEN'}
    pts = sorted([list(p) for p in points] + [[x, y]], key=lambda p: p[0])
    return {'points': pts, 'index': pts.index([x, y])}


def curve_move(points, i, x, y):
    pts = [list(p) for p in points]
    n = len(pts)
    if not (0 <= i < n):
        return {'error': 'CURVE_INDEX'}
    y = clamp(y, 0, 1)
    if i == 0 or i == n - 1:
        x = pts[i][0]
    else:
        x = clamp(x, pts[i - 1][0] + CURVE_MIN_GAP, pts[i + 1][0] - CURVE_MIN_GAP)
    pts[i] = [x, y]
    return {'points': pts}


def curve_remove(points, i):
    n = len(points)
    if not (0 <= i < n):
        return {'error': 'CURVE_INDEX'}
    if i == 0 or i == n - 1:
        return {'error': 'CURVE_ENDPOINT'}
    return {'points': [list(p) for k, p in enumerate(points) if k != i]}


# ---------------- FN-E-17 осциллограммы ----------------
def luma8(r, g, b):
    return int(math.floor(0.2126 * r + 0.7152 * g + 0.0722 * b + 0.5))


def scopes(w, h, px):
    hist = {'r': {}, 'g': {}, 'b': {}, 'y': {}}
    cols = min(w, 256)
    wave = {}
    vec = {}
    for idx, (r, g, b) in enumerate(px):
        x = idx % w
        yv = luma8(r, g, b)
        for ch, v in (('r', r), ('g', g), ('b', b), ('y', yv)):
            hist[ch][v] = hist[ch].get(v, 0) + 1
        col = (x * cols) // w
        wave[(col, yv)] = wave.get((col, yv), 0) + 1
        cb = (-0.1146 * r) + (-0.3854 * g) + (0.5 * b)
        cr = (0.5 * r) + (-0.4542 * g) + (-0.0458 * b)
        u = int(clamp(math.floor(cb + 128 + 1e-9), 0, 255))
        v = int(clamp(math.floor(cr + 128 + 1e-9), 0, 255))
        vec[(u, v)] = vec.get((u, v), 0) + 1
    return {'histogram': {ch: sorted([[k, n] for k, n in d.items()]) for ch, d in hist.items()},
            'waveform': sorted([[c, y, n] for (c, y), n in wave.items()]),
            'vectorscope': sorted([[u, v, n] for (u, v), n in vec.items()])}
