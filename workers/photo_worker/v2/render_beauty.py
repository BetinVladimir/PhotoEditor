"""Scoped existing multi-face beauty stage; retain measured face evidence."""
from contextlib import contextmanager
from dataclasses import dataclass
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.style import Beauty
from photo_worker.operations.portrait.prepare import prepare_beauty, FaceInput, FaceEvidence
from photo_worker.runtime.arrays import read_array
from .render_adjustments import AdjustmentPixels


@dataclass(frozen=True)
class RenderBeautyStage:
    pixels: AdjustmentPixels
    faces: tuple[FaceEvidence, ...]
    review_required: bool


@contextmanager
def prepare_beauty_stage(artifacts, source, current, params, faces, *, permanent=None,
                         tile_size=512, max_roi_pixels=4194304, check_active=lambda: None):
    """Caller qualifies model/runtime and supplies verified effective face inputs.

    Existing working color, source geometry, ROI and texture checks are retained.
    No face/skipped-face evidence is turned into a fabricated passing report.
    Alpha and generated coverage retain their original scoped owners.
    """
    check_active()
    if (not isinstance(params, Beauty) or not isinstance(faces, tuple)
            or any(not isinstance(face, FaceInput) for face in faces)):
        raise ValueError('RENDER_BEAUTY_INPUT_INVALID')
    params=Beauty.model_validate_json(canonical_bytes(params))
    with prepare_beauty(current.rgb,source,faces,params,artifacts.scratch,
            permanent=permanent,tile=tile_size,max_roi_pixels=max_roi_pixels,
            check_active=check_active) as files:
        with read_array(files.image,(source.canvas.height,source.canvas.width,3),
                'float32',check_active) as rgb:
            check_active()
            review=not faces or any(not face.applied and face.reason!='zero_strength' for face in files.faces)
            yield RenderBeautyStage(AdjustmentPixels(rgb,current.alpha,current.generated),files.faces,review)
            check_active()


def render_beauty_params(prepared_style):
    """Derive beauty intensity from authored materialization exactly once."""
    from photo_worker.algorithms.effective import effective_beauty_params
    return effective_beauty_params(prepared_style.effective.beauty, prepared_style.effective.strength)
