"""Owned automatic line evidence from verified source bytes and arrays.

No output is staged and no homography is authorized here. The caller must prove
artifact ownership/version/SHA and install the producer before publication.
"""
from dataclasses import dataclass
from hashlib import sha256
import numpy as np
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.images import WorkingImage
from photo_worker.algorithms.perspective_lines import detect_perspective_lines
from photo_worker.runtime.executor import TaskCancelled

@dataclass(frozen=True)
class DetectedPerspectiveLines:
    source_sha256: str
    canvas_sha256: str
    lines: bytes | None
    lines_sha256: str | None


def prepare_detected_perspective_lines(source_bytes, expected_sha, rgb, alpha, *,
                                       working_icc_sha, check_active=lambda: None):
    if not callable(check_active):
        raise ValueError('PERSPECTIVE_LINE_ACTIVITY_INVALID')
    supplied_check_active=check_active
    def check_active():
        if supplied_check_active() is False:
            raise TaskCancelled('Perspective line task inactive')
    check_active()
    if (not isinstance(source_bytes, bytes) or not 2<=len(source_bytes)<=1048576 or
            sha256(source_bytes).hexdigest()!=expected_sha or
            not isinstance(working_icc_sha,str) or len(working_icc_sha)!=64 or
            any(c not in '0123456789abcdef' for c in working_icc_sha)):
        raise ValueError('PERSPECTIVE_LINE_SOURCE_INVALID')
    parse_json(source_bytes)
    source=WorkingImage.model_validate_json(source_bytes)
    if (canonical_bytes(source)!=source_bytes or source.dtype!='float32' or
            source.color.icc_sha256!=working_icc_sha):
        raise ValueError('PERSPECTIVE_LINE_SOURCE_INVALID')
    shape=(source.canvas.height,source.canvas.width)
    if (shape[0]*shape[1]>200000000 or not isinstance(rgb,np.ndarray) or
            rgb.shape!=(*shape,3) or rgb.dtype!=np.float32 or
            (source.alpha is None)!=(alpha is None)):
        raise ValueError('PERSPECTIVE_LINE_SOURCE_INVALID')
    if alpha is not None and (not isinstance(alpha,np.ndarray) or alpha.shape!=shape or
                             alpha.dtype not in (np.dtype('float32'),np.dtype('uint16'))):
        raise ValueError('PERSPECTIVE_LINE_ALPHA_INVALID')
    # Validate the full mmap in bounded tiles, including pixels proxy skips.
    for top in range(0,shape[0],512):
        for left in range(0,shape[1],512):
            check_active()
            if not np.isfinite(rgb[top:top+512,left:left+512]).all():
                raise ValueError('PERSPECTIVE_LINE_SOURCE_INVALID')
            if alpha is not None:
                block=alpha[top:top+512,left:left+512]
                if not np.isfinite(block).all() or alpha.dtype==np.float32 and (np.any(block<0) or np.any(block>1)):
                    raise ValueError('PERSPECTIVE_LINE_ALPHA_INVALID')
    detected=detect_perspective_lines(rgb,alpha,check_active=check_active)
    body=None if detected is None else canonical_bytes(detected)
    check_active()
    return DetectedPerspectiveLines(expected_sha,sha256(canonical_bytes(source.canvas)).hexdigest(),
                                    body,None if body is None else sha256(body).hexdigest())
