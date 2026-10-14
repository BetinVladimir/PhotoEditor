"""Bounded capture proxy preparation without changing source artifacts."""
from dataclasses import dataclass
from collections.abc import Callable

import cv2
import numpy as np

from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.images import Canvas, MatrixTransform, Rect, TransformChain, WorkingImage


@dataclass(frozen=True)
class CaptureProxyPixels:
    rgb: np.ndarray
    alpha: np.ndarray | None
    width: int
    height: int


def prepare_proxy_pixels(source: WorkingImage, rgb: np.ndarray,
                         alpha: np.ndarray | None = None,
                         check_active: Callable[[], None] = lambda: None) -> CaptureProxyPixels:
    h, w = source.canvas.height, source.canvas.width
    if (source.dtype != 'float32' or source.color.profile_id != 'work_rec2020_gamma24_v1'
            or h * w > 200_000_000 or rgb.dtype != np.float32
            or rgb.shape != (h, w, 3) or (source.alpha is None) != (alpha is None)):
        raise ValueError('CAPTURE_SOURCE_INVALID')
    if alpha is not None and (alpha.dtype not in (np.dtype('uint16'),np.dtype('float32')) or alpha.shape != (h, w)):
        raise ValueError('CAPTURE_ALPHA_INVALID')
    if alpha is not None and alpha.dtype==np.float32:
        for top in range(0,h,512):
            for left in range(0,w,512):
                check_active();block=alpha[top:top+512,left:left+512]
                if not np.isfinite(block).all() or np.any((block<0)|(block>1)):
                    raise ValueError('CAPTURE_ALPHA_INVALID')
    # Scan mapped sources in bounded blocks; do not allocate a full-frame
    # boolean copy for a 200 MP original.
    if not rgb.flags.c_contiguous:
        raise ValueError('CAPTURE_SOURCE_LAYOUT_INVALID')
    flat = rgb.reshape(-1)
    for start in range(0, flat.size, 262144):
        check_active()
        if not np.isfinite(flat[start:start + 262144]).all():
            raise ValueError('CAPTURE_SOURCE_NONFINITE')
    scale = min(1.0, 2048 / max(h, w))
    width, height = max(1, round(w * scale)), max(1, round(h * scale))
    check_active()
    # INTER_AREA is an average, so it introduces no ringing and retains values
    # outside [0,1]. Never clamp before clipping assessment.
    if (width, height) == (w, h):
        pixels = rgb.copy()
        opacity = None if alpha is None else alpha.copy()
    else:
        pixels = cv2.resize(rgb, (width, height), interpolation=cv2.INTER_AREA)
        opacity = None if alpha is None else cv2.resize(alpha, (width, height), interpolation=cv2.INTER_AREA)
    check_active()
    return CaptureProxyPixels(pixels, opacity, width, height)


