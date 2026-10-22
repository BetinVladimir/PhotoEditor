#!/usr/bin/env python3
"""Генератор vectors/editor.json (документ 23). Запуск из этого каталога: python3 -I ref_editor.py > ../editor.json
Только стандартная библиотека. Каждая успешная команда проверяется: применение обратной даёт исходный документ."""
import copy
import json
import sys

sys.path.insert(0, __import__('os').path.dirname(__file__))
import ref_editor_core as K  # noqa: E402
import ref_editor_ui as U  # noqa: E402

TPS = K.TPS
F = 28_224_000


def S(x):
    return round(x * TPS)


def base_doc():
    return {
        'video': {'fps': {'num': 25, 'den': 1}},
        'tracks': [
            {'id': 'V1', 'kind': 'video', 'clips': [
                {'id': 'c1', 'source': {'kind': 'video', 'media_id': 'm1', 'sha256': 'h1'}, 'at': 0, 'src_in': 0,
                 'src_out': S(4), 'speed': {'num': 1, 'den': 1}, 'fit': 'fit',
                 'audio': {'gain_db': 0.0, 'fade_in': 0, 'fade_out': 0, 'mute': False},
                 'effects': [{'id': 'e1', 'type': 'color.basic', 'enabled': True, 'params': {}}]},
                {'id': 'c2', 'source': {'kind': 'video', 'media_id': 'm2', 'sha256': 'h2'}, 'at': S(4),
                 'src_in': S(2), 'src_out': S(6), 'speed': {'num': 1, 'den': 1}, 'fit': 'fit',
                 'audio': {'gain_db': 0.0, 'fade_in': 0, 'fade_out': 0, 'mute': False},
                 'transition_in': {'kind': 'crossfade', 'duration': S(1), 'align': 'center'}, 'effects': []},
                {'id': 'c3', 'source': {'kind': 'photo', 'asset_version_id': 'p9', 'sha256': 'h9'}, 'at': S(8),
                 'duration': S(3), 'fit': 'fit',
                 'motion': {'kind': 'pan_zoom', 'from': {'scale': 1.0, 'center': [0.5, 0.5]},
                            'to': {'scale': 1.2, 'center': [0.6, 0.5]}, 'easing': 'ease_in_out'},
                 'effects': []}]},
            {'id': 'V2', 'kind': 'video', 'clips': [
                {'id': 'p1', 'source': {'kind': 'photo', 'asset_version_id': 'p8', 'sha256': 'h8'}, 'at': S(2),
                 'duration': S(2), 'fit': 'fit',
                 'transform': {'scale': 0.5, 'position': [0.25, -0.25], 'rotate': 0.0, 'crop': [0, 0, 0, 0]},
                 'effects': []}]},
            {'id': 'A1', 'kind': 'audio', 'gain_db': -3.0, 'clips': [
                {'id': 'a1', 'source': {'kind': 'audio', 'media_id': 'm3', 'sha256': 'h3'}, 'at': 0, 'src_in': 0,
                 'src_out': S(10), 'speed': {'num': 1, 'den': 1},
                 'audio': {'gain_db': -6.0, 'fade_in': S(1), 'fade_out': S(2), 'mute': False}, 'effects': []}]},
            {'id': 'A2', 'kind': 'audio', 'clips': []}],
        'markers': [{'id': 'mk1', 'at': S(3), 'kind': 'user', 'label': 'A'}],
        'master': {'effects': [], 'audio': {'loudness': {'target_lufs': -14.0, 'true_peak_db': -1.0}}},
    }


SRC = {'m1': S(10), 'm2': S(10), 'm3': S(30)}
B = base_doc()


def variant(fn):
    d = base_doc()
    fn(d)
    return d


def tr(d, tid):
    return d['tracks'][K.find_track(d, tid)]


def cl(d, cid):
    ti, ci = K.find_clip(d, cid)
    return d['tracks'][ti]['clips'][ci]


OUT = {'_about': 'Сгенерировано ref_editor.py; не редактировать вручную. Правила — документ 23. '
                 'Дробные значения сравниваются с допуском 1e-9; целые и строки — точно.',
       'frame_ticks': F, 'base': B, 'source_duration_ticks': SRC}

# ---------- effective_transition ----------
V_SHORT = variant(lambda d: cl(d, 'c2').update(src_in=S(0.2), src_out=S(4.2)))
V_START = variant(lambda d: cl(d, 'c2')['transition_in'].update(align='start'))
V_END = variant(lambda d: cl(d, 'c2')['transition_in'].update(align='end'))
V_DIP = variant(lambda d: cl(d, 'c2').update(transition_in={'kind': 'dip_to_black', 'duration': S(1), 'align': 'center'}))
V_NOHEAD = variant(lambda d: cl(d, 'c2').update(src_in=0, src_out=S(4)))
V_GAP = variant(lambda d: cl(d, 'c2').update(at=S(5)))
V_FADEIN = variant(lambda d: cl(d, 'c1').update(transition_in={'kind': 'fade_in', 'duration': S(1), 'align': 'center'}))
VARS = {'base': B, 'short': V_SHORT, 'start': V_START, 'end': V_END, 'dip': V_DIP, 'nohead': V_NOHEAD, 'gap': V_GAP,
        'fadein': V_FADEIN}
OUT['docs'] = {k: v for k, v in VARS.items() if k != 'base'}
et = []
for name, (vk, cid) in {'ET-01': ('base', 'c2'), 'ET-02': ('short', 'c2'), 'ET-03': ('start', 'c2'),
                        'ET-04': ('end', 'c2'), 'ET-05': ('dip', 'c2'), 'ET-06': ('nohead', 'c2'),
                        'ET-07': ('gap', 'c2'), 'ET-08': ('fadein', 'c1'), 'ET-09': ('base', 'c1'),
                        'ET-10': ('base', 'c3')}.items():
    d = VARS[vk]
    ti, ci = K.find_clip(d, cid)
    et.append({'id': name, 'doc': vk, 'clip': cid, 'expect': K.effective_transition(d, ti, ci, SRC)})
OUT['effective_transition'] = et

# ---------- photo_transform ----------
pt = []
c3 = cl(B, 'c3')
for i, t in enumerate([S(8), S(8.75), S(9.5), S(10.96), S(11), S(7)], 1):
    pt.append({'id': f'PT-{i:02d}', 'clip': 'c3', 't': t, 'expect': K.photo_transform_at(c3, t)})
