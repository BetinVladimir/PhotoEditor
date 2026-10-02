"""Recompute pinned V2 person mask and Stats20 for a learning holdout."""
from __future__ import annotations

from contextlib import contextmanager
from dataclasses import dataclass
from typing import Callable, Iterator

import cv2
import numpy as np

from photo_worker.algorithms.mask_refine import refine_tiles
from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.algorithms.statistics import (
    compute_stats20_bounded, coverage_counts_bounded,
)
from photo_worker.models.pool import ModelPool
from photo_worker.operations.mask.person import MEAN, STD, _preview, _probability
from photo_worker.runtime.scratch import ScratchSpace


@dataclass(frozen=True)
class HoldoutPersonStats:
    mask: np.memmap
    features: np.ndarray
    coverage_counts: np.ndarray
    confidence: float
    reliable: bool


@contextmanager
def open_holdout_person_stats(source: np.ndarray, alpha: np.ndarray | None,
                              pool: ModelPool, backend: str,
                              model_lock_sha: str, scratch: ScratchSpace,
                              check_active: Callable[[], None],
                              ) -> Iterator[HoldoutPersonStats]:
    """Mirror mask.person and color.stats without publishing intermediate refs."""
    if (pool.lock.lock_id != model_lock_sha or backend != 'cuda' or
            'lraspp_mobilenet_v3_large' not in
            {entry.id for entry in pool.lock.models} or
            not isinstance(source, np.ndarray) or source.ndim != 3 or
            source.shape[2] != 3 or source.dtype != np.float32 or
            (alpha is not None and (alpha.shape != source.shape[:2] or
                                    alpha.dtype not in (np.dtype('uint16'),np.dtype('float32'))))):
        raise ValueError('HOLDOUT_PERSON_INPUT_INVALID')
    height, width = source.shape[:2]
    if alpha is not None and alpha.dtype==np.float32:
        for y in range(0,height,512):
            for x in range(0,width,512):
                check_active();block=alpha[y:y+512,x:x+512]
                if not np.isfinite(block).all() or np.any((block<0)|(block>1)):
                    raise ValueError('HOLDOUT_PERSON_INPUT_INVALID')
    pixels = height * width
    check_active()
    srgb, _ = work_to_model(source)
    preview = _preview(srgb, 768)
    ph, pw = preview.shape[:2]
    with pool.acquire('lraspp_mobilenet_v3_large', backend) as engine:
        logits = engine.infer((preview - MEAN) / STD)
    small, confidence = _probability(logits, ph, pw)
    raw = cv2.resize(small, (width, height),
                     interpolation=cv2.INTER_LINEAR).astype(np.float32)
    if alpha is not None:
        raw[alpha == 0] = 0
    with scratch.reserve(pixels * 12 + 16384) as reservation:
        mask_path = reservation.path('holdout-person', '.npy')
        luma_path = reservation.path('holdout-luma', '.npy')
        mask = np.lib.format.open_memmap(mask_path, mode='w+', dtype='<f4',
                                         shape=(height, width))
        luma = np.lib.format.open_memmap(luma_path, mode='w+', dtype='<f8',
                                         shape=(pixels,))
        try:
            for tile in refine_tiles(raw, source, tile_size=512,
                                     check_active=check_active):
                mask[tile.y:tile.y + tile.values.shape[0],
                     tile.x:tile.x + tile.values.shape[1]] = tile.values
            if alpha is not None:
                mask[alpha == 0] = 0
            stats = compute_stats20_bounded(
                source, mask, luma, alpha=alpha, encoding='gamma24',
                check_active=check_active)
            counts = coverage_counts_bounded(
                source, alpha=alpha, encoding='gamma24',
                check_active=check_active)
            mask.flush()
            reservation.validate()
            reliable = confidence >= .75 and stats.values[15] >= .01
            yield HoldoutPersonStats(mask, stats.values, counts,
                                     confidence, reliable)
            check_active()
        finally:
            mask._mmap.close()
            luma._mmap.close()