def bind_proxy(source: WorkingImage, prepared: CaptureProxyPixels,
               pixels: ArtifactRef, alpha: ArtifactRef | None) -> WorkingImage:
    if ((source.alpha is None) != (prepared.alpha is None)
            or (prepared.alpha is None) != (alpha is None)
            or prepared.rgb.dtype != np.float32
            or prepared.rgb.shape != (prepared.height, prepared.width, 3)
            or min(prepared.width, prepared.height) < 1
            or max(prepared.width, prepared.height) > 2048
            or not prepared.rgb.flags.c_contiguous):
        raise ValueError('CAPTURE_PROXY_INVALID')
    flat = prepared.rgb.reshape(-1)
    for start in range(0, flat.size, 262144):
        if not np.isfinite(flat[start:start + 262144]).all():
            raise ValueError('CAPTURE_PROXY_NONFINITE')
    if prepared.alpha is not None:
        opacity=prepared.alpha
        if (opacity.shape!=(prepared.height,prepared.width) or
                opacity.dtype not in (np.dtype('uint16'),np.dtype('float32')) or
                opacity.dtype==np.float32 and (not np.isfinite(opacity).all() or np.any((opacity<0)|(opacity>1)))):
            raise ValueError('CAPTURE_ALPHA_INVALID')
    scale = min(1.0, 2048 / max(source.canvas.width, source.canvas.height))
    if (prepared.width, prepared.height) != (max(1, round(source.canvas.width * scale)),
                                            max(1, round(source.canvas.height * scale))):
        raise ValueError('CAPTURE_PROXY_SIZE_INVALID')
    canvas = source.canvas
    steps = list(source.transform_chain.steps)
    if (prepared.width, prepared.height) != (canvas.width, canvas.height):
        canvas = Canvas(width=prepared.width, height=prepared.height, orientation=1,
                        space_id=source.canvas.space_id, source_sha256=source.canvas.source_sha256)
        sx, sy = canvas.width / source.canvas.width, canvas.height / source.canvas.height
        steps.append(MatrixTransform(kind='matrix', from_canvas=source.canvas, to_canvas=canvas,
                     invertibility='invertible', valid_region=Rect(x=0, y=0, width=canvas.width, height=canvas.height),
                     forward_matrix=[sx, 0, (sx - 1) / 2, 0, sy, (sy - 1) / 2, 0, 0, 1]))
    return WorkingImage(pixels=pixels, alpha=alpha, canvas=canvas, color=source.color,
                        dtype='float32', transform_chain=TransformChain.create(steps))


@dataclass(frozen=True)
class PublishedCaptureProxy:
    image: WorkingImage
    image_ref: ArtifactRef
    outputs: tuple[ArtifactRef, ...]
    bytes_written: int


def publish_proxy(artifacts, source: WorkingImage, prepared: CaptureProxyPixels,
                  check_active: Callable[[], None]) -> PublishedCaptureProxy:
    """Stage all child arrays before their manifest through fenced gateway IO."""
    from photo_worker.contracts.base import canonical_bytes
    # Validate geometry before any upload. Placeholder refs have the same
    # structural constraints as the eventual gateway response.
    bind_proxy(source, prepared, source.pixels, source.alpha)
    outputs = []
    written = 0
    refs = {}
    total = prepared.width * prepared.height * (12+(prepared.alpha.dtype.itemsize if prepared.alpha is not None else 0)) + 8192
    with artifacts.scratch.reserve(total) as reservation:
        for name, array, dtype, role in (
                ('rgb', prepared.rgb, '<f4', 'working_rgb'),
                ('alpha', prepared.alpha, '<f4' if prepared.alpha is not None and prepared.alpha.dtype==np.float32 else '<u2', 'working_alpha')):
            if array is None:
                continue
            check_active()
            path = reservation.path('capture-' + name, '.npy')
            mapped = np.lib.format.open_memmap(path, mode='w+', dtype=dtype, shape=array.shape)
            try:
                mapped[:] = array
                mapped.flush()
            finally:
                mapped._mmap.close()
            check_active()
            refs[name] = artifacts.stage(path, reservation, role, 'application/octet-stream')
            outputs.append(refs[name])
            written += path.stat().st_size
    image = bind_proxy(source, prepared, refs['rgb'], refs.get('alpha'))
    check_active()
    ref = artifacts.stage_contract(image, 'working_image')
    outputs.append(ref)
    written += len(canonical_bytes(image))
    return PublishedCaptureProxy(image, ref, tuple(outputs), written)