lin = copy.deepcopy(c3)
lin['motion']['easing'] = 'linear'
pt.append({'id': 'PT-07', 'clip_override': lin, 't': S(9.5), 'expect': K.photo_transform_at(lin, S(9.5))})
nom = copy.deepcopy(c3)
del nom['motion']
pt.append({'id': 'PT-08', 'clip_override': nom, 't': S(9.5), 'expect': K.photo_transform_at(nom, S(9.5))})
OUT['photo_transform'] = pt

# ---------- compose_at ----------
V_HID = variant(lambda d: tr(d, 'V2').update(visible=False))
V_MUTE = variant(lambda d: tr(d, 'A1').update(muted=True))
V_SPEED = variant(lambda d: cl(d, 'c1').update(speed={'num': 2, 'den': 1}, src_out=S(8)))
VARS.update(hidden=V_HID, muted=V_MUTE, speed=V_SPEED)
OUT['docs'].update(hidden=V_HID, muted=V_MUTE, speed=V_SPEED)
ca = []
for name, vk, t in [('CA-01', 'base', 0), ('CA-02', 'base', S(0.5)), ('CA-03', 'base', S(2.5)),
                    ('CA-04', 'base', S(3.5)), ('CA-05', 'base', S(4)), ('CA-06', 'base', S(4.48)),
                    ('CA-07', 'base', S(4.5)), ('CA-08', 'base', S(9.5)), ('CA-09', 'base', S(10.5)),
                    ('CA-10', 'base', S(11)), ('CA-11', 'hidden', S(2.5)), ('CA-12', 'muted', S(5)),
                    ('CA-13', 'dip', S(3.8)), ('CA-14', 'dip', S(4.2)), ('CA-15', 'fadein', S(0.25)),
                    ('CA-16', 'speed', S(1.5)), ('CA-17', 'short', S(3.9)), ('CA-18', 'gap', S(4.5)),
                    ('CA-19', 'base', S(8.5))]:
    ca.append({'id': name, 'doc': vk, 't': t, 'expect': K.compose_at(VARS[vk], t, SRC)})
OUT['compose_at'] = ca

# ---------- audio_envelope / schedule ----------
ae = []
for name, vk, cid in [('AE-01', 'base', 'a1'), ('AE-02', 'base', 'c1'), ('AE-03', 'base', 'c2'), ('AE-04', 'dip', 'c1'),
                      ('AE-05', 'dip', 'c2'), ('AE-06', 'short', 'c1')]:
    ae.append({'id': name, 'doc': vk, 'clip': cid, 'expect': K.audio_envelope(VARS[vk], cid, SRC)})
OUT['audio_envelope'] = ae
se = []
pts_a1 = K.audio_envelope(B, 'a1', SRC)['points']
for name, t0, c0, rate in [('SE-01', 0, 10.0, 1), ('SE-02', S(0.5), 3.25, 1), ('SE-03', S(5), 0.0, 2),
                           ('SE-04', S(10), 1.0, 1)]:
    se.append({'id': name, 'points': pts_a1, 't0': t0, 'ctx_time0': c0, 'rate': rate,
               'expect': K.schedule_envelope(pts_a1, t0, c0, rate)})
OUT['schedule_envelope'] = se

# ---------- commands ----------
cmds = []


def case(cid, op, args, doc_key='base'):
    d = VARS[doc_key]
    cmd = {'op': op, 'args': args}
    item = {'id': cid, 'op': op, 'args': args}
    if doc_key != 'base':
        item['doc'] = doc_key
    try:
        res, inv = K.apply(d, cmd)
    except K.Err as e:
        item['expect'] = {'error': str(e)}
        cmds.append(item)
        return
    back, _ = K.apply(res, inv) if inv['op'] != 'delete_clip' else (K.delete_clip(res, inv['args']['clip'], inv['args']['ripple']), None)
    assert K.canon(back) == K.canon(d), ('inverse failed', cid)
    item['expect'] = {'doc': res, 'inverse': inv}
    cmds.append(item)


V_LOCK = variant(lambda d: tr(d, 'V1').update(locked=True))
V_DUCK = variant(lambda d: cl(d, 'a1')['effects'].append(
    {'id': 'dk', 'type': 'audio.duck', 'enabled': True,
     'params': {'sidechain_tracks': ['A2'], 'threshold_db': -30, 'depth_db': 12, 'attack_ms': 30, 'release_ms': 400}}))
V_4V = variant(lambda d: [d['tracks'].insert(i, {'id': f'V{n}', 'kind': 'video', 'clips': []}) for i, n in ((2, 3), (3, 4))])
VARS.update(locked=V_LOCK, duck=V_DUCK, four_video=V_4V)
OUT['docs'].update(locked=V_LOCK, duck=V_DUCK, four_video=V_4V)

newv = {'id': 'n1', 'source': {'kind': 'video', 'media_id': 'm1', 'sha256': 'h1'}, 'at': S(11), 'src_in': 0,
        'src_out': S(2), 'speed': {'num': 1, 'den': 1}, 'fit': 'fit',
        'audio': {'gain_db': 0.0, 'fade_in': 0, 'fade_out': 0, 'mute': False}, 'effects': []}
case('ADD-01', 'add_clip', {'track': 'V1', 'clip': newv, 'ripple': False})
case('ADD-02', 'add_clip', {'track': 'V1', 'clip': dict(newv, at=S(4)), 'ripple': True})
case('ADD-03', 'add_clip', {'track': 'V1', 'clip': dict(newv, at=S(3)), 'ripple': False})
case('ADD-04', 'add_clip', {'track': 'V1', 'clip': dict(newv, at=S(3)), 'ripple': True})
case('ADD-05', 'add_clip', {'track': 'A1', 'clip': newv, 'ripple': False})
case('ADD-06', 'add_clip', {'track': 'V1', 'clip': dict(newv, id='c1'), 'ripple': False})
case('ADD-07', 'add_clip', {'track': 'V1', 'clip': dict(newv, at=S(11) + 1), 'ripple': False})
case('ADD-08', 'add_clip', {'track': 'V9', 'clip': newv, 'ripple': False})
case('ADD-09', 'add_clip', {'track': 'V2', 'clip': dict(newv, id='bad id'), 'ripple': False})
case('ADD-10', 'add_clip', {'track': 'A2', 'clip': {'id': 'n2', 'source': {'kind': 'audio', 'media_id': 'm3', 'sha256': 'h3'},
                                                   'at': 0, 'src_in': 0, 'src_out': 0, 'speed': {'num': 1, 'den': 1},
                                                   'effects': []}, 'ripple': False})
