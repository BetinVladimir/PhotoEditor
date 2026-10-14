"""Conservative combined donor resources pending target hardware qualification."""
from photo_worker.runtime.executor import OperationFailure


def donor_portrait_resources(source_width,source_height,donor_width,donor_height,
                             patch_width,patch_height):
    dims=(source_width,source_height,donor_width,donor_height,patch_width,patch_height)
    if any(type(n) is not int or n<1 for n in dims):
        raise ValueError('DONOR_DIMENSIONS_INVALID')
    source=source_width*source_height;donor=donor_width*donor_height
    patch=patch_width*patch_height
    if (source>200_000_000 or donor>200_000_000 or patch>1_048_576 or
            patch_width>source_width or patch_height>source_height):
        raise ValueError('DONOR_DIMENSIONS_INVALID')
    # Keep mapped-page/full-frame RAM headroom until measured qualification;
    # disk-backed mappings alone do not prove a smaller peak resident footprint.
    mask=8*source+8192
    material=20*(source+donor)+24576  # RGB, original alpha, normalized legacy alpha, headers
    output=16*source+32768
    return {'source_pixel_count':source,'donor_pixel_count':donor,'patch_pixel_count':patch,
        'ram_bytes':400*(source+donor+patch)+512*1024*1024,
        'mask_scratch_bytes':mask,'material_scratch_bytes':material,
        'output_scratch_bytes':output,'scratch_bytes':mask+material+output+64*1024*1024,
        'output_bytes':16*source+32*1024*1024,'vram_bytes':0}


def admit_donor_portrait(profile,*dimensions):
    if not isinstance(profile,dict) or profile.get('renderer_version')!='v2.2' or profile.get('backend')!='cpu':
        raise OperationFailure('CONFIG_INVALID')
    required=donor_portrait_resources(*dimensions)
    estimate=profile.get('resource_estimate')
    if (not isinstance(estimate,dict) or any(type(estimate.get(k)) is not int or
            not 0<=estimate[k]<=2**53-1 for k in required)):
        raise OperationFailure('CONFIG_INVALID')
    for key in ('source_pixel_count','donor_pixel_count','patch_pixel_count','vram_bytes'):
        if estimate[key]!=required[key]:raise OperationFailure('CONFIG_INVALID')
    for key,code in (('ram_bytes','RAM_ADMISSION_EXCEEDED'),
            ('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),
            ('mask_scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),
            ('material_scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),
            ('output_scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),
            ('output_bytes','OUTPUT_ADMISSION_EXCEEDED')):
        if estimate[key]<required[key]:raise OperationFailure(code)
    return required
