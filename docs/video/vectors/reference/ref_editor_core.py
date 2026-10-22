"""Эталон (одноразовый) чистых функций редактора, документ 23. Только стандартная библиотека.
Ядро: время, переходы, состав кадра, огибающие звука, команды FN-T-15…28. Генератор векторов — ref_editor.py."""
import copy
import json
import math
import re
from math import gcd

TPS = 705_600_000
ID_RE = re.compile(r'^[A-Za-z0-9_-]{1,64}$')
EFFECTS_PER_CLIP_MAX = 16
MARKER_LABEL_MAX = 200
MAX_TRACKS_PER_KIND = 4
MAX_LAYERS_AT_POINT = 8
BIG = 1 << 62


class Err(Exception):
    pass


# ---------------- общее ----------------
def ft(doc):
    f = doc['video']['fps']
    v = TPS * f['den']
    assert v % f['num'] == 0
    return v // f['num']


def sp(c):
    s = c.get('speed') or {'num': 1, 'den': 1}
    return s['num'], s['den']


def dur(c):
    if c['source']['kind'] == 'photo':
        return c['duration']
    n, d = sp(c)
    return ((c['src_out'] - c['src_in']) * d) // n


def end_of(c):
    return c['at'] + dur(c)


def doc_end(doc):
    e = 0
    for t in doc['tracks']:
        for c in t['clips']:
            e = max(e, end_of(c))
    return e


def find_clip(doc, cid):
    for ti, t in enumerate(doc['tracks']):
        for ci, c in enumerate(t['clips']):
            if c['id'] == cid:
                return ti, ci
    return None


def find_track(doc, tid):
    for ti, t in enumerate(doc['tracks']):
        if t['id'] == tid:
            return ti
    return None


def clip_kind(c):
    return 'audio' if c['source']['kind'] == 'audio' else 'video'


def src_ticks_at(c, t):
    """FN-E-01a: время источника в момент таймлайна t (деление с округлением вниз; t может быть раньше c.at)."""
    if c['source']['kind'] == 'photo':
        return None
    n, d = sp(c)
    return c['src_in'] + ((t - c['at']) * n) // d


