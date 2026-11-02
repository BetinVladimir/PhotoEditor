"""Boundary scoring (FN-C-03…05). Test infrastructure; not used on product paths."""

from typing import Any

_HARD = {'cut'}
_SOFT = {'dissolve', 'fade_in', 'fade_out'}


def iou(a: tuple[float, float], b: tuple[float, float]) -> float:
    inter = max(0, min(a[1], b[1]) - max(a[0], b[0]))
    union = max(a[1], b[1]) - min(a[0], b[0])
    return inter / union if union > 0 else 0


def _score_group(pred: list[dict[str, Any]], gt: list[dict[str, Any]], hard: bool) -> tuple[int, int, int]:
    pairs = []
    for pi, p in enumerate(pred):
        for gi, g in enumerate(gt):
            if hard:
                d = abs(p['frame_in'] - g['frame_in'])
                if d <= 2:
                    pairs.append((d, pi, gi))
            else:
                v = iou((p['frame_in'], p['frame_out']), (g['frame_in'], g['frame_out']))
                if v >= 0.3:
                    pairs.append((1 - v, pi, gi))
    pairs.sort()
    used_p: set[int] = set()
    used_g: set[int] = set()
    tp = 0
    for _, pi, gi in pairs:
        if pi not in used_p and gi not in used_g:
            used_p.add(pi)
            used_g.add(gi)
            tp += 1
    return tp, len(pred) - tp, len(gt) - tp


def score_boundaries(pred: list[dict[str, Any]], gt: list[dict[str, Any]]) -> dict[str, int]:
    tp = fp = fn = 0
    for kinds, hard in ((_HARD, True), (_SOFT, False)):
        a, b, c = _score_group([x for x in pred if x['kind'] in kinds], [x for x in gt if x['kind'] in kinds], hard)
        tp, fp, fn = tp + a, fp + b, fn + c
    return {'tp': tp, 'fp': fp, 'fn': fn}


def prf(tp: int, fp: int, fn: int) -> dict[str, float]:
    if tp + fp + fn == 0:
        return {'precision': 1.0, 'recall': 1.0, 'f1': 1.0}
    p = tp / (tp + fp) if tp + fp else 0.0
    r = tp / (tp + fn) if tp + fn else 0.0
    f = 2 * p * r / (p + r) if p + r else 0.0
    return {'precision': round(p, 4), 'recall': round(r, 4), 'f1': round(f, 4)}
