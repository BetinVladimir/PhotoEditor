"""Генератор vectors/compile.json (документ 24). Только стандартная библиотека. Запуск: python3 ref_compile_vectors.py > ../compile.json
Секции: fmt_seconds, fmt_num, samples_at, atempo_chain, duck_params, kb_supersample, kb_expr, lut_name, errors, graphs."""
import json, sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), '..'))
import ref_compile as C
import ref_editor_core as K

S = lambda x: round(x * K.TPS)
FPS25 = {'num': 25, 'den': 1}


def clip(cid, kind, sid, at, src_in=0, src_out=0, **kw):
    if kind == 'photo':
        c = {'id': cid, 'source': {'kind': 'photo', 'asset_version_id': sid, 'sha256': 'h' + cid}, 'at': at,
             'duration': kw.pop('duration'), 'fit': 'fit', 'effects': []}
    else:
        c = {'id': cid, 'source': {'kind': kind, 'media_id': sid, 'sha256': 'h' + cid}, 'at': at, 'src_in': src_in,
             'src_out': src_out, 'speed': {'num': 1, 'den': 1}, 'fit': 'fit',
             'audio': {'gain_db': 0.0, 'fade_in': 0, 'fade_out': 0, 'mute': False}, 'effects': []}
    c.update(kw)
    return c


def mkdoc(tracks, master=None, fps=FPS25):
    return {'video': {'fps': fps}, 'tracks': tracks, 'markers': [], 'master': master or {'effects': [], 'audio': {}}}


def vt(tid, clips, **kw):
    d = {'id': tid, 'kind': 'video', 'clips': clips}
    d.update(kw)
    return d


def at_(tid, clips, **kw):
    d = {'id': tid, 'kind': 'audio', 'clips': clips}
    d.update(kw)
    return d


SRC = {'red': {'has_audio': True, 'channels': 1, 'duration': S(12)},
       'green': {'has_audio': True, 'channels': 1, 'duration': S(12)},
       'blue43': {'has_audio': True, 'channels': 1, 'duration': S(12)},
       'orange': {'has_audio': False}, 'grad': {'has_audio': False},
       'hdrpq': {'has_audio': False, 'duration': S(3), 'hdr': 'pq'}, 'hdrhlg': {'has_audio': False, 'duration': S(3), 'hdr': 'hlg'}}
LUT_EFF = [{'id': 'e1', 'type': 'color.lut3d', 'enabled': True,
            'params': {'lut': {'kind': 'lut', 'media_id': 'L', 'sha256': 'x'}, 'intensity': 1.0}}]
DUCK = {'id': 'dk', 'type': 'audio.duck', 'enabled': True,
        'params': {'sidechain_tracks': ['A2'], 'threshold_db': -30, 'depth_db': 12, 'attack_ms': 20, 'release_ms': 250}}
F2997 = {'num': 30000, 'den': 1001}
FT = K.ft({'video': {'fps': F2997}})
KB = {'kind': 'pan_zoom', 'from': {'scale': 1.0, 'center': [0.5, 0.5]}, 'to': {'scale': 2.0, 'center': [0.75, 0.5]}, 'easing': 'ease_in_out'}
TR = lambda s, r, p, c: {'scale': s, 'position': p, 'rotate': r, 'crop': c}
XF = {'kind': 'crossfade', 'duration': S(1), 'align': 'center'}