def frac(n, d):
    g = gcd(n, d) or 1
    return {'num': n // g, 'den': d // g}


def round_frame(t, F):
    return ((t + F // 2) // F) * F


# ---------------- FN-E-02 effectiveTransition ----------------
def head_avail(c):
    if c['source']['kind'] == 'photo':
        return BIG
    n, d = sp(c)
    return (c['src_in'] * d) // n


def tail_avail(c, src_dur):
    if c['source']['kind'] == 'photo':
        return BIG
    n, d = sp(c)
    return ((src_dur[c['source']['media_id']] - c['src_out']) * d) // n


def effective_transition(doc, ti, ci, src_dur):
    F = ft(doc)
    t = doc['tracks'][ti]
    c = t['clips'][ci]
    tr = c.get('transition_in')
    if t['kind'] != 'video' or not tr or tr['kind'] == 'cut':
        return None
    D = tr['duration']
    kind = tr['kind']
    prev = t['clips'][ci - 1] if ci > 0 else None
    adjacent = prev is not None and end_of(prev) == c['at']
    if kind in ('crossfade', 'dip_to_black') and not adjacent:
        kind = 'fade_in'
    if kind == 'fade_in':
        d = min(D, dur(c))
        if d <= 0:
            return None
        return {'clip': c['id'], 'prev': None, 'kind': 'fade_in', 'start': c['at'], 'end': c['at'] + d,
                'duration': d, 'requested': D, 'shortened': d < D}
    if kind == 'dip_to_black':
        half = D // 2
        if half <= 0:
            return None
        return {'clip': c['id'], 'prev': prev['id'], 'kind': 'dip_to_black', 'start': c['at'] - half,
                'end': c['at'] + half, 'duration': 2 * half, 'requested': D, 'shortened': 2 * half < D}
    align = tr.get('align', 'center')
    if align == 'center':
        half = min(D // 2, head_avail(c), tail_avail(prev, src_dur))
        d = 2 * half
        s, e = c['at'] - half, c['at'] + half
    elif align == 'start':
        d = min(D, tail_avail(prev, src_dur))
        s, e = c['at'], c['at'] + d
    else:
        d = min(D, head_avail(c))
        s, e = c['at'] - d, c['at']
    if d <= 0:
        return {'clip': c['id'], 'prev': prev['id'], 'kind': 'cut', 'start': c['at'], 'end': c['at'],
                'duration': 0, 'requested': D, 'shortened': True}
    return {'clip': c['id'], 'prev': prev['id'], 'kind': 'crossfade', 'start': s, 'end': e,
            'duration': d, 'requested': D, 'shortened': d < D}


# ---------------- FN-E-03 photoTransformAt ----------------
def ease(u, easing):
    if easing == 'linear':
        return u
    return u * u * (3.0 - 2.0 * u)


def photo_transform_at(c, t):
    m = c.get('motion')
    if not m:
        return {'scale': 1.0, 'center': [0.5, 0.5]}
    d = c['duration']
    u = min(max(t - c['at'], 0), d) / d
    e = ease(u, m['easing'])
    f, to = m['from'], m['to']
    return {'scale': f['scale'] + (to['scale'] - f['scale']) * e,
            'center': [f['center'][0] + (to['center'][0] - f['center'][0]) * e,
                       f['center'][1] + (to['center'][1] - f['center'][1]) * e]}


# ---------------- FN-E-04 composeAt ----------------
def _layer(tr, c, t, opacity, bright):
    L = {'track': tr['id'], 'clip': c['id'], 'kind': c['source']['kind'], 'opacity': opacity, 'brightness': bright,
         'src_ticks': src_ticks_at(c, t)}
    if c['source']['kind'] == 'photo':
        L['photo'] = photo_transform_at(c, t)
    return L


def video_layers_track(doc, ti, t, src_dur):
    tr = doc['tracks'][ti]
    clips = tr['clips']
    for ci in range(len(clips)):
        et = effective_transition(doc, ti, ci, src_dur)
        if et and et['kind'] != 'cut' and et['start'] <= t < et['end']:
            c = clips[ci]
            if et['kind'] == 'fade_in':
                return [_layer(tr, c, t, frac(t - et['start'], et['duration']), frac(1, 1))]
            prev = clips[ci - 1]
            if et['kind'] == 'crossfade':
                return [_layer(tr, prev, t, frac(1, 1), frac(1, 1)),
                        _layer(tr, c, t, frac(t - et['start'], et['duration']), frac(1, 1))]
            half = et['duration'] // 2
            if t < c['at']:
                return [_layer(tr, prev, t, frac(1, 1), frac(c['at'] - t, half))]
            return [_layer(tr, c, t, frac(1, 1), frac(t - c['at'], half))]
    for c in clips:
        if c['at'] <= t < end_of(c):
            return [_layer(tr, c, t, frac(1, 1), frac(1, 1))]
    return []


def voice_span(doc, ti, ci, src_dur):
    tr = doc['tracks'][ti]
    c = tr['clips'][ci]
    s, e = c['at'], end_of(c)
    if tr['kind'] == 'video':
        et = effective_transition(doc, ti, ci, src_dur)
        if et and et['kind'] == 'crossfade':
            s = min(s, et['start'])
        if ci + 1 < len(tr['clips']):
            en = effective_transition(doc, ti, ci + 1, src_dur)
            if en and en['kind'] == 'crossfade':
                e = max(e, en['end'])
    return s, e


def audio_env(doc, ti, ci, t, src_dur):
    """Множитель огибающей голоса в момент t (float); вне промежутка звучания — 0."""
    tr = doc['tracks'][ti]
    c = tr['clips'][ci]
    s, e = voice_span(doc, ti, ci, src_dur)
    if not (s <= t < e):
        return 0.0
    f = 1.0
    a = c.get('audio') or {}
    fi, fo = a.get('fade_in', 0), a.get('fade_out', 0)
    if fi > 0 and t - c['at'] < fi:
        f *= max(t - c['at'], 0) / fi
    if fo > 0 and end_of(c) - t < fo:
        f *= max(end_of(c) - t, 0) / fo
    if tr['kind'] == 'video':
        et = effective_transition(doc, ti, ci, src_dur)
        if et and et['kind'] in ('crossfade', 'dip_to_black') and et['start'] <= t < et['end']:
            if et['kind'] == 'crossfade':
                f *= (t - et['start']) / et['duration']
            else:
                f *= 0.0 if t < c['at'] else (t - c['at']) / (et['duration'] // 2)
        if ci + 1 < len(tr['clips']):
            en = effective_transition(doc, ti, ci + 1, src_dur)
            if en and en['kind'] in ('crossfade', 'dip_to_black') and en['start'] <= t < en['end']:
                nxt = tr['clips'][ci + 1]
                if en['kind'] == 'crossfade':
                    f *= (en['end'] - t) / en['duration']
                else:
                    f *= 0.0 if t >= nxt['at'] else (nxt['at'] - t) / (en['duration'] // 2)
    return f


def has_audio(c):
    return c['source']['kind'] in ('video', 'audio')


def compose_at(doc, t, src_dur):
    layers = []
    for ti, tr in enumerate(doc['tracks']):
        if tr['kind'] == 'video' and tr.get('visible', True) is not False:
            layers += video_layers_track(doc, ti, t, src_dur)
    voices = []
    for ti, tr in enumerate(doc['tracks']):
        for ci, c in enumerate(tr['clips']):
            if not has_audio(c):
                continue
            s, e = voice_span(doc, ti, ci, src_dur)
            if not (s <= t < e):
                continue
            a = c.get('audio') or {}
            gain_db = float(a.get('gain_db', 0.0)) + (float(tr.get('gain_db', 0.0)) if tr['kind'] == 'audio' else 0.0)
            muted = bool(a.get('mute', False)) or (tr['kind'] == 'audio' and bool(tr.get('muted', False)))
            voices.append({'track': tr['id'], 'clip': c['id'], 'src_ticks': src_ticks_at(c, t), 'gain_db': gain_db,
                           'env': round(audio_env(doc, ti, ci, t, src_dur), 12), 'muted': muted})
    return {'t': t, 'end': doc_end(doc), 'layers': layers, 'voices': voices,
            'over_limit': len(layers) > MAX_LAYERS_AT_POINT}


# ---------------- FN-E-05 audioEnvelope, FN-E-06 scheduleEnvelope ----------------
def audio_envelope(doc, clip_id, src_dur):
    ti, ci = find_clip(doc, clip_id)
    tr = doc['tracks'][ti]
    c = tr['clips'][ci]
    s, e = voice_span(doc, ti, ci, src_dur)
    pts = {s, e}
    a = c.get('audio') or {}
    if a.get('fade_in', 0) > 0:
        pts.update([c['at'], c['at'] + a['fade_in']])
    if a.get('fade_out', 0) > 0:
        pts.update([end_of(c) - a['fade_out'], end_of(c)])
    if tr['kind'] == 'video':
        for k in (ci, ci + 1):
            if k < len(tr['clips']):
                et = effective_transition(doc, ti, k, src_dur)
                if et and et['kind'] in ('crossfade', 'dip_to_black'):
                    pts.update([et['start'], et['end'], tr['clips'][k]['at']])
    out = []
    for p in sorted(x for x in pts if s <= x <= e):
        v = audio_env(doc, ti, ci, p, src_dur) if p < e else end_value(doc, ti, ci, src_dur)
        out.append([p, round(v, 12)])
    return {'clip': clip_id, 'start': s, 'end': e, 'points': out}


def end_value(doc, ti, ci, src_dur):
    """Значение огибающей в правой границе промежутка (предел слева)."""
    tr = doc['tracks'][ti]
    c = tr['clips'][ci]
    s, e = voice_span(doc, ti, ci, src_dur)
    a = c.get('audio') or {}
    if a.get('fade_out', 0) > 0 and e == end_of(c):
        return 0.0
    if tr['kind'] == 'video' and ci + 1 < len(tr['clips']):
        en = effective_transition(doc, ti, ci + 1, src_dur)
        if en and en['kind'] == 'crossfade' and e == en['end']:
            return 0.0
        if en and en['kind'] == 'dip_to_black' and e == end_of(c):
            return 0.0
    return 1.0


def schedule_envelope(points, t0, ctx_time0, rate):
    out = []
    prev = None
    for tick, v in points:
        if tick < t0:
            prev = (tick, v)
            continue
        if prev is not None and not out and tick > t0:
            pt, pv = prev
            out.append([round(ctx_time0, 9), round(pv + (v - pv) * (t0 - pt) / (tick - pt), 12)])
        out.append([round(ctx_time0 + (tick - t0) / TPS / rate, 9), v])
    if not out and prev is not None:
        out.append([round(ctx_time0, 9), prev[1]])
    return out


# ---------------- Команды FN-T-15…28 ----------------
def all_ids(doc):
    ids = set()
    for t in doc['tracks']:
        ids.add(t['id'])
        for c in t['clips']:
            ids.add(c['id'])
            for e in c.get('effects', []):
                ids.add(e['id'])
    for m in doc.get('markers', []):
        ids.add(m['id'])
    for e in doc.get('master', {}).get('effects', []):
        ids.add(e['id'])
    return ids


def num_ok(v, lo, hi):
    return isinstance(v, (int, float)) and not isinstance(v, bool) and math.isfinite(v) and lo <= v <= hi


def vec_ok(v, n, lo, hi):
    return isinstance(v, list) and len(v) == n and all(num_ok(x, lo, hi) for x in v)


def curve_ok(pts):
    if not isinstance(pts, list) or not (2 <= len(pts) <= 16):
        return False
    px = -1.0
    for p in pts:
        if not (isinstance(p, list) and len(p) == 2 and num_ok(p[0], 0, 1) and num_ok(p[1], 0, 1)):
            return False
        if p[0] <= px:
            return False
        px = p[0]
    return True


BASIC_KEYS = {'exposure_ev': (-5, 5)}
for _k in ('temperature', 'tint', 'contrast', 'highlights', 'shadows', 'whites', 'blacks', 'saturation', 'vibrance'):
    BASIC_KEYS[_k] = (-100, 100)


def effect_param_ok(doc, e):
    p = e.get('params')
    if not isinstance(p, dict):
        return False
    ty = e['type']
    if ty == 'color.basic':
        return all(k in BASIC_KEYS and num_ok(v, *BASIC_KEYS[k]) for k, v in p.items())
    if ty == 'color.cdl':
        return (set(p) == {'slope', 'offset', 'power', 'saturation'} and vec_ok(p['slope'], 3, 0, 4)
                and vec_ok(p['offset'], 3, -1, 1) and vec_ok(p['power'], 3, 0.1, 4) and num_ok(p['saturation'], 0, 4))
    if ty == 'color.curves':
        return len(p) > 0 and all(k in ('master', 'r', 'g', 'b') and curve_ok(v) for k, v in p.items())
    if ty == 'color.lut3d':
        lt = p.get('lut')
        return (set(p) == {'lut', 'intensity'} and isinstance(lt, dict) and set(lt) == {'kind', 'media_id', 'sha256'}
                and lt.get('kind') == 'lut' and num_ok(p['intensity'], 0, 1))
    if ty == 'audio.gain':
        return set(p) == {'gain_db'} and num_ok(p['gain_db'], -60, 24)
    if ty == 'audio.fade':
        return set(p) == {'fade_in', 'fade_out'} and all(
            isinstance(p[k], int) and not isinstance(p[k], bool) and p[k] >= 0 for k in p)
    if ty == 'audio.duck':
        st = p.get('sidechain_tracks')
        audio_tracks = {t['id'] for t in doc['tracks'] if t['kind'] == 'audio'}
        return (set(p) == {'sidechain_tracks', 'threshold_db', 'depth_db', 'attack_ms', 'release_ms'}
                and isinstance(st, list) and 1 <= len(st) <= 4 and len(set(st)) == len(st)
                and all(x in audio_tracks for x in st)
                and num_ok(p['threshold_db'], -60, 0) and num_ok(p['depth_db'], 0, 40)
                and num_ok(p['attack_ms'], 1, 1000) and num_ok(p['release_ms'], 10, 5000))
    return False


CREATABLE = ('color.basic', 'color.cdl', 'color.curves', 'color.lut3d', 'audio.gain', 'audio.fade', 'audio.duck')


def _owner(doc, owner):
    if owner == 'master':
        return doc['master']['effects'], 'master'
    loc = find_clip(doc, owner)
    if loc is None:
        raise Err('OWNER_NOT_FOUND')
    c = doc['tracks'][loc[0]]['clips'][loc[1]]
    return c['effects'], c['source']['kind']


def add_clip(doc, track, clip, ripple=False):
    doc = copy.deepcopy(doc)
    F = ft(doc)
    ti = find_track(doc, track)
    if ti is None:
        raise Err('TRACK_NOT_FOUND')
    tr = doc['tracks'][ti]
    if clip_kind(clip) != tr['kind']:
        raise Err('ADD_TRACK_KIND')
    new_ids = [clip['id']] + [e['id'] for e in clip.get('effects', [])]
    if any(not isinstance(i, str) or not ID_RE.match(i) for i in new_ids):
        raise Err('ID_INVALID')
    ex = all_ids(doc)
    if any(i in ex for i in new_ids) or len(set(new_ids)) != len(new_ids):
        raise Err('ID_DUPLICATE')
    d = dur(clip)
    if clip['at'] < 0 or clip['at'] % F or d % F:
        raise Err('ADD_NOT_ALIGNED')
    if d <= 0:
        raise Err('ADD_EMPTY')
    at = clip['at']
    if ripple:
        if any(o['at'] < at < end_of(o) for o in tr['clips']):
            raise Err('ADD_OVERLAP')
        for o in tr['clips']:
            if o['at'] >= at:
                o['at'] += d
    elif any(at < end_of(o) and o['at'] < at + d for o in tr['clips']):
        raise Err('ADD_OVERLAP')
    tr['clips'].append(copy.deepcopy(clip))
    tr['clips'].sort(key=lambda x: x['at'])
    return doc, {'op': 'delete_clip', 'args': {'clip': clip['id'], 'ripple': bool(ripple)}}


def delete_clip(doc, cid, ripple):
    """FN-T-08 (существующая) — нужна как обратная операция."""
    doc = copy.deepcopy(doc)
    loc = find_clip(doc, cid)
    if loc is None:
        raise Err('CLIP_NOT_FOUND')
    tr = doc['tracks'][loc[0]]
    c = tr['clips'].pop(loc[1])
    d = dur(c)
    if ripple:
        for o in tr['clips']:
            if o['at'] > c['at']:
                o['at'] -= d
    return doc


def duplicate_clip(doc, cid, new_id):
    doc = copy.deepcopy(doc)
    loc = find_clip(doc, cid)
    if loc is None:
        raise Err('CLIP_NOT_FOUND')
    tr = doc['tracks'][loc[0]]
    c = tr['clips'][loc[1]]
    b = copy.deepcopy(c)
    b['id'] = new_id
    b['at'] = end_of(c)
    b.pop('transition_in', None)
    for k, e in enumerate(b.get('effects', []), 1):
        e['id'] = f'{new_id}-e{k}'
    nids = [new_id] + [e['id'] for e in b.get('effects', [])]
    if any(not isinstance(i, str) or not ID_RE.match(i) for i in nids):
        raise Err('ID_INVALID')
    if any(i in all_ids(doc) for i in nids):
        raise Err('ID_DUPLICATE')
    d = dur(b)
    if any(b['at'] < end_of(o) and o['at'] < b['at'] + d for o in tr['clips']):
        raise Err('DUP_NO_SPACE')
    tr['clips'].insert(loc[1] + 1, b)
    return doc, {'op': 'delete_clip', 'args': {'clip': new_id, 'ripple': False}}


def set_transition(doc, cid, transition):
    doc = copy.deepcopy(doc)
    F = ft(doc)
    loc = find_clip(doc, cid)
    if loc is None:
        raise Err('CLIP_NOT_FOUND')
    tr = doc['tracks'][loc[0]]
    c = tr['clips'][loc[1]]
    if tr['kind'] != 'video':
        raise Err('TRANSITION_TRACK_KIND')
    old = copy.deepcopy(c.get('transition_in'))
    if transition is None or transition.get('kind') == 'cut':
        c.pop('transition_in', None)
    else:
        k = transition.get('kind')
        if k not in ('crossfade', 'dip_to_black', 'fade_in'):
            raise Err('TRANSITION_KIND')
        D = transition.get('duration')
        if not isinstance(D, int) or isinstance(D, bool) or D <= 0 or D % F:
            raise Err('TRANSITION_NOT_ALIGNED')
        al = transition.get('align', 'center')
        if al not in ('center', 'start', 'end') or (k != 'crossfade' and al != 'center'):
            raise Err('TRANSITION_ALIGN')
        if loc[1] == 0 and k != 'fade_in':
            raise Err('TRANSITION_FIRST_CLIP')
        lim = dur(c) // 2 if loc[1] == 0 else min(dur(c), dur(tr['clips'][loc[1] - 1])) // 2
        if D > lim:
            raise Err('TRANSITION_TOO_LONG')
        c['transition_in'] = {'kind': k, 'duration': D, 'align': al}
    return doc, {'op': 'set_transition', 'args': {'clip': cid, 'transition': old}}


def set_effect(doc, owner, effect, index=None):
    doc = copy.deepcopy(doc)
    lst, okind = _owner(doc, owner)
    ty = effect.get('type')
    if ty not in CREATABLE:
        raise Err('EFFECT_TYPE')
    is_audio_fx = ty.startswith('audio.')
    if (okind == 'audio') != is_audio_fx:
        raise Err('EFFECT_OWNER_KIND')
    if not isinstance(effect.get('enabled'), bool) or set(effect) - {'id', 'type', 'enabled', 'params', 'meta'} \
            or not effect_param_ok(doc, effect):
        raise Err('EFFECT_PARAM')
    eid = effect.get('id')
    if not isinstance(eid, str) or not ID_RE.match(eid):
        raise Err('ID_INVALID')
    pos = next((i for i, e in enumerate(lst) if e['id'] == eid), None)
    if pos is not None:
        old = copy.deepcopy(lst[pos])
        lst[pos] = copy.deepcopy(effect)
        return doc, {'op': 'set_effect', 'args': {'owner': owner, 'effect': old}}
    if eid in all_ids(doc):
        raise Err('ID_DUPLICATE')
    if len(lst) >= EFFECTS_PER_CLIP_MAX:
        raise Err('EFFECT_LIMIT')
    idx = len(lst) if index is None else index
    if not isinstance(idx, int) or isinstance(idx, bool) or not (0 <= idx <= len(lst)):
        raise Err('EFFECT_INDEX')
    lst.insert(idx, copy.deepcopy(effect))
    return doc, {'op': 'remove_effect', 'args': {'owner': owner, 'effect': eid}}


def remove_effect(doc, owner, eid):
    doc = copy.deepcopy(doc)
    lst, _ = _owner(doc, owner)
    pos = next((i for i, e in enumerate(lst) if e['id'] == eid), None)
    if pos is None:
        raise Err('EFFECT_NOT_FOUND')
    old = lst.pop(pos)
    return doc, {'op': 'set_effect', 'args': {'owner': owner, 'effect': old, 'index': pos}}


def move_effect(doc, owner, eid, new_index):
    doc = copy.deepcopy(doc)
    lst, _ = _owner(doc, owner)
    pos = next((i for i, e in enumerate(lst) if e['id'] == eid), None)
    if pos is None:
        raise Err('EFFECT_NOT_FOUND')
    if not isinstance(new_index, int) or isinstance(new_index, bool) or not (0 <= new_index < len(lst)):
        raise Err('EFFECT_INDEX')
    e = lst.pop(pos)
    lst.insert(new_index, e)
    return doc, {'op': 'move_effect', 'args': {'owner': owner, 'effect': eid, 'index': pos}}


PROPS_BY_KIND = {'video': ('fit', 'transform', 'audio'), 'photo': ('fit', 'transform', 'motion', 'duration'),
                 'audio': ('audio',)}
AUDIO_DEF = {'gain_db': 0.0, 'fade_in': 0, 'fade_out': 0, 'mute': False}
TRANSFORM_DEF = {'scale': 1.0, 'position': [0, 0], 'rotate': 0.0, 'crop': [0, 0, 0, 0]}


def _transform_ok(x):
    return (set(x) == set(TRANSFORM_DEF) and num_ok(x['scale'], 0.1, 8) and vec_ok(x['position'], 2, -1, 1)
            and num_ok(x['rotate'], -180, 180) and vec_ok(x['crop'], 4, 0, 0.45))


def _motion_ok(m):
    def pt(p):
        return isinstance(p, dict) and set(p) == {'scale', 'center'} and num_ok(p['scale'], 1, 4) and vec_ok(
            p['center'], 2, 0, 1)
    return (isinstance(m, dict) and set(m) == {'kind', 'from', 'to', 'easing'} and m['kind'] == 'pan_zoom'
            and pt(m['from']) and pt(m['to']) and m['easing'] in ('linear', 'ease_in_out'))


def set_clip_props(doc, cid, patch):
    doc = copy.deepcopy(doc)
    F = ft(doc)
    loc = find_clip(doc, cid)
    if loc is None:
        raise Err('CLIP_NOT_FOUND')
    tr = doc['tracks'][loc[0]]
    c = tr['clips'][loc[1]]
    allowed = PROPS_BY_KIND[c['source']['kind']]
    if not isinstance(patch, dict) or not patch or any(k not in allowed for k in patch):
        raise Err('PROPS_KEY')
    old = {k: copy.deepcopy(c.get(k)) for k in sorted(patch)}
    for k in sorted(patch):
        v = patch[k]
        if v is None:
            if k == 'duration':
                raise Err('PROPS_RANGE')
            c.pop(k, None)
        elif k == 'fit':
            if v not in ('fit', 'fill', 'none'):
                raise Err('PROPS_RANGE')
            c['fit'] = v
        elif k == 'transform':
            if not isinstance(v, dict) or any(x not in TRANSFORM_DEF for x in v):
                raise Err('PROPS_KEY')
            m = copy.deepcopy(c.get('transform') or TRANSFORM_DEF)
            m.update(copy.deepcopy(v))
            if not _transform_ok(m):
                raise Err('PROPS_RANGE')
            c['transform'] = m
        elif k == 'audio':
            if not isinstance(v, dict) or any(x not in AUDIO_DEF for x in v):
                raise Err('PROPS_KEY')
            m = copy.deepcopy(c.get('audio') or AUDIO_DEF)
            m.update(copy.deepcopy(v))
            if not (num_ok(m['gain_db'], -60, 24) and isinstance(m['mute'], bool) and all(
                    isinstance(m[x], int) and not isinstance(m[x], bool) and m[x] >= 0 and m[x] % F == 0
                    for x in ('fade_in', 'fade_out'))):
                raise Err('PROPS_RANGE')
            c['audio'] = m
        elif k == 'motion':
            if not _motion_ok(v):
                raise Err('PROPS_RANGE')
            c['motion'] = copy.deepcopy(v)
        elif k == 'duration':
            if not isinstance(v, int) or isinstance(v, bool) or v < F or v % F:
                raise Err('PROPS_RANGE')
            c['duration'] = v
    if 'audio' in c and c['audio']['fade_in'] + c['audio']['fade_out'] > dur(c):
        raise Err('PROPS_RANGE')
    if loc[1] + 1 < len(tr['clips']) and end_of(c) > tr['clips'][loc[1] + 1]['at']:
        raise Err('PROPS_OVERLAP')
    return doc, {'op': 'set_clip_props', 'args': {'clip': cid, 'patch': old}}


TRACK_PROPS = {'video': ('locked', 'visible'), 'audio': ('locked', 'muted', 'gain_db')}


def set_track_props(doc, tid, patch):
    doc = copy.deepcopy(doc)
    ti = find_track(doc, tid)
    if ti is None:
        raise Err('TRACK_NOT_FOUND')
    tr = doc['tracks'][ti]
    if not isinstance(patch, dict) or not patch or any(k not in TRACK_PROPS[tr['kind']] for k in patch):
        raise Err('PROPS_KEY')
    old = {k: copy.deepcopy(tr.get(k)) for k in sorted(patch)}
    for k in sorted(patch):
        v = patch[k]
        if v is None:
            tr.pop(k, None)
        elif k == 'gain_db':
            if not num_ok(v, -60, 24):
                raise Err('PROPS_RANGE')
            tr[k] = v
        else:
            if not isinstance(v, bool):
                raise Err('PROPS_RANGE')
            tr[k] = v
    return doc, {'op': 'set_track_props', 'args': {'track': tid, 'patch': old}}


def add_track(doc, kind, tid, index=None, props=None):
    doc = copy.deepcopy(doc)
    if kind not in ('video', 'audio'):
        raise Err('TRACK_KIND')
    if not isinstance(tid, str) or not ID_RE.match(tid):
        raise Err('ID_INVALID')
    if tid in all_ids(doc):
        raise Err('ID_DUPLICATE')
    nv = sum(1 for t in doc['tracks'] if t['kind'] == 'video')
    na = len(doc['tracks']) - nv
    if (nv if kind == 'video' else na) >= MAX_TRACKS_PER_KIND:
        raise Err('TRACK_LIMIT')
    lo, hi = (0, nv) if kind == 'video' else (nv, nv + na)
    idx = hi if index is None else index
    if not isinstance(idx, int) or isinstance(idx, bool) or not (lo <= idx <= hi):
        raise Err('TRACK_INDEX')
    doc['tracks'].insert(idx, {'id': tid, 'kind': kind, 'clips': []})
    if props:
        doc, _ = set_track_props(doc, tid, props)
    return doc, {'op': 'remove_track', 'args': {'track': tid}}


def remove_track(doc, tid):
    doc = copy.deepcopy(doc)
    ti = find_track(doc, tid)
    if ti is None:
        raise Err('TRACK_NOT_FOUND')
    tr = doc['tracks'][ti]
    if tr['clips']:
        raise Err('TRACK_NOT_EMPTY')
    for t in doc['tracks']:
        for c in t['clips']:
            for e in c.get('effects', []):
                if e['type'] == 'audio.duck' and tid in e['params'].get('sidechain_tracks', []):
                    raise Err('TRACK_REFERENCED')
    if sum(1 for t in doc['tracks'] if t['kind'] == tr['kind']) == 1:
        raise Err('TRACK_LAST')
    doc['tracks'].pop(ti)
    args = {'kind': tr['kind'], 'track': tid, 'index': ti}
    props = {k: tr[k] for k in ('locked', 'visible', 'muted', 'gain_db') if k in tr}
    if props:
        args['props'] = props
    return doc, {'op': 'add_track', 'args': args}


def _sort_markers(doc):
    doc['markers'].sort(key=lambda m: (m['at'], m['id']))


def _marker_check(doc, m, skip_id=None):
    F = ft(doc)
    if not isinstance(m['at'], int) or isinstance(m['at'], bool) or m['at'] < 0 or m['at'] % F:
        raise Err('MARKER_NOT_ALIGNED')
    if m['kind'] not in ('cut', 'beat', 'user'):
        raise Err('MARKER_KIND')
    if not isinstance(m['label'], str) or len(m['label']) > MARKER_LABEL_MAX:
        raise Err('MARKER_LABEL')
    if any(o['id'] != skip_id and o['at'] == m['at'] and o['kind'] == m['kind'] for o in doc['markers']):
        raise Err('MARKER_EXISTS')


def add_marker(doc, marker):
    doc = copy.deepcopy(doc)
    if not isinstance(marker, dict) or set(marker) != {'id', 'at', 'kind', 'label'}:
        raise Err('MARKER_KIND')
    if not isinstance(marker['id'], str) or not ID_RE.match(marker['id']):
        raise Err('ID_INVALID')
    if marker['id'] in all_ids(doc):
        raise Err('ID_DUPLICATE')
    _marker_check(doc, marker)
    doc['markers'].append(copy.deepcopy(marker))
    _sort_markers(doc)
    return doc, {'op': 'remove_marker', 'args': {'marker': marker['id']}}


def update_marker(doc, mid, patch):
    doc = copy.deepcopy(doc)
    m = next((x for x in doc['markers'] if x['id'] == mid), None)
    if m is None:
        raise Err('MARKER_NOT_FOUND')
    if not isinstance(patch, dict) or not patch or any(k not in ('at', 'label') for k in patch):
        raise Err('PROPS_KEY')
    old = {k: m[k] for k in sorted(patch)}
    cand = dict(m)
    cand.update(patch)
    _marker_check(doc, cand, skip_id=mid)
    m.update(patch)
    _sort_markers(doc)
    return doc, {'op': 'update_marker', 'args': {'marker': mid, 'patch': old}}


def remove_marker(doc, mid):
    doc = copy.deepcopy(doc)
    i = next((k for k, x in enumerate(doc['markers']) if x['id'] == mid), None)
    if i is None:
        raise Err('MARKER_NOT_FOUND')
    m = doc['markers'].pop(i)
    return doc, {'op': 'add_marker', 'args': {'marker': m}}


CLIP_OPS = ('split_clip', 'trim_edge', 'delete_clip', 'set_speed', 'duplicate_clip', 'set_transition',
            'set_clip_props', 'move_clip')


def check_locked(doc, cmd):
    op, a = cmd['op'], cmd['args']
    tids = []
    if op in CLIP_OPS or (op in ('set_effect', 'remove_effect', 'move_effect') and a.get('owner') != 'master'):
        cid = a.get('clip') if op in CLIP_OPS else a.get('owner')
        loc = find_clip(doc, cid)
        if loc is not None:
            tids.append(doc['tracks'][loc[0]]['id'])
        if op == 'move_clip' and a.get('track'):
            tids.append(a['track'])
    elif op == 'add_clip':
        tids.append(a['track'])
    for tid in tids:
        ti = find_track(doc, tid)
        if ti is not None and doc['tracks'][ti].get('locked') is True:
            return 'TRACK_LOCKED'
    return None


OPS = {
    'add_clip': lambda d, a: add_clip(d, a['track'], a['clip'], a.get('ripple', False)),
    'delete_clip': lambda d, a: (delete_clip(d, a['clip'], a['ripple']), None),
    'duplicate_clip': lambda d, a: duplicate_clip(d, a['clip'], a['new_id']),
    'set_transition': lambda d, a: set_transition(d, a['clip'], a['transition']),
    'set_effect': lambda d, a: set_effect(d, a['owner'], a['effect'], a.get('index')),
    'remove_effect': lambda d, a: remove_effect(d, a['owner'], a['effect']),
    'move_effect': lambda d, a: move_effect(d, a['owner'], a['effect'], a['index']),
    'set_clip_props': lambda d, a: set_clip_props(d, a['clip'], a['patch']),
    'set_track_props': lambda d, a: set_track_props(d, a['track'], a['patch']),
    'add_track': lambda d, a: add_track(d, a['kind'], a['track'], a.get('index'), a.get('props')),
    'remove_track': lambda d, a: remove_track(d, a['track']),
    'add_marker': lambda d, a: add_marker(d, a['marker']),
    'update_marker': lambda d, a: update_marker(d, a['marker'], a['patch']),
    'remove_marker': lambda d, a: remove_marker(d, a['marker']),
}


def apply(doc, cmd):
    return OPS[cmd['op']](doc, cmd['args'])


def canon(d):
    return json.dumps(d, sort_keys=True, separators=(',', ':'), ensure_ascii=False)
