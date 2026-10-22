"""Эталон (одноразовый): компилятор плана рендера FN-R-08…R-20, документ 24. Только стандартная библиотека.
Чистые функции: документ + пресет + среда -> inputs / filter_complex. Генерация строк детерминирована."""
import hashlib
import json
import math
from decimal import Decimal, ROUND_HALF_UP
from fractions import Fraction

import ref_editor_core as K

TPS = K.TPS
SR = 48000
SAMPLES_PER_TICK = Fraction(SR, TPS)
MAX_LAYERS = 8
RENDER_MAX_INPUTS = 128
KB_SUPERSAMPLE = 4
KB_MAX_SIDE = 8192
SPEED_MIN, SPEED_MAX = Fraction(1, 4), Fraction(4, 1)


class CompileError(Exception):
    pass


# ---------------- форматирование чисел (FN-R-08) ----------------
def fmt_seconds(ticks):
    """Секунды как точная десятичная дробь: до 9 знаков, округление половина вверх, без хвостовых нулей."""
    q = (Decimal(ticks) / Decimal(TPS)).quantize(Decimal('1.000000000'), rounding=ROUND_HALF_UP)
    s = format(q, 'f')
    if '.' in s:
        s = s.rstrip('0').rstrip('.')
    return s or '0'


def fmt_num(x, digits=6):
    """Число для выражения фильтра: до `digits` знаков, половина вверх, без экспоненты и хвостовых нулей."""
    d = Decimal(repr(x)) if isinstance(x, float) else Decimal(x)
    q = d.quantize(Decimal(1).scaleb(-digits), rounding=ROUND_HALF_UP)
    s = format(q, 'f')
    if '.' in s:
        s = s.rstrip('0').rstrip('.')
    if s in ('-0', ''):
        s = '0'
    return s


def fps_expr(fps):
    return str(fps['num']) if fps['den'] == 1 else f"{fps['num']}/{fps['den']}"


def samples_at(ticks):
    """Позиция в отсчётах 48 кГц: ticks*48000/TPS, половина вверх (целое при кадрах 24/25/50/60 и 24000/1001)."""
    return int((Fraction(ticks) * SAMPLES_PER_TICK + Fraction(1, 2)).__floor__())


def atempo_chain(num, den):
    """Список множителей atempo, каждый в [0.5; 2]; пусто для 1/1."""
    t = Fraction(num, den)
    out = []
    while t > 2:
        out.append(Fraction(2))
        t /= 2
    while t < Fraction(1, 2):
        out.append(Fraction(1, 2))
        t *= 2
    if t != 1:
        out.append(t)
    return [fmt_num(float(x), 9) for x in out]


# ---------------- ducking (FN-R-12) ----------------
def duck_params(threshold_db, depth_db, attack_ms, release_ms):
    """sidechaincompress: threshold = 10^(T/20); ratio выбирается так, чтобы при уровне боковой цепи на 12 дБ выше порога
    ослабление равнялось depth_db: (1 - 1/ratio) * 12 = depth. depth >= 11.4 -> ratio 20 (предел фильтра)."""
    thr = 10 ** (threshold_db / 20)
    thr = min(1.0, max(0.000976563, thr))
    if depth_db <= 0:
        ratio = 1.0
    elif depth_db >= 12 * 0.95:
        ratio = 20.0
    else:
        ratio = 1.0 / (1.0 - depth_db / 12.0)
    return {'threshold': fmt_num(thr, 9), 'ratio': fmt_num(ratio, 6),
            'attack': fmt_num(min(2000.0, max(0.01, attack_ms)), 3),
            'release': fmt_num(min(9000.0, max(0.01, release_ms)), 3)}


# ---------------- Кен Бёрнс (FN-R-10) ----------------
def kb_supersample(w, h):
    s = KB_SUPERSAMPLE
    while s > 1 and max(w, h) * s > KB_MAX_SIDE:
        s -= 1
    return s