def goldens():
    g = {}
    g['G1'] = (mkdoc([vt('V1', [clip('c1', 'video', 'red', 0, S(2), S(5), audio={'gain_db': -6.0, 'fade_in': S(0.5), 'fade_out': 0, 'mute': False}),
                                clip('c2', 'video', 'green', S(3), S(1), S(3), audio={'gain_db': -6.0, 'fade_in': 0, 'fade_out': 0, 'mute': False})]), at_('A1', [])]), {})
    g['G2'] = (mkdoc([vt('V1', [clip('c1', 'video', 'red', 0, 0, S(3)), clip('c2', 'video', 'green', S(3), S(5), S(8), transition_in=XF)]), at_('A1', [])]), {})
    g['G3'] = (mkdoc([vt('V1', [clip('c1', 'video', 'red', 0, S(1), S(4)),
                                clip('c2', 'video', 'green', S(3), S(2), S(5), transition_in={'kind': 'dip_to_black', 'duration': S(1), 'align': 'center'}),
                                clip('c3', 'video', 'red', S(7), S(1), S(3), transition_in={'kind': 'fade_in', 'duration': S(1), 'align': 'center'})]), at_('A1', [])]), {})
    g['G4a'] = (mkdoc([vt('V1', [clip('c1', 'video', 'green', 0, 0, S(3))]), vt('V2', [clip('c2', 'video', 'blue43', 0, 0, S(3))]), at_('A1', [])]), {})
    g['G4b'] = (mkdoc([vt('V1', [clip('c1', 'video', 'green', 0, 0, S(3))]),
                       vt('V2', [clip('p1', 'photo', 'orange', 0, duration=S(3), transform=TR(0.5, 0.0, [0.25, -0.25], [0, 0, 0, 0]))]), at_('A1', [])]), {})
    g['G4c'] = (mkdoc([vt('V1', [clip('c1', 'video', 'green', 0, 0, S(3))]),
                       vt('V2', [clip('c2', 'video', 'blue43', 0, 0, S(3), transform=TR(1.0, 90.0, [0, 0], [0, 0, 0, 0]))]), at_('A1', [])]), {})
    g['G4d'] = (mkdoc([vt('V1', [clip('c1', 'video', 'green', 0, 0, S(3))]),
                       vt('V2', [clip('c2', 'video', 'blue43', 0, 0, S(3), transform=TR(1.0, 0.0, [0, 0], [0.25, 0, 0.25, 0]))]), at_('A1', [])]), {})
    g['G4e'] = (mkdoc([vt('V1', [clip('c1', 'video', 'green', 0, 0, S(3))]), vt('V2', [clip('c2', 'video', 'blue43', 0, 0, S(3), fit='fill')]), at_('A1', [])]), {})
    g['G4f'] = (mkdoc([vt('V1', [clip('c1', 'video', 'green', 0, 0, S(3))]), vt('V2', [clip('c2', 'video', 'blue43', 0, 0, S(3), fit='none')]), at_('A1', [])]), {})
    g['G4g'] = (mkdoc([vt('V1', [clip('c1', 'video', 'green', 0, 0, S(4))]),
                       vt('V2', [clip('r1', 'video', 'red', 0, 0, S(2)), clip('b1', 'video', 'blue43', S(2), 0, S(2), transition_in=XF)]), at_('A1', [])]), {})
    g['G5a'] = (mkdoc([vt('V1', [clip('p1', 'photo', 'grad', 0, duration=S(2), motion=KB)]), at_('A1', [])]), {})
    g['G5b'] = (mkdoc([vt('V1', [clip('p1', 'photo', 'orange', 0, duration=S(3)), clip('c1', 'video', 'green', S(3), S(2), S(5), transition_in=XF)]), at_('A1', [])]), {})
    g['G5c'] = (mkdoc([vt('V1', [clip('c1', 'video', 'green', 0, 0, S(4))]),
                       vt('V2', [clip('p1', 'photo', 'orange', S(1), duration=S(2), transition_in={'kind': 'fade_in', 'duration': S(1), 'align': 'center'})]), at_('A1', [])]), {})
    g['G6'] = (mkdoc([vt('V1', [clip('c1', 'video', 'red', 0, 0, S(4), speed={'num': 2, 'den': 1}),
                                clip('c2', 'video', 'green', S(2), 0, S(1), speed={'num': 1, 'den': 4})]), at_('A1', [])]), {})
    g['G7a'] = (mkdoc([vt('V1', [clip('c1', 'video', 'red', 0, 0, S(2), effects=LUT_EFF), clip('c2', 'video', 'red', S(2), 0, S(2))]), at_('A1', [])]), {})
    g['G7b'] = (mkdoc([vt('V1', [clip('c1', 'video', 'green', 0, 0, S(2))]), at_('A1', [])], master={'effects': LUT_EFF, 'audio': {}}), {})
    g['G7d'] = (mkdoc([vt('V1', [clip('c1', 'video', 'green', 0, 0, S(2))]), vt('V2', [clip('c2', 'video', 'blue43', 0, 0, S(2), effects=LUT_EFF)]), at_('A1', [])]), {})
    g['G8'] = (mkdoc([vt('V1', [clip('v1', 'video', 'red', 0, 0, S(8))]),
                      at_('A1', [clip('mus', 'audio', 'green', 0, 0, S(8), effects=[DUCK])], gain_db=-6.0),
                      at_('A2', [clip('sp', 'audio', 'blue43', S(2), 0, S(3))])]), {})
    g['G9'] = (mkdoc([vt('V1', [clip('v1', 'video', 'red', 0, 0, S(3))], visible=False),
                      at_('A1', [clip('m', 'audio', 'green', 0, 0, S(3))], muted=True),
                      at_('A2', [clip('n', 'audio', 'blue43', 0, 0, S(3), audio={'gain_db': -12.0, 'fade_in': S(1), 'fade_out': S(1), 'mute': False})])]), {})
    g['G10'] = (mkdoc([vt('V1', [clip('c1', 'video', 'red', 0, 0, 90 * FT),
                                 clip('c2', 'video', 'green', 90 * FT, 30 * FT, 120 * FT, transition_in={'kind': 'crossfade', 'duration': 30 * FT, 'align': 'center'}),
                                 clip('c3', 'video', 'red', 180 * FT, 0, 90 * FT)]), at_('A1', [])], fps=F2997), {})
    g['G11'] = (mkdoc([vt('V1', [clip('c1', 'video', 'red', 0, 0, S(2)), clip('c2', 'video', 'blue43', S(2), 0, S(2))]), at_('A1', [])]), {'width': 1280, 'height': 720})
    g['G15'] = (mkdoc([vt('V1', [clip('c1', 'video', 'hdrpq', 0, 0, S(2)), clip('c2', 'video', 'hdrhlg', S(2), 0, S(1))]), at_('A1', [])]), {})
    g['G13'] = (mkdoc([vt('V1', [clip('p1', 'photo', 'orange', 0, duration=S(2))]), at_('A1', [])]), {})
    return g


