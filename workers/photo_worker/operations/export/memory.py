"""Conservative admission for current frame-buffer presentation codecs."""
from photo_worker.runtime.executor import OperationFailure


def presentation_ram_bytes(source_width,source_height,width,height,*,alpha):
    dimensions=(source_width,source_height,width,height)
    if any(type(value) is not int or value<1 for value in dimensions):
        raise ValueError('PRESENTATION_DIMENSIONS_INVALID')
    source=source_width*source_height;target=width*height
    if max(source,target)>200_000_000:raise ValueError('PRESENTATION_PIXEL_LIMIT')
    # Source mapping/transfer/premult buffers and output resample/ICC/codec
    # buffers may overlap; alpha paths reserve extra room for transfer math.
    return source*(96 if alpha else 24)+target*96+128*1024*1024


def admit_presentation(profile,source_width,source_height,width,height,*,alpha):
    required=presentation_ram_bytes(source_width,source_height,width,height,alpha=alpha)
    estimate=profile.get('resource_estimate')
    if estimate is None and profile.get('renderer_version')!='v2.2':
        # Existing small-image profiles retain a bounded compatibility budget.
        budget=512*1024*1024
    elif isinstance(estimate,dict) and type(estimate.get('ram_bytes')) is int and 0<estimate['ram_bytes']<=2**53-1:
        budget=estimate['ram_bytes']
    else:raise OperationFailure('CONFIG_INVALID')
    if required>budget:raise OperationFailure('RAM_ADMISSION_EXCEEDED')
    return required
