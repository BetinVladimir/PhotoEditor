"""Owned existing V2 color stages for a materialized authored base style."""
from contextlib import contextmanager
import numpy as np
from photo_worker.operations.learning.color_render import render_materialized_style
from .render_adjustments import AdjustmentPixels
from .render_modules import required_render_modules

COLOR_MODULES = frozenset({'color','strength','adaptive','local','region_adjustments'})


@contextmanager
def prepare_color_base(artifacts, inputs, prepared_style, original, *, person_reliable,
                       features=None, coverage_counts=None, person_mask=None,
                       region_masks=None, read_protected=None, tile_size=512, check_active=lambda: None):
    """Preserve existing color math; never stand in for unsupported effects.

    Caller independently qualifies runtime/models and verifies feature/mask
    evidence. The materialized effective style already includes scalar edits.
    The output owner stays live until composition/publication finishes.
    """
    check_active()
    style=prepared_style.effective
    if not required_render_modules(style) <= COLOR_MODULES:
        raise ValueError('RENDER_COLOR_EFFECT_PIPELINE_REQUIRED')
    if ((inputs.recipe.recipe.protected_mask is not None and read_protected is None)
            or read_protected is not None and not callable(read_protected)):
        raise ValueError("RENDER_COLOR_PROTECTION_REQUIRED")
    rgb=original.rgb
    if (not isinstance(rgb,np.ndarray) or rgb.dtype!=np.float32
            or rgb.shape!=(inputs.source.canvas.height,inputs.source.canvas.width,3)
            or rgb.shape[0]*rgb.shape[1]>200000000):
        raise ValueError('RENDER_COLOR_SOURCE_INVALID')
    with artifacts.scratch.reserve(rgb.size*4+8192) as owner:
        path=owner.path('render-color','.npy')
        target=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=rgb.shape)
        try:
            render_materialized_style(rgb,target,style,person_reliable=person_reliable,
                features=features,coverage_counts=coverage_counts,person_mask=person_mask,
                region_masks=region_masks,tile=tile_size,check_active=check_active)
            if read_protected is not None:
                for y in range(0,rgb.shape[0],tile_size):
                    for x in range(0,rgb.shape[1],tile_size):
                        check_active()
                        height,width=min(tile_size,rgb.shape[0]-y),min(tile_size,rgb.shape[1]-x)
                        mask=read_protected(x,y,width,height)
                        if (not isinstance(mask,np.ndarray) or mask.dtype!=np.float32
                                or mask.shape!=(height,width) or not np.isfinite(mask).all()
                                or np.any(mask<0) or np.any(mask>1)):
                            raise ValueError("RENDER_COLOR_PROTECTION_INVALID")
                        part=np.s_[y:y+height,x:x+width]
                        protected=mask>0
                        target[part][protected]=rgb[part][protected]
            target.flush();owner.validate_file(path);check_active()
            target.flags.writeable=False
            yield AdjustmentPixels(target,original.alpha,original.generated)
            check_active()
        finally:target._mmap.close()


def color_provider_peak(width, height, style, *, tile_size=512):
    """Conservative color-provider peak; actual qualification remains required.

    Includes owned RGB, resident LUT conversion/blend, masks and tile temporaries.
    Values supplement shared composition costs and never imply GPU qualification.
    """
    if (any(type(v) is not int or v<1 for v in (width,height)) or width*height>200000000
            or type(tile_size) is not int or tile_size not in (256,512,1024)
            or not required_render_modules(style)<=COLOR_MODULES):
        raise ValueError('RENDER_COLOR_RESOURCES_INVALID')
    pixels=width*height;tile=min(width,tile_size)*min(height,tile_size)
    prototypes=style.adaptation.prototypes if style.adaptation is not None else ()
    lut_bytes=17**3*3*4
    if prototypes:
        size=prototypes[0].absolute_lut.size
        lut_bytes+=(len(prototypes)+3)*size**3*3*4
    mask_count=len(style.extensions.region_adjustments)+(1 if style.local.enabled or prototypes else 0)
    return dict(ram_bytes=(12+4*mask_count)*pixels+lut_bytes+1024*tile+128*1024*1024,
        scratch_bytes=12*pixels+8192,vram_bytes=0,output_bytes=0)
