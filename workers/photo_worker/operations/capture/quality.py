"""Versioned technical quality for confirmed shot-list coverage."""

from .contracts import CapturePolicy, QAReport

METRIC_VERSION = 'capture.technical_quality/1'


def technical_quality(report: QAReport, policy: CapturePolicy) -> float | None:
    """Return conservative normalized quality, or None for incomplete evidence.

    Focus and clipping are mandatory foundations. Other requested imaging
    checks participate; duplicate and shot-list checks describe context rather
    than image quality. No unavailable score is replaced by a clean score.
    """
    requested = set(policy.checks)
    actual = {check.code for check in report.checks}
    if actual != requested:
        raise ValueError('CAPTURE_QUALITY_CHECK_COVERAGE')
    foundations = {code: 0 for code in ('focus', 'clipping')}
    scores: list[float] = []
    incomplete = False
    observed: set[tuple[str, object]] = set()
    for check in report.checks:
        if check.code not in {'focus', 'clipping', 'eyes', 'glare'}:
            continue
        key = (check.code, check.face_id)
        if key in observed:
            raise ValueError('CAPTURE_QUALITY_DUPLICATE_CHECK')
        observed.add(key)
        if check.face_id is None and check.code in foundations:
            foundations[check.code] += 1
        if check.status == 'unavailable' or check.score is None:
            incomplete = True
            continue
        scores.append(1.0 - check.score if check.code in {'clipping', 'glare'} else check.score)
    if not {'focus', 'clipping'} <= requested or incomplete or any(count != 1 for count in foundations.values()) or not scores:
        return None
    return min(scores)
