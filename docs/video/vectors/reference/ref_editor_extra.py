"""Эталон (одноразовый): таймкод, копирование/вставка клипа, колесо и трекпад. Документ 23, §20 (дополнение).
Только стандартная библиотека. Целочисленная арифметика; округление «половина вверх» = floor(v + 0.5), как Math.round."""
import copy
import math
import re

import ref_editor_core as K
import ref_editor_ui as U

WHEEL_LINE_PX = 16
WHEEL_ZOOM_STEP_PX = 40
WHEEL_ACC_MAX = 80
WHEEL_RESET_MS = 200
TC_RE = re.compile(r'^[0-9]{1,6}(?:[:.;][0-9]{1,6}){0,3}$')


def rhu(v):
    return math.floor(v + 0.5)


# ---------------- FN-E-20 / FN-E-21 таймкод ----------------
def nominal_fps(fps):
    return (fps['num'] + fps['den'] - 1) // fps['den']


def format_timecode(ticks, doc_fps):
    doc = {'video': {'fps': doc_fps}}
    F = K.ft(doc)
    n = max(0, ticks) // F
    N = nominal_fps(doc_fps)
    ff = n % N
    s = n // N
    return '%02d:%02d:%02d:%02d' % (s // 3600, (s // 60) % 60, s % 60, ff)


def parse_timecode(text, doc_fps):
    doc = {'video': {'fps': doc_fps}}
    F = K.ft(doc)
    N = nominal_fps(doc_fps)
    t = text.strip()
    if not TC_RE.match(t):
        return None
    f = [int(x) for x in re.split(r'[:.;]', t)]
    if len(f) == 1:
        return f[0] * F
    ff, rest = f[-1], f[:-1][::-1]  # rest: секунды, минуты, часы (от младшего)
    if ff >= N:
        return None
    ss = rest[0]
    mm = rest[1] if len(rest) > 1 else 0
    hh = rest[2] if len(rest) > 2 else 0
    if len(rest) > 1 and ss >= 60:
        return None
    if len(rest) > 2 and mm >= 60:
        return None
    if hh > 99:
        return None
    return ((hh * 3600 + mm * 60 + ss) * N + ff) * F


# ---------------- FN-E-18 копирование / вставка ----------------
def copy_clip(doc, selection):
    loc = K.find_clip(doc, selection) if selection else None
    if loc is None:
        return None
    return {'track': doc['tracks'][loc[0]]['id'], 'clip': copy.deepcopy(doc['tracks'][loc[0]]['clips'][loc[1]])}


def paste_clip(doc, snap, playhead, new_id):
    if snap is None:
        return {'error': 'PASTE_EMPTY'}
    kind = 'audio' if snap['clip']['source']['kind'] == 'audio' else 'video'
    pick = None
    for t in doc['tracks']:
        if t['id'] == snap['track'] and t['kind'] == kind and t.get('locked') is not True:
            pick = t['id']
    if pick is None:
        for t in doc['tracks']:
            if t['kind'] == kind and t.get('locked') is not True:
                pick = t['id']
                break
    if pick is None:
        return {'error': 'PASTE_NO_TRACK'}
    F = K.ft(doc)
    b = copy.deepcopy(snap['clip'])
    b['id'] = new_id
    b['at'] = max(0, playhead) // F * F
    b.pop('transition_in', None)
    for k, e in enumerate(b.get('effects', []), 1):
        e['id'] = f'{new_id}-e{k}'
    return {'track': pick, 'clip': b}


# ---------------- FN-E-19 колесо и трекпад ----------------
def _px(d, mode, width):
    return rhu(d * (1, WHEEL_LINE_PX, width)[mode])


def wheel_step(state, ev, view, end, busy):
    if busy:
        return state, {'consume': False, 'view': view}
    dx = _px(ev['dx'], ev['mode'], view['width'])
    dy = _px(ev['dy'], ev['mode'], view['width'])
    acc = state['acc'] if ev['t'] - state['t'] <= WHEEL_RESET_MS else 0
    if ev['ctrl'] or ev['meta']:
        acc = max(-WHEEL_ACC_MAX, min(WHEEL_ACC_MAX, acc + dy))
        direction = 0
        if acc <= -WHEEL_ZOOM_STEP_PX:
            direction, acc = 1, acc + WHEEL_ZOOM_STEP_PX
        elif acc >= WHEEL_ZOOM_STEP_PX:
            direction, acc = -1, acc - WHEEL_ZOOM_STEP_PX
        nv = U.zoom_at(view, ev['x'], direction) if direction else view
        return {'acc': acc, 't': ev['t']}, {'consume': True, 'view': nv}
    if ev['shift']:
        d = dx if abs(dx) > abs(dy) else dy
    elif abs(dx) > abs(dy):
        d = dx
    else:
        return {'acc': 0, 't': ev['t']}, {'consume': False, 'view': view}
    span = view['width'] * view['tpp']
    smax = max(0, end - span * 3 // 4)
    ns = max(0, min(smax, view['scroll'] + d * view['tpp']))
    return {'acc': 0, 't': ev['t']}, {'consume': True, 'view': dict(view, scroll=ns)}


# ---------------- FN-E-22 оформление клипа на canvas ----------------
KIND_COLORS = {'video': ('primaryContainer', 'onPrimaryContainer'),
               'photo': ('secondaryContainer', 'onSecondaryContainer'),
               'audio': ('tertiaryContainer', 'onTertiaryContainer')}


def clip_style(kind, fl):
    fill, text = KIND_COLORS[kind]
    out = {'fill': fill, 'text': text, 'stroke': 'outline', 'stroke_w': 1, 'dash': [], 'alpha': 1.0, 'hatch': False}
    if fl.get('missing'):
        out.update(fill='errorContainer', text='onErrorContainer', stroke='error', dash=[4, 3])
    alphas = [1.0]
    if fl.get('locked'):
        alphas.append(0.5)
        out['hatch'] = True
    if fl.get('hidden') and kind != 'audio':
        alphas.append(0.4)
    if fl.get('muted') and kind == 'audio':
        alphas.append(0.5)
    out['alpha'] = min(alphas)
    if fl.get('hover') and not fl.get('selected'):
        out.update(stroke='primary', stroke_w=1)
    if fl.get('selected'):
        out.update(stroke='primary', stroke_w=2)
    pv = fl.get('preview')
    if pv is not None:
        out = {'fill': fill if pv['valid'] else 'error', 'text': text, 'stroke': None, 'stroke_w': 0, 'dash': [],
               'alpha': 0.6 if pv['valid'] else 0.5, 'hatch': False}
    return out