case('DUP-01', 'duplicate_clip', {'clip': 'c3', 'new_id': 'c3d'})
case('DUP-02', 'duplicate_clip', {'clip': 'c1', 'new_id': 'c1d'})
case('DUP-03', 'duplicate_clip', {'clip': 'p1', 'new_id': 'p1d'})
case('DUP-04', 'duplicate_clip', {'clip': 'a1', 'new_id': 'mk1'})
case('DUP-05', 'duplicate_clip', {'clip': 'zz', 'new_id': 'q'})
case('TRN-01', 'set_transition', {'clip': 'c2', 'transition': {'kind': 'dip_to_black', 'duration': S(0.8), 'align': 'center'}})
case('TRN-02', 'set_transition', {'clip': 'c2', 'transition': None})
case('TRN-03', 'set_transition', {'clip': 'c2', 'transition': {'kind': 'cut', 'duration': 0}})
case('TRN-04', 'set_transition', {'clip': 'c1', 'transition': {'kind': 'crossfade', 'duration': S(0.4), 'align': 'center'}})
case('TRN-05', 'set_transition', {'clip': 'c1', 'transition': {'kind': 'fade_in', 'duration': S(0.4), 'align': 'center'}})
case('TRN-06', 'set_transition', {'clip': 'c2', 'transition': {'kind': 'crossfade', 'duration': S(2.08), 'align': 'center'}})
case('TRN-07', 'set_transition', {'clip': 'c2', 'transition': {'kind': 'crossfade', 'duration': S(0.04), 'align': 'center'}})
case('TRN-08', 'set_transition', {'clip': 'c2', 'transition': {'kind': 'fade_out', 'duration': S(0.4), 'align': 'center'}})
case('TRN-09', 'set_transition', {'clip': 'a1', 'transition': {'kind': 'fade_in', 'duration': S(0.4), 'align': 'center'}})
case('TRN-10', 'set_transition', {'clip': 'c2', 'transition': {'kind': 'dip_to_black', 'duration': S(0.8), 'align': 'start'}})
case('TRN-11', 'set_transition', {'clip': 'c3', 'transition': {'kind': 'crossfade', 'duration': S(2), 'align': 'end'}})
cdl = {'id': 'e9', 'type': 'color.cdl', 'enabled': True,
       'params': {'slope': [1.1, 1.0, 0.9], 'offset': [0.0, 0.0, 0.01], 'power': [1.0, 1.0, 1.0], 'saturation': 1.0}}
case('EF-01', 'set_effect', {'owner': 'c1', 'effect': cdl})
case('EF-02', 'set_effect', {'owner': 'c1', 'effect': cdl, 'index': 0})
case('EF-03', 'set_effect', {'owner': 'c1', 'effect': {'id': 'e1', 'type': 'color.basic', 'enabled': False,
                                                        'params': {'exposure_ev': 0.5}}})
case('EF-04', 'set_effect', {'owner': 'c1', 'effect': {'id': 'e8', 'type': 'color.basic', 'enabled': True,
                                                        'params': {'exposure_ev': 6}}})
case('EF-05', 'set_effect', {'owner': 'a1', 'effect': cdl})
case('EF-06', 'set_effect', {'owner': 'c1', 'effect': {'id': 'e8', 'type': 'color.match', 'enabled': True, 'params': {}}})
case('EF-07', 'set_effect', {'owner': 'c1', 'effect': dict(cdl, id='c2')})
case('EF-08', 'set_effect', {'owner': 'master', 'effect': {'id': 'mx', 'type': 'color.curves', 'enabled': True,
                                                            'params': {'master': [[0, 0], [0.5, 0.6], [1, 1]]}}})
case('EF-09', 'set_effect', {'owner': 'master', 'effect': {'id': 'mx', 'type': 'color.curves', 'enabled': True,
                                                            'params': {'master': [[0, 0], [0.5, 0.6], [0.5, 0.7], [1, 1]]}}})
case('EF-10', 'set_effect', {'owner': 'a1', 'effect': {'id': 'dk2', 'type': 'audio.duck', 'enabled': True,
                                                        'params': {'sidechain_tracks': ['A2'], 'threshold_db': -30,
                                                                   'depth_db': 12, 'attack_ms': 30, 'release_ms': 400}}})
case('EF-11', 'set_effect', {'owner': 'a1', 'effect': {'id': 'dk2', 'type': 'audio.duck', 'enabled': True,
                                                        'params': {'sidechain_tracks': ['V1'], 'threshold_db': -30,
                                                                   'depth_db': 12, 'attack_ms': 30, 'release_ms': 400}}})
case('EF-12', 'set_effect', {'owner': 'c1', 'effect': cdl, 'index': 5})
case('EF-13', 'set_effect', {'owner': 'nope', 'effect': cdl})
case('EF-14', 'remove_effect', {'owner': 'c1', 'effect': 'e1'})
case('EF-15', 'remove_effect', {'owner': 'c1', 'effect': 'e7'})
V_TWOFX = variant(lambda d: cl(d, 'c1')['effects'].append(
    {'id': 'e2', 'type': 'color.cdl', 'enabled': True,
     'params': {'slope': [1, 1, 1], 'offset': [0, 0, 0], 'power': [1, 1, 1], 'saturation': 1}}))
