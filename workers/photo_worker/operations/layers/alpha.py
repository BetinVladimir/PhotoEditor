"""Explicit, disk-bounded legacy alpha conversion for composition adapters."""
from contextlib import contextmanager
from dataclasses import dataclass
import numpy as np
from photo_worker.operations.optical.alpha import normalized_alpha


@dataclass(frozen=True)
class LayerAlpha:
    values: np.ndarray | None
    converted: bool
    # Converted representation requires a new float32 artifact even if opacity
    # was unchanged; the legacy uint16 reference cannot label these bytes.
    requires_new_reference: bool


@contextmanager
def layer_alpha(alpha,shape,scratch,*,mode,check_active=lambda:None):
    """Caller explicitly declares conversion; kernel only receives float32.

    Missing alpha remains absent. Existing float32 is validated and borrowed.
    The mapped result must not escape this context. Publication and provenance
    require a compiler-declared conversion node and are not inferred here.
    """
    check_active()
    if mode not in ('require_float32','normalize_uint16'):
        raise ValueError('LAYER_ALPHA_MODE_INVALID')
    if alpha is None:
        yield LayerAlpha(None,False,False)
        return
    if not isinstance(alpha,np.ndarray):raise ValueError('LAYER_ALPHA_INVALID')
    converted=alpha.dtype==np.uint16
    if converted and mode!='normalize_uint16':raise ValueError('LAYER_ALPHA_CONVERSION_REQUIRED')
    with normalized_alpha(alpha,shape,scratch,tile=512,check_active=check_active) as values:
        yield LayerAlpha(values,converted,converted)
