"""Retouch proposal arrays only; no confirmed mask or approved edit invented."""
from dataclasses import dataclass
import numpy as np
from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput
from .preview_binding import (bind_retouch_preview_documents,
    bind_retouch_preview_geometry, bind_retouch_preview_protection)
from .preview_loader import load_proposal_preview_arrays


@dataclass(frozen=True)
class RetouchPreviewArrays:
    original: np.ndarray
    mask: np.ndarray
    protected: np.ndarray
    loader_peak_bytes: int
    retained_loader_bytes: int


def load_retouch_preview_arrays(artifacts,params,*,verify_admission,verify_origin,
        verify_color,max_bytes,check_active):
    # Three mapped inputs plus owned RGB/mask/protection and bounded scan buffers.
    return load_proposal_preview_arrays(artifacts,params,input_model=SemanticRetouchPreviewInput,
        bind_documents=bind_retouch_preview_documents,bind_geometry=bind_retouch_preview_geometry,
        bind_protection=bind_retouch_preview_protection,
        estimate_peak=lambda shape:64*shape[0]*shape[1]+64*1048576,array_type=RetouchPreviewArrays,
        verify_admission=verify_admission,verify_origin=verify_origin,verify_color=verify_color,
        max_bytes=max_bytes,check_active=check_active)


def load_retouch_preview_alpha(artifacts,params,source_raw,*,verify_admission,max_bytes,check_active):
    from .preview_loader import _load_preview_alpha
    from .preview_binding import bind_retouch_preview_source
    return _load_preview_alpha(artifacts,params,source_raw,input_model=SemanticRetouchPreviewInput,
        bind_source=bind_retouch_preview_source,verify_admission=verify_admission,
        max_bytes=max_bytes,check_active=check_active)