VARS['twofx'] = V_TWOFX
OUT['docs']['twofx'] = V_TWOFX
case('EF-16', 'move_effect', {'owner': 'c1', 'effect': 'e2', 'index': 0}, 'twofx')
case('EF-17', 'move_effect', {'owner': 'c1', 'effect': 'e2', 'index': 2}, 'twofx')
case('EF-18', 'move_effect', {'owner': 'c1', 'effect': 'e1', 'index': 0}, 'twofx')
case('CP-01', 'set_clip_props', {'clip': 'c1', 'patch': {'audio': {'gain_db': -4.5, 'fade_in': S(0.2)}}})
case('CP-02', 'set_clip_props', {'clip': 'c1', 'patch': {'audio': {'fade_in': S(3), 'fade_out': S(2)}}})
case('CP-03', 'set_clip_props', {'clip': 'c1', 'patch': {'audio': {'fade_in': S(0.2) + 1}}})
case('CP-04', 'set_clip_props', {'clip': 'c1', 'patch': {'transform': {'scale': 2.0}, 'fit': 'fill'}})
case('CP-05', 'set_clip_props', {'clip': 'c1', 'patch': {'transform': {'scale': 9}}})
case('CP-06', 'set_clip_props', {'clip': 'c1', 'patch': {'motion': None}})
case('CP-07', 'set_clip_props', {'clip': 'c3', 'patch': {'motion': None}})
case('CP-08', 'set_clip_props', {'clip': 'c3', 'patch': {'duration': S(5)}})
case('CP-09', 'set_clip_props', {'clip': 'p1', 'patch': {'duration': S(1)}})
case('CP-10', 'set_clip_props', {'clip': 'p1', 'patch': {'duration': S(0.01)}})
case('CP-11', 'set_clip_props', {'clip': 'a1', 'patch': {'fit': 'fit'}})
case('CP-12', 'set_clip_props', {'clip': 'p1', 'patch': {'transform': None, 'fit': 'none'}})
case('CP-13', 'set_clip_props', {'clip': 'c3', 'patch': {'motion': {'kind': 'pan_zoom', 'from': {'scale': 1, 'center': [0.5, 0.5]},
                                                                     'to': {'scale': 0.9, 'center': [0.5, 0.5]}, 'easing': 'linear'}}})
V_PRE = variant(lambda d: tr(d, 'V1')['clips'].insert(3, {'id': 'c4', 'source': {'kind': 'video', 'media_id': 'm1',
                                                                                    'sha256': 'h1'},
                                                            'at': S(12), 'src_in': 0, 'src_out': S(1),
                                                            'speed': {'num': 1, 'den': 1}, 'effects': []}))
VARS['tail'] = V_PRE
OUT['docs']['tail'] = V_PRE
case('CP-14', 'set_clip_props', {'clip': 'c3', 'patch': {'duration': S(5)}}, 'tail')
case('TP-01', 'set_track_props', {'track': 'V2', 'patch': {'visible': False, 'locked': True}})
case('TP-02', 'set_track_props', {'track': 'A1', 'patch': {'muted': True, 'gain_db': None}})
case('TP-03', 'set_track_props', {'track': 'V1', 'patch': {'muted': True}})
case('TP-04', 'set_track_props', {'track': 'A1', 'patch': {'gain_db': 30}})
case('TP-05', 'set_track_props', {'track': 'A1', 'patch': {'locked': 1}})
case('TK-01', 'add_track', {'kind': 'video', 'track': 'V3'})
case('TK-02', 'add_track', {'kind': 'audio', 'track': 'A3', 'index': 2, 'props': {'muted': True}})
case('TK-03', 'add_track', {'kind': 'audio', 'track': 'A3', 'index': 1})
case('TK-04', 'add_track', {'kind': 'video', 'track': 'V5'}, 'four_video')
case('TK-05', 'add_track', {'kind': 'video', 'track': 'c1'})
case('TK-06', 'remove_track', {'track': 'A2'})
case('TK-07', 'remove_track', {'track': 'V2'})
case('TK-08', 'remove_track', {'track': 'A2'}, 'duck')
V_EMPTYV2 = variant(lambda d: tr(d, 'V2').update(clips=[], locked=True))
VARS['emptyv2'] = V_EMPTYV2
OUT['docs']['emptyv2'] = V_EMPTYV2
case('TK-09', 'remove_track', {'track': 'V2'}, 'emptyv2')
V_ONEA = variant(lambda d: d['tracks'].pop(K.find_track(d, 'A1')))
VARS['one_audio'] = V_ONEA
OUT['docs']['one_audio'] = V_ONEA
case('TK-10', 'remove_track', {'track': 'A2'}, 'one_audio')
case('MK-01', 'add_marker', {'marker': {'id': 'mk2', 'at': S(1), 'kind': 'beat', 'label': ''}})
case('MK-02', 'add_marker', {'marker': {'id': 'mk2', 'at': S(3), 'kind': 'user', 'label': ''}})
case('MK-03', 'add_marker', {'marker': {'id': 'mk2', 'at': S(3), 'kind': 'beat', 'label': ''}})
case('MK-04', 'add_marker', {'marker': {'id': 'mk2', 'at': S(1) + 5, 'kind': 'user', 'label': ''}})
case('MK-05', 'add_marker', {'marker': {'id': 'c1', 'at': S(1), 'kind': 'user', 'label': ''}})
case('MK-06', 'add_marker', {'marker': {'id': 'mk2', 'at': S(1), 'kind': 'user', 'label': 'x' * 201}})
case('MK-07', 'update_marker', {'marker': 'mk1', 'patch': {'at': S(6), 'label': 'B'}})
case('MK-08', 'update_marker', {'marker': 'mk9', 'patch': {'label': 'B'}})
case('MK-09', 'remove_marker', {'marker': 'mk1'})
case('MK-10', 'update_marker', {'marker': 'mk1', 'patch': {'kind': 'beat'}})
OUT['commands'] = cmds

lk = []
for name, vk, cmd in [
        ('LK-01', 'locked', {'op': 'split_clip', 'args': {'clip': 'c1', 'at': S(1), 'new_id': 'x'}}),
        ('LK-02', 'locked', {'op': 'move_clip', 'args': {'clip': 'p1', 'new_at': S(5), 'track': 'V1'}}),
        ('LK-03', 'locked', {'op': 'move_clip', 'args': {'clip': 'p1', 'new_at': S(5)}}),
        ('LK-04', 'locked', {'op': 'add_clip', 'args': {'track': 'V1', 'clip': newv, 'ripple': False}}),
        ('LK-05', 'locked', {'op': 'set_track_props', 'args': {'track': 'V1', 'patch': {'locked': False}}}),
        ('LK-06', 'locked', {'op': 'set_effect', 'args': {'owner': 'c1', 'effect': cdl}}),
        ('LK-07', 'locked', {'op': 'set_effect', 'args': {'owner': 'master', 'effect': cdl}}),
        ('LK-08', 'locked', {'op': 'add_marker', 'args': {'marker': {'id': 'q', 'at': 0, 'kind': 'user', 'label': ''}}}),
        ('LK-09', 'base', {'op': 'delete_clip', 'args': {'clip': 'c1', 'ripple': False}})]:
    lk.append({'id': name, 'doc': vk, 'command': cmd, 'expect': K.check_locked(VARS[vk], cmd)})