def graph_case(name, doc, preset):
    preset = dict({'width': 320, 'height': 180}, **preset)
    plan = C.compile_graph(doc, preset, {'sources': SRC})
    return {'name': name, 'doc': doc, 'preset': preset, 'expect': {
        'inputs': [{'index': i['index'], 'clip': i['clip'], 'options': i['options']} for i in plan['inputs']],
        'filter_complex': plan['filter_complex'], 'maps': plan['maps'], 'luts': plan['luts'],
        'duration_ticks': plan['duration_ticks'], 'decisions': plan['decisions']}}


def errors():
    out = []
    base = lambda **kw: mkdoc([vt('V1', [clip('c1', 'video', 'red', 0, 0, S(2), **kw)]), at_('A1', [])])
    # слишком много слоёв
    doc = mkdoc([vt(f'V{i}', [clip(f'c{i}', 'video', 'red', 0, 0, S(1))]) for i in range(1, 10)])
    out.append({'name': 'too_many_layers', 'doc': doc, 'sources': SRC, 'error': 'COMPILE_TOO_MANY_LAYERS'})
    out.append({'name': 'source_missing', 'doc': base(), 'sources': {}, 'error': 'COMPILE_SOURCE_MISSING:c1'})
    s = dict(SRC, red=dict(SRC['red'], hdr='dolby_vision'))
    out.append({'name': 'hdr_unknown', 'doc': base(), 'sources': s, 'error': 'COMPILE_COLOR_UNSUPPORTED:c1'})
    s = dict(SRC, red=dict(SRC['red'], matrix='bt2020nc'))
    out.append({'name': 'matrix', 'doc': base(), 'sources': s, 'error': 'COMPILE_COLOR_UNSUPPORTED:c1'})
    clips = [clip(f'c{i}', 'video', 'red', S(1) * i, 0, S(1)) for i in range(129)]
    out.append({'name': 'too_many_inputs', 'doc': mkdoc([vt('V1', clips)]), 'sources': SRC, 'error': 'COMPILE_TOO_MANY_INPUTS'})
    out.append({'name': 'fps_mismatch', 'doc': base(), 'sources': SRC, 'preset_fps': {'num': 30, 'den': 1}, 'error': 'COMPILE_FPS_UNSUPPORTED'})
    return out


