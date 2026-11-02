"""Timeline document validator VT-01…VT-10 (FN-T-12); mirrors apps/photographer video-timeline-validate.ts."""

import math
import re
from typing import Any

from .timeline import TICKS_PER_SECOND, canonicalize, clip_duration, frame_ticks

SUPPORTED_FPS = {'24/1', '25/1', '30/1', '50/1', '60/1', '24000/1001', '30000/1001', '60000/1001'}
MAX_DOC_BYTES = 4 * 1024 * 1024
MAX_DURATION_TICKS = 3 * 3600 * TICKS_PER_SECOND
MAX_ISSUES = 100
_ID = re.compile(r'^[A-Za-z0-9_-]{1,64}$')

_KEYS = {
    'root': {'schema_version', 'sequence_id', 'base_revision', 'timebase', 'video', 'audio', 'tracks', 'markers', 'master', 'rights'},
    'timebase': {'ticks_per_second'}, 'video': {'fps', 'width', 'height', 'color', 'background'}, 'color': {'space', 'range'},
    'fps': {'num', 'den'}, 'audio': {'sample_rate', 'channels'},
    'track': {'id', 'kind', 'locked', 'visible', 'muted', 'gain_db', 'clips'},
    'clip': {'id', 'source', 'at', 'src_in', 'src_out', 'duration', 'speed', 'fit', 'transform', 'motion', 'audio', 'transition_in', 'effects'},
    'source': {'kind', 'media_id', 'asset_version_id', 'sha256'}, 'effect': {'id', 'type', 'enabled', 'params', 'meta'},
    'marker': {'id', 'at', 'kind', 'label'}, 'master': {'effects', 'audio'}, 'masterAudio': {'loudness'},
    'loudness': {'target_lufs', 'true_peak_db'}, 'rights': {'music_declared', 'declared_at'},
    'transition': {'kind', 'duration', 'align'}, 'clipAudio': {'gain_db', 'fade_in', 'fade_out', 'mute'},
    'transform': {'scale', 'position', 'rotate', 'crop'}, 'motion': {'kind', 'from', 'to', 'easing'}, 'pose': {'scale', 'center'},
}
_BASIC = {'exposure_ev': (-5, 5), **{k: (-100, 100) for k in (
    'temperature', 'tint', 'contrast', 'highlights', 'shadows', 'whites', 'blacks', 'saturation', 'vibrance')}}


def _is_int(v: Any) -> bool:
    return isinstance(v, int) and not isinstance(v, bool)


def _num(v: Any) -> bool:
    return isinstance(v, (int, float)) and not isinstance(v, bool) and math.isfinite(v)


def _in(v: Any, lo: float, hi: float) -> bool:
    return _num(v) and lo <= v <= hi


def _vec(v: Any, n: int, lo: float, hi: float) -> bool:
    return isinstance(v, list) and len(v) == n and all(_in(x, lo, hi) for x in v)


def _esc(s: str) -> str:
    return s.replace('~', '~0').replace('/', '~1')


