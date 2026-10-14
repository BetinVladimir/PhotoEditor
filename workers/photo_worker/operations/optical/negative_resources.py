"""Conservative complete negative attempt budgets pending measured qualification."""


def negative_resources(width,height,*,has_alpha):
    if any(type(n) is not int or n<1 for n in (width,height)) or width*height>200_000_000 or type(has_alpha) is not bool:
        raise ValueError('NEGATIVE_DIMENSIONS_INVALID')
    pixels=width*height
    material=(12+(4 if has_alpha else 0))*pixels+32768
    prepared=16*pixels+32768
    # Resident mapped pages, retained qualification JSON/typed copies, Params,
    # native tile temporaries and scheduler/reader bookkeeping. This is a
    # conservative admission floor, not measured peak RSS or scientific QA.
    return dict(pixel_count=pixels,vram_bytes=0,
        ram_bytes=(28+(4 if has_alpha else 0))*pixels+128*min(pixels,512*512)+512*1024*1024,
        material_scratch_bytes=material,output_scratch_bytes=prepared,
        scratch_bytes=material+prepared,output_bytes=16*pixels+32768+4*65536)



def admit_negative(profile,width,height,*,has_alpha):
    from photo_worker.runtime.executor import OperationFailure
    if not isinstance(profile,dict) or profile.get('renderer_version')!='v2.2' or profile.get('backend')!='cpu':
        raise OperationFailure('CONFIG_INVALID')
    required=negative_resources(width,height,has_alpha=has_alpha)
    estimate=profile.get('resource_estimate')
    if not isinstance(estimate,dict) or any(type(estimate.get(key)) is not int or not 0<=estimate[key]<=2**53-1 for key in required):
        raise OperationFailure('CONFIG_INVALID')
    if any(estimate[key]!=required[key] for key in ('pixel_count','vram_bytes')):
        raise OperationFailure('CONFIG_INVALID')
    for key,code in [('ram_bytes','RAM_ADMISSION_EXCEEDED'),('material_scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),
            ('output_scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),
            ('output_bytes','OUTPUT_ADMISSION_EXCEEDED')]:
        if estimate[key]<required[key]:raise OperationFailure(code)
    if estimate['scratch_bytes']<estimate['material_scratch_bytes']+estimate['output_scratch_bytes']:
        raise OperationFailure('SCRATCH_ADMISSION_EXCEEDED')
    return dict(estimate)
