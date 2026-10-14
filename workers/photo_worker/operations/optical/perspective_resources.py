"""Conservative bounded CPU budgets for perspective preparation/publication."""
from photo_worker.runtime.executor import OperationFailure

SAFE_INTEGER=2**53-1


def perspective_resources(source_width,source_height,output_width,output_height):
    dimensions=(source_width,source_height,output_width,output_height)
    if any(type(value) is not int or value<1 for value in dimensions):
        raise ValueError('PERSPECTIVE_LIMIT_INVALID')
    source=source_width*source_height;output=output_width*output_height
    if max(source,output)>200000000:raise ValueError('PERSPECTIVE_PIXEL_LIMIT')
    # Source RGB/alpha plus transfer temporary; RGB/alpha/changed output.
    # Include mapped pages conservatively and tiled float64 interpolation plus
    # JSON/index headroom. Input downloads remain live until staging completes.
    return dict(pixel_count=output,ram_bytes=32*source+20*output+512*1024*1024,
        scratch_bytes=32*source+20*output+64*1024*1024,
        output_bytes=20*output+32*1024*1024,vram_bytes=0)


def admit_perspective(profile,source_width,source_height,output_width,output_height):
    required=perspective_resources(source_width,source_height,output_width,output_height)
    if not isinstance(profile,dict) or profile.get('backend')!='cpu' or profile.get('renderer_version')!='v2.2':
        raise OperationFailure('CONFIG_INVALID')
    estimate=profile.get('resource_estimate')
    if not isinstance(estimate,dict):raise OperationFailure('CONFIG_INVALID')
    if any(type(estimate.get(name)) is not int or not 0<=estimate[name]<=SAFE_INTEGER for name in required):
        raise OperationFailure('CONFIG_INVALID')
    if estimate['pixel_count']!=required['pixel_count'] or estimate['vram_bytes']!=0:
        raise OperationFailure('CONFIG_INVALID')
    for name,code in [('ram_bytes','RAM_ADMISSION_EXCEEDED'),('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),('output_bytes','OUTPUT_ADMISSION_EXCEEDED')]:
        if estimate[name]<required[name]:raise OperationFailure(code)
    return required
