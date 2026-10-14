"""Conservative admission for V28 preparation.

Preparation uses disk-backed tiles. The original conservative full-frame RAM
ceiling is retained until measured tile peak-memory qualification is available.
"""
from photo_worker.runtime.executor import OperationFailure


def local_portrait_resources(width, height):
    if any(type(n) is not int or n < 1 for n in (width, height)) or width * height > 200_000_000:
        raise ValueError('PORTRAIT_DIMENSIONS_INVALID')
    pixels = width * height
    # Includes float64 RGB/chroma/gamut/compose intermediates, Gaussian planes,
    # float32 proposal/encoded output, masks, alpha and resident mapped pages.
    # OpenCV workspace and serialization receive fixed additional headroom.
    return {'pixel_count': pixels, 'ram_bytes': 400*pixels + 512*1024*1024,
            'scratch_bytes': 64*pixels + 64*1024*1024,
            'output_bytes': 16*pixels + 32*1024*1024, 'vram_bytes': 0}


def admit_local_portrait(profile, width, height):
    if profile.get('renderer_version') != 'v2.2' or profile.get('backend') != 'cpu':
        raise OperationFailure('CONFIG_INVALID')
    required = local_portrait_resources(width, height)
    estimate = profile.get('resource_estimate')
    if not isinstance(estimate, dict) or any(type(estimate.get(k)) is not int or estimate[k] < 0 for k in required):
        raise OperationFailure('CONFIG_INVALID')
    if estimate['pixel_count'] != required['pixel_count'] or estimate['vram_bytes'] != 0:
        raise OperationFailure('CONFIG_INVALID')
    for key, code in [('ram_bytes','RAM_ADMISSION_EXCEEDED'),
                      ('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),
                      ('output_bytes','OUTPUT_ADMISSION_EXCEEDED')]:
        if estimate[key] < required[key]:
            raise OperationFailure(code)
    return required