def run_error(case):
    preset = {'width': 320, 'height': 180}
    if 'preset_fps' in case:
        preset['fps'] = case['preset_fps']
    try:
        C.compile_graph(case['doc'], preset, {'sources': case['sources']})
    except C.CompileError as e:
        return str(e)
    return None


def main():
    kbc = clip('p1', 'photo', 'grad', 0, duration=S(2), motion=KB)
    out = {'meta': {'ffmpeg': '6.1', 'tps': K.TPS, 'description': 'Векторы компилятора рендера (документ 24)'}}
    out['fmt_seconds'] = [{'ticks': t, 'out': C.fmt_seconds(t)} for t in
                          [0, 1, 705600000, 3 * 705600000, 705600000 * 5 // 2, 28224000, 14112000, 1411200000 + 7056000,
                           123456789, 705600000 * 3600, 235200000, 100000000, 7]]
    out['fmt_num'] = [{'x': x, 'digits': d, 'out': C.fmt_num(x, d)} for x, d in
                      [(0.5, 6), (1.0, 6), (-0.25, 6), (0.1234565, 6), (1 / 3, 6), (2.0, 9), (-0.0, 6), (1e-7, 6), (12345.678, 3), (0.0000004, 6)]]
    out['samples_at'] = [{'ticks': t, 'out': C.samples_at(t)} for t in [0, 705600000, 28224000, 3 * 705600000 + 14112000, 23520000 * 90, 1]]
    out['atempo_chain'] = [{'num': n, 'den': d, 'out': C.atempo_chain(n, d)} for n, d in
                           [(1, 1), (2, 1), (4, 1), (1, 4), (3, 2), (1, 3), (7, 3), (4, 3)]]
    out['duck_params'] = [{'in': list(a), 'out': C.duck_params(*a)} for a in
                          [(-30, 12, 20, 250), (-18, 6, 10, 100), (-40, 0, 5, 50), (-30, 9, 20, 250), (0, 20, 0, 20000), (-80, 3, 5000, 1)]]
    out['kb_supersample'] = [{'w': w, 'h': h, 'out': C.kb_supersample(w, h)} for w, h in [(320, 180), (1920, 1080), (3840, 2160), (7680, 4320)]]
    out['kb_expr'] = [{'clip': kbc, 'head': h, 'n': n, 'out': dict(zip(('z', 'x', 'y'), C.kb_expr(kbc, h, n)))} for h, n in [(0, 50), (3, 120)]]
    out['lut_name'] = [{'effects': LUT_EFF, 'out': C.lut_name(LUT_EFF)}, {'effects': [], 'out': None},
                       {'effects': [dict(LUT_EFF[0], enabled=False)], 'out': None}]
    out['errors'] = [dict(c, doc=c['doc'], got=run_error(c)) for c in errors()]
    for e in out['errors']:
        assert e['got'] == e['error'], (e['name'], e['got'])
        e.pop('got')
    out['graphs'] = [graph_case(n, d, p) for n, (d, p) in goldens().items()]
    json.dump(out, sys.stdout, ensure_ascii=False, indent=1, sort_keys=True)
    print()


if __name__ == '__main__':
    main()
