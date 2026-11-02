"""Timeline time arithmetic (FN-T-01…04) and canonical form (FN-T-11)."""

from typing import Any, Literal

from photo_worker.contracts.base import canonical_bytes, canonical_hash

TICKS_PER_SECOND = 705_600_000


def frame_ticks(num: int, den: int) -> int:
    if not (isinstance(num, int) and isinstance(den, int)) or isinstance(num, bool) or isinstance(den, bool) or num <= 0 or den <= 0:
        raise ValueError('FPS_UNSUPPORTED')
    v = TICKS_PER_SECOND * den
    if v % num:
        raise ValueError('FPS_UNSUPPORTED')
    return v // num


def frames_to_ticks(num: int, den: int, frame: int) -> int:
    if not isinstance(frame, int) or isinstance(frame, bool) or frame < 0:
        raise ValueError('FRAME_NEGATIVE')
    return frame * frame_ticks(num, den)


def ticks_to_frame(num: int, den: int, ticks: int, mode: Literal['floor', 'round']) -> int:
    if not isinstance(ticks, int) or isinstance(ticks, bool) or ticks < 0:
        raise ValueError('TICKS_NEGATIVE')
    ft = frame_ticks(num, den)
    return ticks // ft if mode == 'floor' else (ticks + ft // 2) // ft


def clip_duration(clip: dict[str, Any]) -> int:
    if clip['source']['kind'] == 'photo':
        return clip['duration']
    speed = clip.get('speed') or {'num': 1, 'den': 1}
    return ((clip['src_out'] - clip['src_in']) * speed['den']) // speed['num']


def canonicalize(doc: Any) -> bytes:
    return canonical_bytes(doc)


def document_sha256(doc: Any) -> str:
    return canonical_hash(doc)
