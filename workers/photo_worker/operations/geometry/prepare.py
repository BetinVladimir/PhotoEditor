"""Native linear-light crop preparation shared by standalone and render stages."""
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path
import cv2
import numpy as np
from photo_worker.contracts.images import Canvas, TransformChain
from photo_worker.runtime.scratch import Reservation
from photo_worker.algorithms.color import to_encoded, to_linear


@dataclass(frozen=True)
class PreparedCrop:
    rgb: Path
    alpha: Path | None
    reservation: Reservation


@contextmanager
def prepare_crop(rgb, alpha, plan, scratch, *, check_active=lambda: None):
    """Own cropped NPY files through staging; preserve existing native math.

    Caller proves plan/source lineage and admits native full-frame memory.
    Alpha is the standalone WorkingImage uint16 representation.
    """
    check_active()
    h, w = plan.source_height, plan.source_width
    oh, ow = plan.output_height, plan.output_width
    if (plan.disposition == 'outpaint' or rgb.shape != (h, w, 3)
            or rgb.dtype != np.float32 or ow * oh > 200000000
            or alpha is not None and (alpha.shape != (h, w) or alpha.dtype != np.uint16)):
        raise ValueError('CROP_SOURCE_INVALID')
    matrix = np.asarray(plan.forward_matrix, np.float64).reshape(3, 3)
    reserve = ow * oh * (12 + (2 if alpha is not None else 0)) + 65536
    with scratch.reserve(reserve) as reservation:
        path = reservation.path('crop-rgb', '.npy')
        out = np.lib.format.open_memmap(path, mode='w+', dtype='<f4', shape=(oh, ow, 3))
        try:
            check_active()
            out[:] = to_encoded(cv2.warpPerspective(to_linear(np.asarray(rgb)), matrix,
                (ow, oh), flags=cv2.INTER_LANCZOS4, borderMode=cv2.BORDER_REFLECT_101)).clip(0, 1)
            check_active()
            out.flush()
        finally:
            out._mmap.close()
        alpha_path = None
        if alpha is not None:
            alpha_path = reservation.path('crop-alpha', '.npy')
            aout = np.lib.format.open_memmap(alpha_path, mode='w+', dtype='<u2', shape=(oh, ow))
            try:
                check_active()
                aout[:] = np.rint(np.clip(cv2.warpPerspective(np.asarray(alpha, np.float32),
                    matrix, (ow, oh), flags=cv2.INTER_LANCZOS4,
                    borderMode=cv2.BORDER_REFLECT_101), 0, 65535)).astype(np.uint16)
                check_active()
                aout.flush()
            finally:
                aout._mmap.close()
        reservation.validate()
        check_active()
        yield PreparedCrop(path, alpha_path, reservation)
        check_active()


def crop_geometry(image_ref, image, plan_ref, plan):
    """Bind a crop plan to source metadata and derive deterministic output space."""
    from photo_worker.contracts.base import canonical_hash
    from photo_worker.contracts.images import Canvas, MatrixTransform, Rect, TransformChain
    if (plan.image != image_ref or plan.source_width != image.canvas.width
            or plan.source_height != image.canvas.height or plan.disposition == 'outpaint'
            or plan.output_width * plan.output_height > 200000000):
        raise ValueError('CROP_PLAN_SOURCE_INVALID')
    canvas = Canvas(width=plan.output_width, height=plan.output_height, orientation=1,
        space_id='crop:' + canonical_hash({'plan': plan_ref.model_dump(mode='json')})[:32],
        source_sha256=image.canvas.source_sha256)
    step = MatrixTransform(kind='matrix', from_canvas=image.canvas, to_canvas=canvas,
        invertibility='invertible', valid_region=Rect(x=0, y=0, width=canvas.width,
        height=canvas.height), forward_matrix=plan.forward_matrix)
    return canvas, TransformChain.create([*image.transform_chain.steps, step])


@dataclass(frozen=True)
class CropStage:
    rgb: np.ndarray
    alpha: np.ndarray | None
    canvas: Canvas
    transform_chain: TransformChain
    review_required: bool
    review_reasons: tuple[str, ...]


@contextmanager
def prepare_crop_stage(image_ref, image, plan_ref, plan, rgb, alpha, scratch, *,
                       check_active=lambda: None):
    """Expose owned native pixels and derived geometry to a pipeline context.

    Input refs, decoded documents and mapped arrays must be independently
    verified by the caller. Returned arrays remain valid only in this context.
    """
    from photo_worker.runtime.arrays import read_array
    from contextlib import ExitStack
    check_active()
    canvas, chain = crop_geometry(image_ref, image, plan_ref, plan)
    if (image.dtype != 'float32' or image.color.profile_id != 'work_rec2020_gamma24_v1'
            or (image.alpha is None) != (alpha is None)):
        raise ValueError('CROP_SOURCE_INVALID')
    with prepare_crop(rgb, alpha, plan, scratch, check_active=check_active) as prepared:
        with ExitStack() as stack:
            result = stack.enter_context(read_array(prepared.rgb,
                (canvas.height, canvas.width, 3), 'float32', check_active, unit_interval=True))
            result_alpha = None if prepared.alpha is None else stack.enter_context(read_array(
                prepared.alpha, (canvas.height, canvas.width), 'uint16', check_active))
            check_active()
            yield CropStage(result, result_alpha, canvas, chain,
                plan.disposition == 'review', tuple(plan.reasons))
            check_active()


def crop_provider_peak(image_ref, image, plan_ref, plan):
    """Conservative full-frame native costs for installer resource admission.

    Count input/output mapped pages, simultaneous linear-light conversion,
    native warp and encoding temporaries, plus alpha conversion/rounding.
    This is a static bound, not a measured device qualification.
    """
    canvas, _ = crop_geometry(image_ref, image, plan_ref, plan)
    source_pixels = image.canvas.width * image.canvas.height
    output_pixels = canvas.width * canvas.height
    if source_pixels > 200000000:
        raise ValueError('CROP_SOURCE_PIXEL_LIMIT')
    alpha = image.alpha is not None
    peak = dict(ram_bytes=(64 + (8 if alpha else 0)) * source_pixels
        + (96 + (24 if alpha else 0)) * output_pixels + 128 * 1024 * 1024,
        scratch_bytes=(12 + (2 if alpha else 0)) * output_pixels + 65536,
        vram_bytes=0, output_bytes=0)
    if any(value > 2**53-1 for value in peak.values()):
        raise ValueError('CROP_RESOURCE_OVERFLOW')
    return peak
