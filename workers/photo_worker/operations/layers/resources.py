"""Conservative budgets for same-geometry CPU layer composition."""
from photo_worker.runtime.executor import OperationFailure


def layer_resources(width,height,*,mask_refs:int,mask_nodes:int,tile_size:int=512,alpha_mode:str="require_float32"):
    if (any(type(n) is not int or n<1 for n in (width,height)) or width*height>200_000_000 or
            type(mask_refs) is not int or not 1<=mask_refs<=257 or
            type(mask_nodes) is not int or not 1<=mask_nodes<=256 or
            type(tile_size) is not int or tile_size not in (256,512,1024) or
            alpha_mode not in ('require_float32','normalize_uint16')):
        raise ValueError('LAYER_RESOURCES_INVALID')
    pixels=width*height;tile=min(width,tile_size)*min(height,tile_size)
    # Count full mapped pages conservatively: two RGB/alpha parents, two binary
    # parent coverages, probability/protection leaves and all output mappings.
    # Mask evaluation releases its node cache before composition allocates tiles.
    mapped=(68+4*mask_refs)*pixels
    # Both parents may need a simultaneously live float32 normalization map.
    if alpha_mode=="normalize_uint16":mapped+=8*pixels+8192
    tile_bytes=max(256,4*(mask_nodes+3))*tile
    return dict(pixel_count=pixels,ram_bytes=mapped+tile_bytes+256*1024*1024,
        scratch_bytes=mapped+64*1024*1024,output_bytes=24*pixels+32*1024*1024,vram_bytes=0)


def layer_tile_bytes(width,height,*,mask_nodes:int,tile_size:int=512):
    layer_resources(width,height,mask_refs=1,mask_nodes=mask_nodes,tile_size=tile_size)
    return max(256,4*(mask_nodes+3))*min(width,tile_size)*min(height,tile_size)


def admit_layer(profile,width,height,*,mask_refs:int,mask_nodes:int,tile_size:int=512):
    if not isinstance(profile,dict):raise OperationFailure('CONFIG_INVALID')
    mode=profile.get("layer_alpha_mode","require_float32")
    if mode not in ("require_float32","normalize_uint16"):raise OperationFailure("CONFIG_INVALID")
    required=layer_resources(width,height,mask_refs=mask_refs,mask_nodes=mask_nodes,tile_size=tile_size,alpha_mode=mode)
    estimate=profile.get('resource_estimate')
    if not isinstance(estimate,dict):raise OperationFailure('CONFIG_INVALID')
    if any(type(estimate.get(name)) is not int or estimate[name]<0 for name in required):
        raise OperationFailure('CONFIG_INVALID')
    if estimate['pixel_count']!=required['pixel_count'] or estimate['vram_bytes']!=0:
        raise OperationFailure('CONFIG_INVALID')
    for name,code in (('ram_bytes','RAM_ADMISSION_EXCEEDED'),('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),('output_bytes','OUTPUT_ADMISSION_EXCEEDED')):
        if estimate[name]<required[name]:raise OperationFailure(code)
    return required
