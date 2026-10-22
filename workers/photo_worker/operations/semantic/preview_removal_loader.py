"""Removal proposal arrays only; no confirmed mask or approved edit invented."""
from dataclasses import dataclass
import numpy as np
from photo_worker.contracts.semantic_preview import SemanticRemovalPreviewInput
from .preview_binding import (bind_removal_preview_documents,
    bind_removal_preview_geometry, bind_removal_preview_protection)
from .preview_loader import load_proposal_preview_arrays


@dataclass(frozen=True)
class RemovalPreviewArrays:
    original: np.ndarray
    mask: np.ndarray
    protected: np.ndarray
    loader_peak_bytes: int
    retained_loader_bytes: int


def load_removal_preview_arrays(artifacts,params,*,verify_admission,verify_origin,
        verify_color,max_bytes,check_active):
    # Three mapped inputs plus owned RGB/mask/protection and bounded scan buffers.
    return load_proposal_preview_arrays(artifacts,params,input_model=SemanticRemovalPreviewInput,
        bind_documents=bind_removal_preview_documents,bind_geometry=bind_removal_preview_geometry,
        bind_protection=bind_removal_preview_protection,
        estimate_peak=lambda shape:64*shape[0]*shape[1]+64*1048576,array_type=RemovalPreviewArrays,
        verify_admission=verify_admission,verify_origin=verify_origin,verify_color=verify_color,
        max_bytes=max_bytes,check_active=check_active)