def kb_expr(c, head_frames, n_frames):
    """Выражения zoompan z, x, y для фото с движением (d=1, `on` — номер кадра с начала потока)."""
    m = c['motion']
    u = f"min(max((on-{head_frames})/{n_frames},0),1)"
    e = u if m['easing'] == 'linear' else f"({u}*{u}*(3-2*{u}))"
    f, t = m['from'], m['to']

    def lerp(a, b):
        return f"({fmt_num(a)}+({fmt_num(b)}-({fmt_num(a)}))*{e})"
    z = lerp(f['scale'], t['scale'])
    cx = lerp(f['center'][0], t['center'][0])
    cy = lerp(f['center'][1], t['center'][1])
    x = f"max(0,min(iw-iw/zoom,{cx}*iw-iw/zoom/2))"
    y = f"max(0,min(ih-ih/zoom,{cy}*ih-ih/zoom/2))"
    return z, x, y


# ---------------- LUT-имена ----------------
def color_effects(effects):
    return [e for e in effects if e.get('enabled', True) and e['type'].startswith('color.')]


def lut_name(effects):
    ce = color_effects(effects)
    if not ce:
        return None
    h = hashlib.sha256(K.canon(ce).encode('utf-8')).hexdigest()[:16]
    return f'lut_{h}.cube'


# ---------------- планирование ----------------
def plan_items(doc, src_dur, srcs):
    """Список использований клипов в порядке слоёв: [{ti, ci, track, clip, s, e, ss, t, need_v, need_a, ...}]."""
    items = []
    for ti, tr in enumerate(doc['tracks']):
        for ci, c in enumerate(tr['clips']):
            kind = c['source']['kind']
            s, e = K.voice_span(doc, ti, ci, src_dur)
            sid = c['source'].get('media_id') or c['source'].get('asset_version_id')
            info = srcs.get(sid)
            if info is None:
                raise CompileError(f'COMPILE_SOURCE_MISSING:{c["id"]}')
            need_v = kind in ('video', 'photo') and tr['kind'] == 'video' and tr.get('visible', True) is not False
            a = c.get('audio') or {}
            muted = bool(a.get('mute', False)) or (tr['kind'] == 'audio' and bool(tr.get('muted', False)))
            need_a = kind in ('video', 'audio') and info.get('has_audio', False) and not muted
            if kind == 'video' and not info.get('has_video', True):
                need_v = False
            if need_v and info.get('hdr') not in (None, 'pq', 'hlg'):
                raise CompileError(f'COMPILE_COLOR_UNSUPPORTED:{c["id"]}')
            if not need_v and not need_a:
                continue
            items.append({'ti': ti, 'ci': ci, 'track': tr, 'clip': c, 's': s, 'e': e, 'info': info,
                          'need_v': need_v, 'need_a': need_a, 'sid': sid})
    return items


def input_options(it, fps):
    c = it['clip']
    kind = c['source']['kind']
    opts = []
    if kind == 'photo':
        opts += ['-loop', '1', '-framerate', fps_expr(fps), '-t', fmt_seconds(it['e'] - it['s'])]
        return opts
    ss = max(0, K.src_ticks_at(c, it['s']))
    se = K.src_ticks_at(c, it['e'])
    if not it['need_v']:
        opts += ['-vn']
    if not it['need_a']:
        opts += ['-an']
    opts += ['-ss', fmt_seconds(ss), '-t', fmt_seconds(se - ss)]
    return opts


HDR_TONEMAP = ('zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,'
              'zscale=t=bt709:m=bt709:r=tv,format=yuv420p')
MATRIX = {'bt709': 'bt709', 'bt601': 'bt601', 'smpte170m': 'bt601', 'bt470bg': 'bt601'}


def lut_apply(name, fmt):
    """3D-LUT в RGB с явными преобразованиями Rec.709 ограниченного диапазона (альфа сохраняется)."""
    return (f"scale=in_color_matrix=bt709:in_range=tv,format=rgba,lut3d=file='{name}':interp=tetrahedral,"
            f"scale=out_color_matrix=bt709:out_range=tv,format={fmt}")