def capture_model_preview(work: np.ndarray, alpha: np.ndarray | None = None) -> np.ndarray:
    """Models consume bounded sRGB; QA continues to use the untouched proxy."""
    from photo_worker.algorithms.model_delta import work_to_model
    if (work.dtype != np.float32 or work.ndim != 3 or work.shape[2] != 3
            or work.shape[0] * work.shape[1] > 2048 * 2048 or not np.isfinite(work).all()):
        raise ValueError('CAPTURE_MODEL_INPUT_INVALID')
    srgb, _ = work_to_model(np.clip(work, 0, 1))
    if alpha is not None:
        if alpha.dtype not in (np.dtype('uint16'),np.dtype('float32')) or alpha.shape != work.shape[:2]:
            raise ValueError('CAPTURE_ALPHA_INVALID')
        if alpha.dtype==np.float32 and (not np.isfinite(alpha).all() or np.any((alpha<0)|(alpha>1))):
            raise ValueError('CAPTURE_ALPHA_INVALID')
        srgb = np.where(alpha[..., None] > 0, srgb, 0).astype(np.float32)
    return srgb


def prepare_face_observations(source: WorkingImage, prepared: CaptureProxyPixels,
                              binding, calibration, pool, backend: str, check_active):
    """Reuse pinned SFD/FAN geometry without inferring or assigning identities."""
    from photo_worker.contracts.base import canonical_hash
    from photo_worker.models.install import ModelError
    from photo_worker.operations.session.describe import _face_observations
    if (canonical_hash(source.canvas) != binding.canvas_hash or
            not calibration.qualified or calibration.model_lock_sha != pool.lock.lock_id):
        raise ValueError('CAPTURE_PREPARATION_BINDING_INVALID')
    bind_proxy(source, prepared, source.pixels, source.alpha)
    entries = {entry.id: entry for entry in pool.lock.models}
    for model in ('sfd', 'fan2d'):
        entry = entries.get(model)
        if (entry is None or not entry.approved_for_install or backend not in entry.backends
                or entry.backends[backend].status != 'passed'):
            raise ModelError('MODEL_UNAVAILABLE')
    check_active()
    srgb = capture_model_preview(prepared.rgb, prepared.alpha)
    check_active()
    return _face_observations(srgb, binding, pool, backend,
                              calibration.min_face_edge_px, check_active)


@dataclass(frozen=True)
class PublishedCaptureInputs:
    proxy: PublishedCaptureProxy
    faces_ref: ArtifactRef
    outputs: tuple[ArtifactRef, ...]
    bytes_written: int


def prepare_capture_inputs(ctx, artifacts, binding, calibration, pool,
                           backend: str) -> PublishedCaptureInputs:
    """Prepare proxy and observations from declared, immutable source inputs."""
    from contextlib import ExitStack
    from photo_worker.contracts.base import canonical_bytes, canonical_hash
    from photo_worker.runtime.arrays import read_array, read_alpha_array
    source = artifacts.contract(binding.source, WorkingImage)
    if canonical_hash(source.canvas) != binding.canvas_hash:
        raise ValueError('CAPTURE_SOURCE_BINDING_INVALID')
    with ExitStack() as stack:
        local = stack.enter_context(artifacts.input(source.pixels, suffix='.npy'))
        work = stack.enter_context(read_array(local.path,
            (source.canvas.height, source.canvas.width, 3), 'float32',
            ctx.check_active, unit_interval=False))
        alpha = None
        if source.alpha is not None:
            local_alpha = stack.enter_context(artifacts.input(source.alpha, suffix='.npy'))
            alpha = stack.enter_context(read_alpha_array(local_alpha.path,
                (source.canvas.height, source.canvas.width), ctx.check_active))
        prepared = prepare_proxy_pixels(source, work, alpha, ctx.check_active)
    # Validate/infer before uploading anything. Failed model qualification
    # cannot leave a seemingly complete preparation result.
    faces = prepare_face_observations(source, prepared, binding, calibration,
                                      pool, backend, ctx.check_active)
    proxy = publish_proxy(artifacts, source, prepared, ctx.check_active)
    ctx.check_active()
    faces_ref = artifacts.stage_contract(faces, 'faces')
    return PublishedCaptureInputs(proxy, faces_ref, (*proxy.outputs, faces_ref),
                                  proxy.bytes_written + len(canonical_bytes(faces)))