OUT['check_locked'] = lk

# ---------- геометрия ----------
VIEW = {'scroll': 0, 'tpp': 14112000, 'width': 1000}   # 20 мс на пиксель: 1 с = 50 px
OUT['view'] = VIEW
OUT['layout_rows'] = {'doc': 'base', 'expect': U.layout_rows(B)}
OUT['layout_rows_four_video'] = {'doc': 'four_video', 'expect': U.layout_rows(V_4V)}
geo = []
for t in [0, S(1), S(1) + 1, S(1) - 1, -S(1)]:
    geo.append({'fn': 'tick_to_x', 'view': VIEW, 't': t, 'expect': U.tick_to_x(t, VIEW)})
for x in [160, 210, 159, 1160]:
    geo.append({'fn': 'x_to_tick', 'view': VIEW, 'x': x, 'expect': U.x_to_tick(x, VIEW)})
v2 = {'scroll': S(2), 'tpp': 7056000, 'width': 800}
geo.append({'fn': 'tick_to_x', 'view': v2, 't': S(3), 'expect': U.tick_to_x(S(3), v2)})
geo.append({'fn': 'x_to_tick', 'view': v2, 'x': 260, 'expect': U.x_to_tick(260, v2)})
OUT['geometry'] = geo
rows = U.layout_rows(B)
ht = []
pts = [('HT-01', 300, 10), ('HT-02', 100, 10), ('HT-03', 50, 40), ('HT-04', 170, 100), ('HT-05', 165, 100),
       ('HT-06', 361, 100), ('HT-07', 355, 100), ('HT-08', 355, 110), ('HT-09', 360, 105), ('HT-10', 365, 100),
       ('HT-11', 262, 50), ('HT-12', 650, 100), ('HT-13', 700, 100), ('HT-14', 500, 50), ('HT-15', 300, 93),
       ('HT-16', 300, 170), ('HT-17', 300, 200), ('HT-18', 400, 216), ('HT-19', -1, 50), ('HT-20', 160, 165),
       ('HT-21', 559, 100), ('HT-22', 715, 100)]
for name, x, y in pts:
    ht.append({'id': name, 'doc': 'base', 'view': VIEW, 'x': x, 'y': y, 'expect': U.hit_test(B, VIEW, x, y)})
ht.append({'id': 'HT-24', 'doc': 'base', 'view': VIEW, 'x': 360, 'y': 140, 'expect': U.hit_test(B, VIEW, 360, 140)})
ht.append({'id': 'HT-25', 'doc': 'base', 'view': VIEW, 'x': 359, 'y': 140, 'expect': U.hit_test(B, VIEW, 359, 140)})
ht.append({'id': 'HT-23', 'doc': 'locked', 'view': VIEW, 'x': 300, 'y': 100, 'expect': U.hit_test(V_LOCK, VIEW, 300, 100)})
OUT['hit_test'] = ht
zm = []
for name, view, ax, dr in [('ZM-01', VIEW, 160, 1), ('ZM-02', VIEW, 410, 1), ('ZM-03', VIEW, 410, -1),
                           ('ZM-04', {'scroll': 0, 'tpp': 705600, 'width': 1000}, 300, 1),
                           ('ZM-05', {'scroll': 0, 'tpp': 7056000000, 'width': 1000}, 300, -1),
                           ('ZM-06', {'scroll': S(1), 'tpp': 14112000, 'width': 1000}, 200, -1),
                           ('ZM-07', {'scroll': 0, 'tpp': 10000000, 'width': 1000}, 160, 1)]:
    zm.append({'id': name, 'view': view, 'anchor_x': ax, 'direction': dr, 'expect': U.zoom_at(view, ax, dr)})
OUT['zoom_at'] = zm
OUT['zoom_to_fit'] = [{'id': f'ZF-0{i}', 'doc': 'base', 'width': w, 'expect': U.zoom_to_fit(B, w)}
                      for i, w in enumerate([1000, 100, 1, 2000000], 1)]
OUT['visible_clips'] = [{'id': f'VC-0{i}', 'doc': 'base', 'view': v, 'expect': U.visible_clips(B, v)}
                        for i, v in enumerate([VIEW, {'scroll': S(9), 'tpp': 705600, 'width': 500},
                                               {'scroll': S(20), 'tpp': 705600, 'width': 500},
                                               {'scroll': S(4.4), 'tpp': 705600, 'width': 100}], 1)]
# ---------- привязка ----------
OUT['snap_anchors'] = [
    {'id': 'SA-01', 'doc': 'base', 'playhead': S(5), 'exclude': None, 'expect': U.snap_anchors(B, S(5))},
    {'id': 'SA-02', 'doc': 'base', 'playhead': 0, 'exclude': 'c2', 'expect': U.snap_anchors(B, 0, 'c2')}]
anch = U.snap_anchors(B, S(5), 'p1')
thr = U.SNAP_PX * VIEW['tpp']
sm = []
for name, raw, d in [('SM-01', S(3.9), S(2)), ('SM-02', S(6.1) - S(2), S(2)), ('SM-03', S(6.5), S(2)),
                     ('SM-04', S(2.02), S(2)), ('SM-05', S(5.05), S(1)), ('SM-06', -S(0.1), S(2)),
                     ('SM-07', S(8.95), S(2.0))]:
    sm.append({'id': name, 'raw': raw, 'duration': d, 'anchors': anch, 'threshold': thr,
               'expect': U.snap_move(raw, d, anch, thr, F)})
OUT['snap_move'] = sm

# ---------- жесты ----------
item_v = {'kind': 'video', 'media_id': 'm1', 'sha256': 'h1', 'duration': S(3) + 7}
item_p = {'kind': 'photo', 'asset_version_id': 'p7', 'sha256': 'h7'}
item_a = {'kind': 'audio', 'media_id': 'm3', 'sha256': 'h3', 'duration': S(30)}
row = {r['track']: r for r in rows}
yV1 = row['V1']['y'] + 40
yV1top = row['V1']['y'] + 5
yV2 = row['V2']['y'] + 40
yA1 = row['A1']['y'] + 20
yA2 = row['A2']['y'] + 20


