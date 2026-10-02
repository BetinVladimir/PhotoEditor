"""Source preparation budget only; complete renderer adds operator/model costs."""
from photo_worker.runtime.executor import OperationFailure


def region_source_resources(width, height, *, has_alpha, alpha_mode):
    if (any(type(n) is not int or n<1 for n in (width,height))
            or width*height>200_000_000 or type(has_alpha) is not bool
            or alpha_mode not in ('require_float32','normalize_uint16')):
        raise ValueError('REGION_SOURCE_RESOURCES_INVALID')
    pixels=width*height
    # Strict NPY headers are bounded by 4096 for each live materialized input.
    inputs=12*pixels+4096+(4*pixels+4096 if has_alpha else 0)
    normalization=4*pixels+4096 if has_alpha and alpha_mode=='normalize_uint16' else 0
    # Count mapped pages, full-width row validation and normalization tile temps.
    rows=min(height,512)*width
    tile=min(width,512)*min(height,512)
    return dict(pixel_count=pixels,ram_bytes=inputs+normalization+12*rows+16*tile+16*1024*1024,
                scratch_bytes=inputs+normalization,normalization_bytes=normalization)


def admit_region_source(profile,width,height,*,has_alpha,alpha_mode):
    required=region_source_resources(width,height,has_alpha=has_alpha,alpha_mode=alpha_mode)
    estimate=profile.get('resource_estimate') if isinstance(profile,dict) else None
    if (not isinstance(estimate,dict) or any(type(estimate.get(k)) is not int
            or not 0<=estimate[k]<=2**53-1 for k in ('pixel_count','ram_bytes','scratch_bytes'))
            or estimate['pixel_count']!=required['pixel_count']):
        raise OperationFailure('CONFIG_INVALID')
    for key,code in [('ram_bytes','RAM_ADMISSION_EXCEEDED'),('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED')]:
        if estimate[key]<required[key]:raise OperationFailure(code)
    return required
