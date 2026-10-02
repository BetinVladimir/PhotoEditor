"""Conservative admitted budgets for the current tiled CPU lens implementation."""
from photo_worker.runtime.executor import OperationFailure


def lens_resources(width,height,*,strength):
    if any(type(value) is not int or value<1 for value in (width,height)) or width*height>200_000_000:
        raise ValueError('LENS_DIMENSIONS_INVALID')
    if type(strength) not in (int,float) or not 0<=strength<=1:
        raise ValueError('LENS_PARAMS_INVALID')
    pixels=width*height
    # Includes mapped source/output, normalized opacity and gamma temporary;
    # count mapped resident pages conservatively rather than assuming eviction.
    # Fixed headroom covers 512px float64 map/resample temporaries and JSON/index.
    return dict(pixel_count=pixels,ram_bytes=60*pixels+512*1024*1024,
                scratch_bytes=60*pixels+64*1024*1024,
                output_bytes=(4 if strength==0 else 28)*pixels+32*1024*1024,
                vram_bytes=0)


def admit_lens(profile,width,height,*,strength):
    required=lens_resources(width,height,strength=strength)
    estimate=profile.get('resource_estimate')
    if not isinstance(estimate,dict):raise OperationFailure('CONFIG_INVALID')
    for name in required:
        if type(estimate.get(name)) is not int or estimate[name]<0:
            raise OperationFailure('CONFIG_INVALID')
    if estimate['pixel_count']!=required['pixel_count'] or estimate['vram_bytes']!=0:
        raise OperationFailure('CONFIG_INVALID')
    for name,code in [('ram_bytes','RAM_ADMISSION_EXCEEDED'),('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),('output_bytes','OUTPUT_ADMISSION_EXCEEDED')]:
        if estimate[name]<required[name]:raise OperationFailure(code)
    return required