def X(t):
    return U.tick_to_x(t, VIEW)


scen = [
    ('GS-01', 'клик по линейке и протяжка', {}, [
        {'type': 'down', 'x': X(S(1.01)), 'y': 10}, {'type': 'move', 'x': X(S(2.5)), 'y': 12},
        {'type': 'move', 'x': X(S(30)), 'y': 12}, {'type': 'up', 'x': X(S(30)), 'y': 12}]),
    ('GS-02', 'клик без протяжки только выделяет', {}, [
        {'type': 'down', 'x': X(S(1)), 'y': yV1}, {'type': 'move', 'x': X(S(1)) + 3, 'y': yV1},
        {'type': 'up', 'x': X(S(1)) + 3, 'y': yV1}]),
    ('GS-03', 'перенос фото по V2 с привязкой начала к маркеру', {}, [
        {'type': 'down', 'x': X(S(3)), 'y': yV2}, {'type': 'move', 'x': X(S(3)) + 10, 'y': yV2},
        {'type': 'move', 'x': X(S(4.1)), 'y': yV2},
        {'type': 'up', 'x': X(S(4.1)), 'y': yV2}]),
    ('GS-04', 'перенос фото на другую дорожку без привязки (Shift)', {}, [
        {'type': 'down', 'x': X(S(3)), 'y': yV2}, {'type': 'move', 'x': X(S(3)) + 20, 'y': yV2, 'shift': True},
        {'type': 'move', 'x': X(S(12.33)), 'y': yV1, 'shift': True},
        {'type': 'up', 'x': X(S(12.33)), 'y': yV1, 'shift': True}]),
    ('GS-05', 'перенос на занятое место (и на аудиодорожку) недопустим и откатывается', {}, [
        {'type': 'down', 'x': X(S(1)), 'y': yV1}, {'type': 'move', 'x': X(S(1)) + 50, 'y': yA2},
        {'type': 'up', 'x': X(S(1)) + 50, 'y': yA2}]),
    ('GS-06', 'обрезка конца видео', {}, [
        {'type': 'down', 'x': X(S(4)) - 2, 'y': yV1}, {'type': 'move', 'x': X(S(3.5)), 'y': yV1},
        {'type': 'up', 'x': X(S(3.5)), 'y': yV1}]),
    ('GS-07', 'обрезка начала c1 без изменения позиции не даёт команды', {}, [
        {'type': 'down', 'x': X(0) + 1, 'y': yV1}, {'type': 'move', 'x': X(0) + 6, 'y': yV1},
        {'type': 'move', 'x': X(0) + 1, 'y': yV1}, {'type': 'up', 'x': X(0) + 1, 'y': yV1}]),
    ('GS-08', 'обрезка конца фото меняет длительность', {}, [
        {'type': 'down', 'x': X(S(11)) - 2, 'y': yV1}, {'type': 'move', 'x': X(S(12.5)), 'y': yV1},
        {'type': 'up', 'x': X(S(12.5)), 'y': yV1}]),
    ('GS-09', 'ручка перехода', {}, [
        {'type': 'down', 'x': X(S(4)), 'y': yV1top}, {'type': 'move', 'x': X(S(4)) + 25, 'y': yV1top},
        {'type': 'up', 'x': X(S(4)) + 25, 'y': yV1top}]),
    ('GS-10', 'бритва', {'tool': 'razor'}, [{'type': 'down', 'x': X(S(1.5)), 'y': yV1}]),
    ('GS-11', 'бритва с привязкой к маркеру', {'tool': 'razor'}, [{'type': 'down', 'x': X(S(3.05)), 'y': yV1}]),
    ('GS-12', 'клик по пустому месту снимает выделение', {}, [{'type': 'down', 'x': X(S(5)), 'y': yA2}]),
    ('GS-13', 'отмена Escape во время переноса', {}, [
        {'type': 'down', 'x': X(S(1)), 'y': yV1}, {'type': 'move', 'x': X(S(2)), 'y': yV1}, {'type': 'cancel'},
        {'type': 'up', 'x': X(S(2)), 'y': yV1}]),
    ('GS-14', 'бросок видео из бина в конец V1', {}, [
        {'type': 'bin_over', 'x': X(S(11.1)), 'y': yV1, 'item': item_v},
        {'type': 'bin_drop', 'x': X(S(11.1)), 'y': yV1, 'item': item_v}]),
    ('GS-15', 'бросок фото со вставкой (Alt)', {}, [
        {'type': 'bin_drop', 'x': X(S(4)), 'y': yV1, 'item': item_p, 'alt': True}]),
    ('GS-16', 'бросок аудио на видеодорожку недопустим', {}, [
        {'type': 'bin_over', 'x': X(S(1)), 'y': yV1, 'item': item_a},
        {'type': 'bin_drop', 'x': X(S(1)), 'y': yV1, 'item': item_a}]),
    ('GS-17', 'бросок аудио на A2, уход из области', {}, [
        {'type': 'bin_over', 'x': X(S(2)), 'y': yA2, 'item': item_a}, {'type': 'bin_leave'}]),
    ('GS-18', 'перенос на заблокированной дорожке не начинается', {'doc': 'locked'}, [
        {'type': 'down', 'x': X(S(1)), 'y': yV1}, {'type': 'move', 'x': X(S(2)), 'y': yV1},
        {'type': 'up', 'x': X(S(2)), 'y': yV1}]),
    ('GS-19', 'привязка выключена глобально', {'snap': False}, [
        {'type': 'down', 'x': X(S(9)), 'y': yV1}, {'type': 'move', 'x': X(S(13.11)), 'y': yV1},
        {'type': 'up', 'x': X(S(13.11)), 'y': yV1}]),
    ('GS-21', 'обрезка начала c2 зажимается концом c1', {}, [
        {'type': 'down', 'x': X(S(4)) + 1, 'y': yV1 + 20}, {'type': 'move', 'x': X(S(2)), 'y': yV1 + 20},
        {'type': 'up', 'x': X(S(2)), 'y': yV1 + 20}]),
    ('GS-22', 'длина перехода зажимается пределом', {}, [
        {'type': 'down', 'x': X(S(4)), 'y': yV1top}, {'type': 'move', 'x': X(S(7)), 'y': yV1top},
        {'type': 'up', 'x': X(S(7)), 'y': yV1top}]),
    ('GS-20', 'обрезка конца аудио с привязкой к playhead', {'playhead': S(8.4)}, [
        {'type': 'down', 'x': X(S(10)) - 2, 'y': yA1}, {'type': 'move', 'x': X(S(8.45)), 'y': yA1},
        {'type': 'up', 'x': X(S(8.45)), 'y': yA1}]),
]
gs = []
for name, desc, opt, events in scen:
    vk = opt.get('doc', 'base')
    ctx = {'doc': VARS[vk], 'view': VIEW, 'playhead': opt.get('playhead', S(5)), 'tool': opt.get('tool', 'select'),
           'snap': opt.get('snap', True), 'src_dur': SRC, 'new_ids': ['g1']}
    st = {'mode': 'idle'}
    outs = []
    for ev in events:
        st, o = U.gesture(st, ev, ctx)
        outs.append(o)
    gs.append({'id': name, 'desc': desc, 'doc': vk,
               'ctx': {'playhead': ctx['playhead'], 'tool': ctx['tool'], 'snap': ctx['snap'], 'new_ids': ['g1']},
               'events': events, 'expect': outs})
