"""Shot-boundary post-processing (FN-C-01)."""

from typing import Any

THRESHOLDS = {1: 0.8, 2: 0.65, 3: 0.5, 4: 0.4, 5: 0.3}
DEFAULT_PARAMS = {'sensitivity': 3, 'min_shot_frames': 8, 'mode': 'all', 'include_flash': False}


def _runs(flags: list[bool]) -> list[tuple[int, int]]:
    out, start = [], None
    for i, f in enumerate(flags):
        if f and start is None:
            start = i
        elif not f and start is not None:
            out.append((start, i - 1))
            start = None
    if start is not None:
        out.append((start, len(flags) - 1))
    return out


def postprocess_boundaries(single: list[float], many: list[float], luma: list[float],
                           params: dict[str, Any] | None, frame_ticks: int) -> list[dict[str, Any]]:
    p = {**DEFAULT_PARAMS, **(params or {})}
    thr = THRESHOLDS[p['sensitivity']]
    n = len(single)
    flashes = [i for i in range(1, n - 1)
               if luma[i] - luma[i - 1] > 60 and luma[i] - luma[i + 1] > 60 and abs(luma[i - 1] - luma[i + 1]) < 20]
    cand: list[dict[str, Any]] = []
    for a, b in _runs([v >= thr for v in single]):
        k = max(range(a, b + 1), key=lambda i: (single[i], -i))  # first maximum
        cand.append({'kind': 'cut', 'frame_in': k + 1, 'frame_out': k + 1, 'confidence': single[k]})
    if p['mode'] == 'all':
        for a, b in _runs([v >= thr for v in many]):
            if b - a + 1 < 3:
                continue
            cand = [c for c in cand if not (c['kind'] == 'cut' and a <= c['frame_in'] <= b + 1)]
            cand.append({'kind': 'dissolve', 'frame_in': a, 'frame_out': b + 1, 'confidence': max(many[a:b + 1])})
        for a, b in _runs([v <= 24 for v in luma]):
            if b - a + 1 < 2:
                continue
            j = a
            while j > 0 and luma[j - 1] > luma[j]:
                j -= 1
            if a - j >= 5 and luma[j] >= 64:
                cand.append({'kind': 'fade_out', 'frame_in': j, 'frame_out': a, 'confidence': 0.9})
            k = b
            while k < n - 1 and luma[k + 1] > luma[k]:
                k += 1
            if k - b >= 5 and luma[k] >= 64:
                cand.append({'kind': 'fade_in', 'frame_in': b, 'frame_out': k, 'confidence': 0.9})
    flash_set = set(flashes)
    cand = [c for c in cand if not (c['kind'] == 'cut' and (c['frame_in'] in flash_set or c['frame_in'] - 1 in flash_set))]
    if p['mode'] == 'all' and p['include_flash']:
        cand += [{'kind': 'flash', 'frame_in': i, 'frame_out': i + 1, 'confidence': 0.8} for i in flashes]
    cand.sort(key=lambda c: (c['frame_in'], -c['confidence'], c['kind']))
    kept: list[dict[str, Any]] = []
    for c in cand:
        if kept and c['frame_in'] - kept[-1]['frame_in'] < p['min_shot_frames']:
            if c['confidence'] > kept[-1]['confidence']:
                kept[-1] = c
        else:
            kept.append(c)
    return [{'id': f'b{i}', 'kind': c['kind'], 'frame_in': c['frame_in'], 'frame_out': c['frame_out'],
             'start_ticks': c['frame_in'] * frame_ticks, 'end_ticks': c['frame_out'] * frame_ticks,
             'confidence': round(c['confidence'], 4)} for i, c in enumerate(kept, 1)]
