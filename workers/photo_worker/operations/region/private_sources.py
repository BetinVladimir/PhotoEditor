"""Separate exact adopted replay from measured fresh attempt execution."""
import numpy as np
from .private_frames import PrivateStageInput


def require_stage_source(operation,source_ref,rgb,alpha,original,original_alpha,*,private_input=None,check_active):
    if rgb.dtype!=np.float32 or rgb.shape!=original.shape:
        raise ValueError('REGION_NATIVE_SOURCE_INVALID')
    if private_input is not None:
        if type(private_input) is not PrivateStageInput:
            raise ValueError('REGION_PRIVATE_STAGE_INPUT_INVALID')
        # This proves fresh attempt ancestry, never equality to adopted bytes.
        # The exact original source/policy was independently admitted and read.
        private_input.require(operation,source_ref,rgb,alpha)
        check_active();return
    # Existing exact replay semantics remain unchanged for every ordinary caller.
    for y in range(0,rgb.shape[0],256):
        check_active()
        if not np.array_equal(rgb[y:y+256].view(np.uint32),original[y:y+256].view(np.uint32)):
            raise ValueError('REGION_NATIVE_SOURCE_INVALID')
    if original_alpha is None:
        if alpha is not None:raise ValueError('REGION_NATIVE_ALPHA_INVALID')
    else:
        from photo_worker.v2.render_defects import alpha_tile_matches
        if alpha is None or not alpha_tile_matches(alpha,original_alpha):
            raise ValueError('REGION_NATIVE_ALPHA_INVALID')
    check_active()
