"""Numerical Big-LaMa helpers shared by confirmed edits and proposal previews.

These helpers grant no approval or model qualification; callers own admission.
"""
from __future__ import annotations

import cv2
import numpy as np
from photo_worker.algorithms.model_delta import transfer_model_delta, work_to_model
from photo_worker.runtime.executor import OperationFailure


def infer_inpaint_candidate(engine, rgb, mask, max_padded_pixels):
    h, w = rgb.shape[:2]; ph = (-h) % 8; pw = (-w) % 8
    if (type(max_padded_pixels) is not int or max_padded_pixels < 1
            or (h + ph) * (w + pw) > max_padded_pixels):
        raise OperationFailure("RESOURCE_OOM")
    image = np.pad(rgb, ((0, ph), (0, pw), (0, 0)), mode="reflect")
    binary = np.pad(mask.astype(np.float32), ((0, ph), (0, pw)), mode="constant")
    result = np.asarray(engine.infer(image, binary), np.float32)
    if result.shape != image.shape or not np.isfinite(result).all() or np.any(result < 0) or np.any(result > 1):
        raise ValueError("MODEL_OUTPUT_INVALID")
    return result[:h, :w]


def inpaint_mask_roi(mask, context_pixels):
    """Find the selected bounding box without allocating per-pixel coordinates."""
    if (mask.dtype != np.bool_ or mask.ndim != 2 or mask.size == 0
            or type(context_pixels) is not int or context_pixels < 0):
        raise ValueError("INPAINT_MASK_INVALID")
    rows = np.any(mask, axis=1)
    cols = np.any(mask, axis=0)
    if not np.any(rows):
        raise ValueError("INPAINT_MASK_EMPTY")
    h, w = mask.shape
    y0 = max(0, int(np.argmax(rows)) - context_pixels)
    y1 = min(h, h - int(np.argmax(rows[::-1])) + context_pixels)
    x0 = max(0, int(np.argmax(cols)) - context_pixels)
    x1 = min(w, w - int(np.argmax(cols[::-1])) + context_pixels)
    return x0, y0, x1, y1




def compute_inpaint_region(engine, source, effective, protected, roi, *,
        feather_pixels, max_padded_pixels, check_active):
    """Return private restored ROI and blend weights using the production recipe.

    Caller admits frame/model memory and qualification before entering; no
    publication or approval is conferred. Cancellation is checked around each
    model call and before returning numerical output.
    """
    if not callable(check_active):
        raise ValueError("INPAINT_CANCELLATION_REQUIRED")
    check_active()
    if (type(feather_pixels) is not int or feather_pixels < 1
            or not isinstance(source, np.ndarray) or source.dtype != np.float32
            or source.ndim != 3 or source.shape[2] != 3
            or any(not isinstance(m, np.ndarray) or m.dtype != np.bool_
                   or m.shape != source.shape[:2] for m in (effective, protected))
            or len(roi) != 4 or any(type(v) is not int for v in roi)):
        raise ValueError("INPAINT_REGION_INVALID")
    x0, y0, x1, y1 = roi
    h, w = source.shape[:2]
    if not (0 <= x0 < x1 <= w and 0 <= y0 < y1 <= h):
        raise ValueError("INPAINT_REGION_INVALID")
    # Enforce padding admission before model-input allocation as well as infer.
    rh, rw = y1-y0, x1-x0
    if (type(max_padded_pixels) is not int or max_padded_pixels < 1
            or (rh+(-rh)%8)*(rw+(-rw)%8) > max_padded_pixels):
        raise OperationFailure("RESOURCE_OOM")
    roi_work = np.asarray(source[y0:y1, x0:x1], np.float32)
    roi_mask = effective[y0:y1, x0:x1]
    global_pass = max(rw, rh) > 1024
    model_input, _ = work_to_model(roi_work)
    if global_pass:
        scale = 1024 / max(h, w)
        sw, sh = max(8, int(round(w*scale))), max(8, int(round(h*scale)))
        preview_work = cv2.resize(np.asarray(source), (sw, sh), interpolation=cv2.INTER_AREA)
        preview_mask = cv2.resize(effective.astype(np.uint8), (sw, sh), interpolation=cv2.INTER_NEAREST) > 0
        preview_input, _ = work_to_model(preview_work)
        check_active()
        preview_result = infer_inpaint_candidate(engine, preview_input, preview_mask, max_padded_pixels)
        check_active()
        global_result = cv2.resize(preview_result, (w, h), interpolation=cv2.INTER_CUBIC)
        provisional = np.clip(global_result[y0:y1, x0:x1], 0, 1).astype(np.float32)
        model_input = np.where(roi_mask[..., None], provisional, model_input)
    check_active()
    model_output = infer_inpaint_candidate(engine, model_input, roi_mask, max_padded_pixels)
    check_active()
    restored, _ = transfer_model_delta(roi_work, model_input, model_output, 1.)
    distance = cv2.distanceTransform(roi_mask.astype(np.uint8), cv2.DIST_L2, 3)
    weight = np.minimum(distance / feather_pixels, 1).astype(np.float32)
    weight[protected[y0:y1, x0:x1]] = 0
    check_active()
    return restored, weight, global_pass
