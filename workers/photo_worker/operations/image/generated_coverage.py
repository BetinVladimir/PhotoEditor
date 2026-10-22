"""Publish the exact operation selection as immutable binary generated coverage."""
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import MaskRef


def stage_generated_coverage(artifacts, image, selection, check_active):
    h, w = image.canvas.height, image.canvas.width
    if not isinstance(selection, np.ndarray) or selection.dtype != np.bool_ or selection.shape != (h,w):
        raise ValueError('GENERATED_COVERAGE_INVALID')
    with artifacts.scratch.reserve(4*h*w+8192) as owner:
        path = owner.path('generated-coverage', '.npy')
        output = np.lib.format.open_memmap(path, mode='w+', dtype='<f4', shape=(h,w))
        try:
            for y in range(0,h,256):
                check_active(); output[y:y+256] = selection[y:y+256]
            output.flush(); owner.validate_file(path); check_active()
            pixels = artifacts.stage(path, owner, 'generated_pixels', 'application/octet-stream')
            size = path.stat().st_size
        finally: output._mmap.close()
    mask = MaskRef(artifact=pixels, canvas=image.canvas, transform_chain=image.transform_chain,
        object_id=None, selector='generated', confidence=1, model_lock_id=None, protected=False)
    reference = artifacts.stage_contract(mask, 'generated_mask')
    return pixels, reference, size+len(canonical_bytes(mask))