def validate_document(doc: dict[str, Any], ctx: dict[str, Any]) -> list[dict[str, str]]:
    out: list[dict[str, str]] = []

    def add(rule: str, pointer: str, code: str) -> None:
        if len(out) < MAX_ISSUES:
            out.append({'rule': rule, 'pointer': pointer, 'code': code})

    def closed(o: Any, kind: str, p: str) -> None:
        if isinstance(o, dict):
            for k in o:
                if k not in _KEYS[kind]:
                    add('VT-01', f'{p}/{_esc(k)}', 'DOC_SCHEMA')

    # VT-01
    closed(doc, 'root', '')
    if doc.get('schema_version') != 1:
        add('VT-01', '/schema_version', 'DOC_SCHEMA')
    video, master = doc.get('video') or {}, doc.get('master') or {}
    closed(doc.get('timebase'), 'timebase', '/timebase')
    closed(video, 'video', '/video')
    closed(video.get('color'), 'color', '/video/color')
    closed(video.get('fps'), 'fps', '/video/fps')
    closed(doc.get('audio'), 'audio', '/audio')
    closed(master, 'master', '/master')
    closed(master.get('audio'), 'masterAudio', '/master/audio')
    closed((master.get('audio') or {}).get('loudness'), 'loudness', '/master/audio/loudness')
    closed(doc.get('rights'), 'rights', '/rights')
    f = video.get('fps')
    if not isinstance(f, dict) or f'{f.get("num")}/{f.get("den")}' not in SUPPORTED_FPS:
        add('VT-01', '/video/fps', 'DOC_SCHEMA')
    tracks = doc.get('tracks') if isinstance(doc.get('tracks'), list) else []
    for ti, t in enumerate(tracks):
        closed(t, 'track', f'/tracks/{ti}')
        for ci, c in enumerate(t.get('clips') or []):
            p = f'/tracks/{ti}/clips/{ci}'
            closed(c, 'clip', p)
            closed(c.get('source'), 'source', p + '/source')
            closed(c.get('transition_in'), 'transition', p + '/transition_in')
            closed(c.get('audio'), 'clipAudio', p + '/audio')
            closed(c.get('transform'), 'transform', p + '/transform')
            closed(c.get('motion'), 'motion', p + '/motion')
            for side in ('from', 'to'):
                closed((c.get('motion') or {}).get(side), 'pose', f'{p}/motion/{side}')
            for k in ('at', 'src_in', 'src_out', 'duration'):
                if k in c and not _is_int(c[k]):
                    add('VT-01', f'{p}/{k}', 'DOC_SCHEMA')
            for ei, e in enumerate(c.get('effects') or []):
                closed(e, 'effect', f'{p}/effects/{ei}')
    for mi, m in enumerate(doc.get('markers') or []):
        closed(m, 'marker', f'/markers/{mi}')
        if 'at' in m and not _is_int(m['at']):
            add('VT-01', f'/markers/{mi}/at', 'DOC_SCHEMA')
    if out:
        return out

    # VT-02
    nv = sum(t['kind'] == 'video' for t in tracks)
    na = sum(t['kind'] == 'audio' for t in tracks)
    nc = sum(len(t['clips']) for t in tracks)
    end = max([c['at'] + clip_duration(c) for t in tracks for c in t['clips']] or [0])
    if len(canonicalize(doc)) > MAX_DOC_BYTES or nv > 4 or na > 4 or nc > 2000 or end > MAX_DURATION_TICKS:
        add('VT-02', '/tracks', 'DOC_LIMIT')

    # VT-03
    seen: set[str] = set()

    def chk(i: Any, p: str) -> None:
        if not isinstance(i, str) or not _ID.match(i) or i in seen:
            add('VT-03', p, 'DOC_ID_DUPLICATE')
        if isinstance(i, str):
            seen.add(i)

    for ti, t in enumerate(tracks):
        chk(t['id'], f'/tracks/{ti}/id')
        for ci, c in enumerate(t['clips']):
            chk(c['id'], f'/tracks/{ti}/clips/{ci}/id')
            for ei, e in enumerate(c['effects']):
                chk(e['id'], f'/tracks/{ti}/clips/{ci}/effects/{ei}/id')
    for mi, m in enumerate(doc['markers']):
        chk(m['id'], f'/markers/{mi}/id')

    # VT-04
    for ti, t in enumerate(tracks):
        for ci in range(1, len(t['clips'])):
            a, b = t['clips'][ci - 1], t['clips'][ci]
            if b['at'] < a['at'] or a['at'] + clip_duration(a) > b['at']:
                add('VT-04', f'/tracks/{ti}/clips/{ci}/at', 'CLIP_OVERLAP')

    # VT-05
    for ti, t in enumerate(tracks):
        for ci, c in enumerate(t['clips']):
            if c['source']['kind'] == 'photo':
                continue
            p = f'/tracks/{ti}/clips/{ci}'
            if c['src_in'] < 0:
                add('VT-05', p + '/src_in', 'CLIP_RANGE')
            elif c['src_out'] <= c['src_in']:
                add('VT-05', p + '/src_out', 'CLIP_RANGE')
            else:
                sd = ctx['sourceDurations'].get(c['source'].get('media_id'))
                if sd is not None and c['src_out'] > sd:
                    add('VT-05', p + '/src_out', 'CLIP_RANGE')

    # VT-06
    for ti, t in enumerate(tracks):
        for ci, c in enumerate(t['clips']):
            s = c['source']
            sid = s.get('media_id') or s.get('asset_version_id')
            p = f'/tracks/{ti}/clips/{ci}/source'
            if ctx['sourceStates'].get(sid) != 'ready':
                add('VT-06', p, 'SOURCE_NOT_READY')
            elif ctx['sourceHashes'].get(sid) != s.get('sha256'):
                add('VT-06', p, 'SOURCE_HASH_MISMATCH')

    # VT-07
    by_id = {t['id']: t for t in tracks}
    ft = frame_ticks(f['num'], f['den'])
    for ti, t in enumerate(tracks):
        for ci, c in enumerate(t['clips']):
            p = f'/tracks/{ti}/clips/{ci}'

            def bad(sub: str) -> None:
                add('VT-07', f'{p}/{sub}', 'EFFECT_PARAM')

            if c.get('fit') is not None and c['fit'] not in ('fit', 'fill', 'none'):
                bad('fit')
            tf = c.get('transform')
            if tf is not None:
                if not _in(tf.get('scale'), 0.1, 8):
                    bad('transform/scale')
                if not _vec(tf.get('position'), 2, -1, 1):
                    bad('transform/position')
                if not _in(tf.get('rotate'), -180, 180):
                    bad('transform/rotate')
                if not _vec(tf.get('crop'), 4, 0, 0.45):
                    bad('transform/crop')
            au = c.get('audio')
            if au is not None:
                if not _in(au.get('gain_db'), -60, 24):
                    bad('audio/gain_db')
                for k in ('fade_in', 'fade_out'):
                    if not _is_int(au.get(k)) or au[k] < 0 or au[k] % ft:
                        bad('audio/' + k)
                if _is_int(au.get('fade_in')) and _is_int(au.get('fade_out')) and au['fade_in'] + au['fade_out'] > clip_duration(c):
                    bad('audio/fade_out')
                if not isinstance(au.get('mute'), bool):
                    bad('audio/mute')
            m = c.get('motion')
            if m is not None:
                if m.get('kind') != 'pan_zoom':
                    bad('motion/kind')
                if m.get('easing') not in ('linear', 'ease_in_out'):
                    bad('motion/easing')
                for k in ('from', 'to'):
                    pose = m.get(k)
                    if not isinstance(pose, dict) or not _in(pose.get('scale'), 1, 4) or not _vec(pose.get('center'), 2, 0, 1):
                        bad('motion/' + k)
            if c['source']['kind'] == 'photo' and (not _is_int(c.get('duration')) or c['duration'] < ft or c['duration'] % ft):
                bad('duration')
            for ei, e in enumerate(c['effects']):
                ep = f'{p}/effects/{ei}/params'
                params = e.get('params') if isinstance(e.get('params'), dict) else {}

                def badp(k: str) -> None:
                    add('VT-07', f'{ep}/{_esc(k)}', 'EFFECT_PARAM')

                et = e['type']
                if et == 'color.basic':
                    for k, v in params.items():
                        r = _BASIC.get(k)
                        if r is None or not _in(v, *r):
                            badp(k)
                elif et == 'color.cdl':
                    spec = {'slope': (3, 0, 4), 'offset': (3, -1, 1), 'power': (3, 0.1, 4), 'saturation': (0, 0, 4)}
                    for k in params:
                        if k not in spec:
                            badp(k)
                    for k, (n, lo, hi) in spec.items():
                        if k not in params or not (_vec(params[k], n, lo, hi) if n else _in(params[k], lo, hi)):
                            badp(k)
                elif et == 'color.curves':
                    if not params:
                        badp('')
                    for k, v in params.items():
                        ok = (k in ('master', 'r', 'g', 'b') and isinstance(v, list) and 2 <= len(v) <= 16
                              and all(_vec(q, 2, 0, 1) for q in v) and all(v[i][0] > v[i - 1][0] for i in range(1, len(v))))
                        if not ok:
                            badp(k)
                elif et == 'color.lut3d':
                    lut = params.get('lut')
                    if not isinstance(lut, dict) or lut.get('kind') != 'lut' or not isinstance(lut.get('media_id'), str) or not isinstance(lut.get('sha256'), str):
                        badp('lut')
                    if not _in(params.get('intensity'), 0, 1):
                        badp('intensity')
                    for k in params:
                        if k not in ('lut', 'intensity'):
                            badp(k)
                elif et == 'audio.gain':
                    if not _in(params.get('gain_db'), -60, 24):
                        badp('gain_db')
                    for k in params:
                        if k != 'gain_db':
                            badp(k)
                elif et == 'audio.fade':
                    for k in ('fade_in', 'fade_out'):
                        if not _is_int(params.get(k)) or params[k] < 0:
                            badp(k)
                    for k in params:
                        if k not in ('fade_in', 'fade_out'):
                            badp(k)
                elif et == 'audio.duck':
                    sc = params.get('sidechain_tracks')
                    if not isinstance(sc, list) or not 1 <= len(sc) <= 4 or len(set(sc)) != len(sc):
                        badp('sidechain_tracks')
                    for k, lo, hi in (('threshold_db', -60, 0), ('depth_db', 0, 40), ('attack_ms', 1, 1000), ('release_ms', 10, 5000)):
                        if not _in(params.get(k), lo, hi):
                            badp(k)
                    for k in params:
                        if k not in ('sidechain_tracks', 'threshold_db', 'depth_db', 'attack_ms', 'release_ms'):
                            badp(k)

    # VT-08
    for ti, t in enumerate(tracks):
        for ci, c in enumerate(t['clips']):
            tr = c.get('transition_in')
            if not tr:
                continue
            p = f'/tracks/{ti}/clips/{ci}/transition_in'
            if ci == 0 and tr['kind'] not in ('fade_in', 'cut'):
                add('VT-08', p + '/kind', 'TRANSITION_INVALID')
                continue
            lim = clip_duration(c) // 2 if ci == 0 else min(clip_duration(c), clip_duration(t['clips'][ci - 1])) // 2
            if tr.get('duration', 0) > lim:
                add('VT-08', p + '/duration', 'TRANSITION_INVALID')

    # VT-09
    for ti, t in enumerate(tracks):
        for ci, c in enumerate(t['clips']):
            for ei, e in enumerate(c['effects']):
                sc = e['params'].get('sidechain_tracks') if e['type'] == 'audio.duck' and isinstance(e.get('params'), dict) else None
                if isinstance(sc, list) and any(s not in by_id or by_id[s]['kind'] != 'audio' or s == t['id'] for s in sc):
                    add('VT-09', f'/tracks/{ti}/clips/{ci}/effects/{ei}/params/sidechain_tracks', 'SIDECHAIN_INVALID')

    # VT-10
    if ctx.get('forRender') and ctx.get('musicClipIds') and doc['rights'].get('music_declared') is not True:
        add('VT-10', '/rights/music_declared', 'RIGHTS_REQUIRED')
    return out