OUT['gestures'] = gs

# ---------- горячие клавиши ----------
kc = []
keys = [({'key': ' '}, 'KB-01'), ({'key': 'K'}, 'KB-02'), ({'key': 'l'}, 'KB-03'), ({'key': 'j'}, 'KB-04'),
        ({'key': 's'}, 'KB-05'), ({'key': 'S', 'shift': True}, 'KB-06'), ({'key': 'Delete'}, 'KB-07'),
        ({'key': 'Backspace', 'shift': True}, 'KB-08'), ({'key': 'ArrowLeft'}, 'KB-09'),
        ({'key': 'ArrowRight', 'shift': True, 'repeat': True}, 'KB-10'), ({'key': 'z', 'ctrl': True}, 'KB-11'),
        ({'key': 'Z', 'meta': True, 'shift': True}, 'KB-12'), ({'key': 'y', 'ctrl': True}, 'KB-13'),
        ({'key': 's', 'ctrl': True}, 'KB-14'), ({'key': '='}, 'KB-15'), ({'key': '-', 'repeat': True}, 'KB-16'),
        ({'key': 'i'}, 'KB-17'), ({'key': 'I', 'shift': True}, 'KB-18'), ({'key': 'o'}, 'KB-19'),
        ({'key': 'm'}, 'KB-20'), ({'key': 'm', 'repeat': True}, 'KB-21'), ({'key': 's', 'editing': True}, 'KB-22'),
        ({'key': 'c', 'alt': True}, 'KB-23'), ({'key': 'c'}, 'KB-24'), ({'key': 'v'}, 'KB-25'),
        ({'key': 'n'}, 'KB-26'), ({'key': 'Home'}, 'KB-27'), ({'key': 'End'}, 'KB-28'), ({'key': 'Escape'}, 'KB-29'),
        ({'key': '\\'}, 'KB-30'), ({'key': 'ArrowUp'}, 'KB-31'), ({'key': 'ArrowDown'}, 'KB-32'),
        ({'key': 'q'}, 'KB-33'), ({'key': ' ', 'composing': True}, 'KB-34')]
for ev, name in keys:
    full = {'key': ev['key'], 'ctrl': ev.get('ctrl', False), 'meta': ev.get('meta', False),
            'shift': ev.get('shift', False), 'alt': ev.get('alt', False), 'repeat': ev.get('repeat', False),
            'editing': ev.get('editing', False), 'composing': ev.get('composing', False)}
    kc.append({'id': name, 'event': full, 'expect': U.match_shortcut(full)})
OUT['shortcuts'] = kc

# ---------- цели команд ----------
st_ = []
for name, vk, sel, p in [('ST-01', 'base', 'c1', S(1)), ('ST-02', 'base', 'c1', S(5)), ('ST-03', 'base', None, S(3)),
                         ('ST-04', 'base', None, S(4)), ('ST-05', 'locked', None, S(3)),
                         ('ST-06', 'locked', 'c1', S(1)), ('ST-07', 'base', None, S(20)),
                         ('ST-08', 'base', 'a1', S(5))]:
    st_.append({'id': name, 'doc': vk, 'selection': sel, 'playhead': p, 'expect': U.split_target(VARS[vk], sel, p)})
OUT['split_target'] = st_
OUT['adjacent_edit'] = [{'id': f'AJ-0{i}', 'doc': 'base', 'playhead': p, 'direction': dr,
                         'expect': U.adjacent_edit(B, p, dr)}
                        for i, (p, dr) in enumerate([(S(5), -1), (S(5), 1), (0, -1), (S(11), 1), (S(4), 1),
                                                    (S(4), -1)], 1)]

# ---------- воспроизведение ----------
pb = []


def scenario(name, desc, events, vk='base', start=None):
    ctx = {'doc': VARS[vk], 'src_dur': SRC}
    s = U.new_playback_state() if start is None else copy.deepcopy(start)
    outs = []
    for ev in events:
        s, fx = U.playback(s, ev, ctx)
        outs.append({'effects': fx, 'state': U.summary(s)})
    pb.append({'id': name, 'desc': desc, 'doc': vk, 'events': events, 'expect': outs})
    return s


scenario('PB-01', 'пуск с неготовыми колодами → ожидание; готовность → воспроизведение', [
    {'type': 'play', 'now': 0}, {'type': 'deck_ready', 'deck': 'V1/a'}, {'type': 'deck_ready', 'deck': 'A1/a'},
    {'type': 'tick', 't': S(1), 'now': 1000}])
scenario('PB-02', 'предзагрузка следующего клипа и кроссфейд', [
    {'type': 'seek', 't': S(1.4), 'now': 0}, {'type': 'deck_ready', 'deck': 'V1/a'},
    {'type': 'deck_ready', 'deck': 'A1/a'}, {'type': 'play', 'now': 10}, {'type': 'tick', 't': S(1.6), 'now': 210},
    {'type': 'deck_ready', 'deck': 'V1/b'}, {'type': 'deck_ready', 'deck': 'V2/a'},
    {'type': 'tick', 't': S(2), 'now': 610}, {'type': 'tick', 't': S(3.6), 'now': 2210},
    {'type': 'tick', 't': S(4.6), 'now': 3210}])