def fade_black(direction, st, d):
    """Яркость к чёрному (16,128,128): fade умножает Y и (U-128, V-128), поэтому на время fade диапазон растягивается до полного."""
    return (f"scale=in_range=tv:out_range=pc,fade=t={direction}:st={fmt_seconds(st)}:d={fmt_seconds(d)},"
            f"scale=in_range=pc:out_range=tv")


def video_chain(doc, it, idx, W, H, fps, F, src_dur):
    c, tr, info = it['clip'], it['track'], it['info']
    kind = c['source']['kind']
    ti, ci = it['ti'], it['ci']
    s, e = it['s'], it['e']
    s0 = s
    s = -(-s // F) * F          # слой начинается на сетке кадров монтажа (округление вверх)
    parts = []
    # 1. время
    if kind == 'video':
        sp = c.get('speed') or {'num': 1, 'den': 1}
        if (sp['num'], sp['den']) != (1, 1):
            parts.append(f"setpts=(PTS-STARTPTS)*{sp['den']}/{sp['num']}")
        else:
            parts.append('setpts=PTS-STARTPTS')
    else:
        parts.append('setpts=PTS-STARTPTS')
    parts.append(f'fps={fps_expr(fps)}')
    # 2. подгонка и цвет
    m = MATRIX.get(info.get('matrix', 'bt709'))
    if info.get('hdr') in ('pq', 'hlg') and kind == 'video':
        parts.append(HDR_TONEMAP)   # результат уже Rec.709 ограниченного диапазона
        m = 'bt709'
    if m is None:
        raise CompileError(f'COMPILE_COLOR_UNSUPPORTED:{c["id"]}')
    cm = f'in_color_matrix={m}:out_color_matrix=bt709:out_range=tv'
    motion = c.get('motion') if kind == 'photo' else None
    S = kb_supersample(W, H) if motion else 1
    CW, CH = W * S, H * S
    fit = c.get('fit', 'fit')
    if fit == 'fit':
        parts.append(f'scale={CW}:{CH}:force_original_aspect_ratio=decrease:force_divisible_by=2:{cm}')
    elif fit == 'fill':
        parts.append(f'scale={CW}:{CH}:force_original_aspect_ratio=increase:force_divisible_by=2:{cm}')
        parts.append(f'crop={CW}:{CH}')
    else:
        if S > 1:
            parts.append(f'scale=w=trunc(iw*{S}/2)*2:h=trunc(ih*{S}/2)*2:{cm}')
        else:
            parts.append(f'scale={cm}')
        parts.append(f"crop=w='min(iw,{CW})':h='min(ih,{CH})'")
    parts.append('format=' + ('rgba' if motion else 'yuva420p'))
    parts.append(f'pad={CW}:{CH}:(ow-iw)/2:(oh-ih)/2:color=black@0')
    if motion:
        head = (c['at'] - s) // F
        n = K.dur(c) // F
        z, x, y = kb_expr(c, head, n)
        parts.append(f"zoompan=z='{z}':x='{x}':y='{y}':d=1:s={W}x{H}:fps={fps_expr(fps)}")
        parts.append('scale=out_color_matrix=bt709:out_range=tv')
        parts.append('format=yuva420p')
    # 3. цвет клипа (LUT)
    ln = lut_name(c.get('effects', []))
    if ln:
        parts.append(lut_apply(ln, 'yuva420p'))
    # 4. преобразование (обрезка, масштаб, поворот)
    tf = c.get('transform')
    px = py = 0.0
    if tf:
        cr = tf['crop']
        if any(v != 0 for v in cr):
            cl, ct, crr, cb = cr
            parts.append(f"crop=w=iw*(1-{fmt_num(cl)}-{fmt_num(crr)}):h=ih*(1-{fmt_num(ct)}-{fmt_num(cb)}):"
                         f"x=iw*{fmt_num(cl)}:y=ih*{fmt_num(ct)}")
            parts.append(f"pad={W}:{H}:{W}*{fmt_num(cl)}:{H}*{fmt_num(ct)}:color=black@0")
        if tf['scale'] != 1:
            sc = fmt_num(tf['scale'])
            parts.append(f"scale=w='trunc(iw*{sc}/2)*2':h='trunc(ih*{sc}/2)*2'")
        if tf['rotate'] != 0:
            a = f"{fmt_num(tf['rotate'])}*PI/180"
            parts.append(f"rotate=a={a}:ow='rotw({a})':oh='roth({a})':c=none")
        px, py = tf['position']
    # 5. переходы (яркость и альфа)
    et = K.effective_transition(doc, ti, ci, src_dur)
    if et and et['kind'] == 'fade_in':
        parts.append(f"fade=t=in:st=0:d={fmt_seconds(et['duration'])}:alpha=1")
    elif et and et['kind'] == 'crossfade':
        parts.append(f"fade=t=in:st=0:d={fmt_seconds(et['duration'])}:alpha=1")
    elif et and et['kind'] == 'dip_to_black':
        half = et['duration'] // 2
        parts.append(fade_black('in', 0, half))
    if ci + 1 < len(tr['clips']):
        en = K.effective_transition(doc, ti, ci + 1, src_dur)
        if en and en['kind'] == 'dip_to_black':
            half = en['duration'] // 2
            parts.append(fade_black('out', max(0, en['start'] - s), half))
    # 6. позиция во времени
    parts.append(f"setpts=PTS-STARTPTS+{fmt_seconds(s)}/TB")
    label = f'[vc{idx}]'
    chain = f'[{idx}:v]' + ','.join(parts) + label
    ov = (f"x='(W-w)/2+({fmt_num(px)})*W':y='(H-h)/2+({fmt_num(py)})*H':eof_action=pass:"
          f"enable='between(t,{fmt_seconds(s)},{fmt_seconds(e)})':format=auto")
    return chain, label, ov


def voice_chain(doc, it, idx, src_dur):
    c, tr = it['clip'], it['track']
    ti, ci = it['ti'], it['ci']
    s, e = it['s'], it['e']
    parts = ['asetpts=PTS-STARTPTS', f'aresample={SR}']
    if it['info'].get('channels', 2) == 1:
        parts.append('pan=stereo|c0=c0|c1=c0')
    parts.append('aformat=sample_fmts=fltp:channel_layouts=stereo')
    if c['source']['kind'] == 'video' or c['source']['kind'] == 'audio':
        sp = c.get('speed') or {'num': 1, 'den': 1}
        for f in atempo_chain(sp['num'], sp['den']):
            parts.append(f'atempo={f}')
    a = c.get('audio') or {}
    gain = float(a.get('gain_db', 0.0)) + (float(tr.get('gain_db', 0.0)) if tr['kind'] == 'audio' else 0.0)
    if gain != 0:
        parts.append(f'volume={fmt_num(gain, 4)}dB:precision=float')
    fi, fo = a.get('fade_in', 0), a.get('fade_out', 0)
    if fi > 0:
        parts.append(f"afade=t=in:st={fmt_seconds(c['at'] - s)}:d={fmt_seconds(fi)}:curve=tri")
    if fo > 0:
        parts.append(f"afade=t=out:st={fmt_seconds(K.end_of(c) - fo - s)}:d={fmt_seconds(fo)}:curve=tri")
    if tr['kind'] == 'video':
        et = K.effective_transition(doc, ti, ci, src_dur)
        if et and et['kind'] == 'crossfade':
            parts.append(f"afade=t=in:st=0:d={fmt_seconds(et['duration'])}:curve=tri")
        elif et and et['kind'] == 'dip_to_black':
            parts.append(f"afade=t=in:st=0:d={fmt_seconds(et['duration'] // 2)}:curve=tri")
        if ci + 1 < len(tr['clips']):
            en = K.effective_transition(doc, ti, ci + 1, src_dur)
            if en and en['kind'] == 'crossfade':
                parts.append(f"afade=t=out:st={fmt_seconds(en['start'] - s)}:d={fmt_seconds(en['duration'])}:curve=tri")
            elif en and en['kind'] == 'dip_to_black':
                half = en['duration'] // 2
                parts.append(f"afade=t=out:st={fmt_seconds(en['start'] - s)}:d={fmt_seconds(half)}:curve=tri")
    parts.append(f'adelay={samples_at(s)}S:all=1')
    return f'[{idx}:a]' + ','.join(parts)


def compile_graph(doc, preset, runtime, mode='full'):
    """-> {inputs[{index, options, key}], filter_complex, maps, decisions, luts, duration_ticks}."""
    srcs = runtime['sources']
    src_dur = {k: v['duration'] for k, v in srcs.items() if 'duration' in v}
    F = K.ft(doc)
    W, H = preset['width'], preset['height']
    fps = preset.get('fps') or doc['video']['fps']
    if (fps['num'], fps['den']) != (doc['video']['fps']['num'], doc['video']['fps']['den']):
        raise CompileError('COMPILE_FPS_UNSUPPORTED')
    D = K.doc_end(doc)
    # лимит слоёв
    pts = sorted({0} | {x for tr in doc['tracks'] for c in tr['clips'] for x in (c['at'], K.end_of(c))}
                 | {x for ti, tr in enumerate(doc['tracks']) for ci in range(len(tr['clips']))
                    for et in [K.effective_transition(doc, ti, ci, src_dur)] if et for x in (et['start'], et['end'])})
    for p in pts:
        if p < D and K.compose_at(doc, p, src_dur)['over_limit']:
            raise CompileError('COMPILE_TOO_MANY_LAYERS')
    items = plan_items(doc, src_dur, srcs)
    if mode == 'audio':
        items = [dict(it, need_v=False) for it in items if it['need_a']]
    if len(items) > RENDER_MAX_INPUTS:
        raise CompileError('COMPILE_TOO_MANY_INPUTS')
    decisions = []
    for ti, tr in enumerate(doc['tracks']):
        for ci, c in enumerate(tr['clips']):
            et = K.effective_transition(doc, ti, ci, src_dur)
            if et and et['shortened']:
                decisions.append({'code': 'TRANSITION_SHORTENED', 'clip': c['id'],
                                  'detail': f"requested={et['requested']} effective={et['duration']}"})
            sid = c['source'].get('media_id')
            if c['source']['kind'] == 'video' and srcs.get(sid, {}).get('hdr') in ('pq', 'hlg') and tr['kind'] == 'video':
                decisions.append({'code': 'HDR_TONEMAPPED', 'clip': c['id'], 'detail': srcs[sid]['hdr']})
            for ef in c.get('effects', []):
                if ef['type'] in ('audio.gain', 'audio.fade') and ef.get('enabled', True):
                    decisions.append({'code': 'AUDIO_EFFECT_NOT_RENDERED', 'clip': c['id'], 'detail': ef['type']})
    inputs, vlines, alines = [], [], []
    luts = {}
    ovs = []
    voices = []   # (idx, item)
    for idx, it in enumerate(items):
        inputs.append({'index': idx, 'clip': it['clip']['id'], 'options': input_options(it, fps)})
        if it['need_v']:
            ch, lb, ov = video_chain(doc, it, idx, W, H, fps, F, src_dur)
            vlines.append(ch)
            ovs.append((lb, ov))
            ln = lut_name(it['clip'].get('effects', []))
            if ln:
                luts[ln] = color_effects(it['clip']['effects'])
        if it['need_a']:
            voices.append((idx, it))
    # видео: холст и наложения
    vg = [] if mode == 'audio' else [f"color=c=black:s={W}x{H}:r={fps_expr(fps)}:d={fmt_seconds(D)},format=yuv420p,"
          f"setparams=colorspace=bt709:color_primaries=bt709:color_trc=bt709:range=tv[bg]"] if D > 0 else []
    vg = vlines + vg
    prev = '[bg]'
    for k, (lb, ov) in enumerate(ovs if mode != 'audio' else []):
        out = f'[ov{k}]'
        vg.append(f'{prev}{lb}overlay={ov}{out}')
        prev = out
    mlut = lut_name(doc.get('master', {}).get('effects', []))
    if mode == 'audio':
        pass
    elif mlut:
        luts[mlut] = color_effects(doc['master']['effects'])
        vg.append(f"{prev}{lut_apply(mlut, 'yuv420p')}[vout]")
    else:
        vg.append(f'{prev}format=yuv420p[vout]')
    if mode == 'audio':
        vg = []
    # звук: голоса, ducking, шина
    bus_users = {}
    for idx, it in voices:
        for ef in it['clip'].get('effects', []):
            if ef['type'] == 'audio.duck' and ef.get('enabled', True):
                for tid in ef['params']['sidechain_tracks']:
                    bus_users.setdefault(tid, 0)
                    bus_users[tid] += 1
    ag = []
    sil = 0

    def silence():
        nonlocal sil
        sil += 1
        lab = f'[sil{sil}]'
        ag.append(f"anullsrc=r={SR}:cl=stereo,atrim=duration={fmt_seconds(D)},asetpts=PTS-STARTPTS{lab}")
        return lab
    final_inputs = []
    bus_parts = {tid: [] for tid in bus_users}
    for idx, it in voices:
        base = voice_chain(doc, it, idx, src_dur)
        tid = it['track']['id']
        lab = f'[av{idx}]'
        if tid in bus_users:
            ag.append(base + f',asplit=2{lab}[bs{idx}]')
            bus_parts[tid].append(f'[bs{idx}]')
        else:
            ag.append(base + lab)
        final_inputs.append((idx, it, lab))
    buses = {}
    for tid in sorted(bus_parts):
        parts = bus_parts[tid]
        n = bus_users[tid]
        if not parts:
            continue
        s0 = silence()
        lab = f'[bus_{tid}]'
        ag.append(f"{s0}{''.join(parts)}amix=inputs={len(parts) + 1}:normalize=0:duration=first,"
                  f"asplit={n}{''.join(f'[{lab[1:-1]}_{k}]' for k in range(n))}")
        buses[tid] = [f'[{lab[1:-1]}_{k}]' for k in range(n)]
    mix_labels = []
    for idx, it, lab in final_inputs:
        cur = lab
        for ef in it['clip'].get('effects', []):
            if ef['type'] == 'audio.duck' and ef.get('enabled', True):
                p = ef['params']
                dp = duck_params(p['threshold_db'], p['depth_db'], p['attack_ms'], p['release_ms'])
                sc_parts = []
                for tid in p['sidechain_tracks']:
                    if tid in buses and buses[tid]:
                        sc_parts.append(buses[tid].pop(0))
                if not sc_parts:
                    continue
                if len(sc_parts) > 1:
                    s0 = silence()
                    ag.append(f"{s0}{''.join(sc_parts)}amix=inputs={len(sc_parts) + 1}:normalize=0:duration=first[scm{idx}]")
                    sc = f'[scm{idx}]'
                else:
                    sc = sc_parts[0]
                out = f'[du{idx}_{len(mix_labels)}]'
                ag.append(f"{cur}{sc}sidechaincompress=threshold={dp['threshold']}:ratio={dp['ratio']}:"
                          f"attack={dp['attack']}:release={dp['release']}:makeup=1:level_sc=1{out}")
                cur = out
        mix_labels.append(cur)
    s0 = silence()
    ag.append(f"{s0}{''.join(mix_labels)}amix=inputs={len(mix_labels) + 1}:normalize=0:duration=first[abus]")
    fc = ';\n'.join(vg + ag)
    return {'inputs': inputs, 'filter_complex': fc, 'maps': ['[abus]'] if mode == 'audio' else ['[vout]', '[abus]'],
            'decisions': decisions,
            'luts': sorted(luts), 'duration_ticks': D}