scenario('PB-03', 'перемотка с устаревшей ссылкой перезагружает колоду', [
    {'type': 'seek', 't': S(1), 'now': 0}, {'type': 'deck_ready', 'deck': 'V1/a'},
    {'type': 'deck_ready', 'deck': 'A1/a'}, {'type': 'seek', 't': S(1.2), 'now': 20000},
    {'type': 'seek', 't': S(1.5), 'now': 60000}])
scenario('PB-04', 'ошибки колоды: повторы, затем отказ с уведомлением', [
    {'type': 'seek', 't': S(1), 'now': 0}, {'type': 'deck_ready', 'deck': 'A1/a'}, {'type': 'play', 'now': 1},
    {'type': 'deck_error', 'deck': 'V1/a', 'now': 2}, {'type': 'deck_error', 'deck': 'V1/a', 'now': 3},
    {'type': 'deck_error', 'deck': 'V1/a', 'now': 4}, {'type': 'deck_error', 'deck': 'V1/a', 'now': 5}])
scenario('PB-05', 'конец монтажа и повторный пуск с начала', [
    {'type': 'seek', 't': S(10.5), 'now': 0}, {'type': 'deck_ready', 'deck': 'V1/a'}, {'type': 'play', 'now': 1},
    {'type': 'tick', 't': S(11.2), 'now': 700}, {'type': 'play', 'now': 800}])
scenario('PB-06', 'петля I/O и скорость', [
    {'type': 'loop', 'range': [S(1), S(2)]}, {'type': 'seek', 't': S(1), 'now': 0},
    {'type': 'deck_ready', 'deck': 'V1/a'}, {'type': 'deck_ready', 'deck': 'A1/a'}, {'type': 'play', 'now': 1},
    {'type': 'rate', 'rate': 2}, {'type': 'rate', 'rate': 3}, {'type': 'tick', 't': S(2), 'now': 500},
    {'type': 'pause', 'now': 600}])
scenario('PB-07', 'скрытая дорожка: колода звука нужна, слоя нет', [
    {'type': 'seek', 't': S(2.5), 'now': 0}], 'hidden')
OUT['playback'] = pb

# ---------- цвет ----------
wc = []
for name, lift, gamma, gain in [
        ('WC-01', {'x': 0, 'y': 0, 'v': 0}, {'x': 0, 'y': 0, 'v': 0}, {'x': 0, 'y': 0, 'v': 0}),
        ('WC-02', {'x': 0.5, 'y': 0, 'v': 0}, {'x': 0, 'y': 0, 'v': 0.2}, {'x': 0, 'y': 0.5, 'v': -0.3}),
        ('WC-03', {'x': 3, 'y': 4, 'v': 1}, {'x': 0, 'y': 0, 'v': -1}, {'x': -0.2, 'y': -0.2, 'v': 1}),
        ('WC-04', {'x': 0, 'y': 0, 'v': -1}, {'x': 0.3, 'y': -0.4, 'v': 0}, {'x': 0, 'y': 0, 'v': 2})]:
    out = U.wheels_to_cdl(lift, gamma, gain)
    wc.append({'id': name, 'lift': lift, 'gamma': gamma, 'gain': gain, 'expect': out,
               'roundtrip': U.cdl_to_wheels(out)})
OUT['wheels'] = wc
cv = []
base_curve = [[0, 0], [0.5, 0.6], [1, 1]]
cv.append({'id': 'CV-01', 'op': 'add', 'points': base_curve, 'x': 0.25, 'y': 0.2, 'expect': U.curve_add(base_curve, 0.25, 0.2)})
cv.append({'id': 'CV-02', 'op': 'add', 'points': base_curve, 'x': 0.501, 'y': 0.2, 'expect': U.curve_add(base_curve, 0.501, 0.2)})
full = [[i / 15, i / 15] for i in range(16)]
cv.append({'id': 'CV-03', 'op': 'add', 'points': full, 'x': 0.03, 'y': 0.2, 'expect': U.curve_add(full, 0.03, 0.2)})
cv.append({'id': 'CV-04', 'op': 'add', 'points': base_curve, 'x': 1.5, 'y': -1, 'expect': U.curve_add(base_curve, 1.5, -1)})
cv.append({'id': 'CV-05', 'op': 'move', 'points': base_curve, 'i': 1, 'x': 0.99, 'y': 1.2, 'expect': U.curve_move(base_curve, 1, 0.99, 1.2)})
cv.append({'id': 'CV-06', 'op': 'move', 'points': base_curve, 'i': 0, 'x': 0.4, 'y': 0.1, 'expect': U.curve_move(base_curve, 0, 0.4, 0.1)})
cv.append({'id': 'CV-07', 'op': 'move', 'points': base_curve, 'i': 3, 'x': 0.4, 'y': 0.1, 'expect': U.curve_move(base_curve, 3, 0.4, 0.1)})
cv.append({'id': 'CV-08', 'op': 'remove', 'points': base_curve, 'i': 1, 'expect': U.curve_remove(base_curve, 1)})
cv.append({'id': 'CV-09', 'op': 'remove', 'points': base_curve, 'i': 2, 'expect': U.curve_remove(base_curve, 2)})
OUT['curves'] = cv
img = {'w': 4, 'h': 2, 'pixels': [[0, 0, 0], [255, 255, 255], [128, 128, 128], [255, 0, 0],
                                   [0, 255, 0], [0, 0, 255], [200, 150, 100], [128, 128, 128]]}
img2 = {'w': 300, 'h': 1, 'pixels': [[i % 256, 0, 0] for i in range(300)]}
OUT['scopes'] = [{'id': 'SC-01', 'image': img, 'expect': U.scopes(img['w'], img['h'], img['pixels'])},
                 {'id': 'SC-02', 'image': img2, 'expect': {'waveform_columns': len({c for c, _, _ in U.scopes(300, 1, img2['pixels'])['waveform']}),
                                                           'waveform_head': U.scopes(300, 1, img2['pixels'])['waveform'][:4]}}]

print(json.dumps(OUT, ensure_ascii=False, indent=1))
