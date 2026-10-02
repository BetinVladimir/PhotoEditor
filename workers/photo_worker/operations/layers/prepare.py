"""Attempt-owned tiled adjustment-layer composition; no artifact publication."""
from contextlib import contextmanager,ExitStack
from dataclasses import dataclass
from pathlib import Path
import math
import numpy as np
from photo_worker.algorithms.layer_compose import compose_layer_tile
from photo_worker.algorithms.layer_lineage import generated_coverage
from photo_worker.contracts.images import ColorSpace
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.scratch import ScratchSpace,Reservation

@dataclass(frozen=True)
class LayerFiles:
    rgb:Path
    alpha:Path|None
    changed_mask:Path
    reservation:Reservation
    any_changed:bool
    rgb_changed:bool
    alpha_changed:bool
    generated_mask:Path|None=None
    source_generated_ref:ArtifactRef|None=None
    proposal_generated_ref:ArtifactRef|None=None
    alpha_converted:bool=False


@contextmanager
def prepare_layer(source,proposal,read_mask,scratch:ScratchSpace,*,color:ColorSpace,
                  opacity:float,blend:str,source_alpha=None,proposal_alpha=None,source_alpha_converted:bool=False,
                  read_protected=None,read_source_generated=None,read_proposal_generated=None,
                  source_generated_ref:ArtifactRef|None=None,proposal_generated_ref:ArtifactRef|None=None,tile_size:int=512,max_tile_bytes:int,
                  check_active=lambda:None):
    """Close/flush every mapping before yielding files for fenced staging.

    All arrays are borrowed immutable inputs. Semantic changes are independent
    of alpha representation conversion, which requires a new alpha reference.
    """
    check_active()
    if (not isinstance(source,np.ndarray) or source.dtype!=np.float32 or source.ndim!=3 or source.shape[-1]!=3 or
        not isinstance(proposal,np.ndarray) or proposal.dtype!=np.float32 or proposal.shape!=source.shape or
        min(source.shape[:2])<1 or source.shape[0]*source.shape[1]>200000000 or
        type(tile_size) is not int or tile_size not in (256,512,1024) or
        type(opacity) not in (int,float) or not math.isfinite(opacity) or not 0<=opacity<=1 or
        blend not in ('normal','multiply','screen') or not callable(read_mask) or
        any(reader is not None and not callable(reader) for reader in
            (read_protected,read_source_generated,read_proposal_generated)) or type(max_tile_bytes) is not int or
        max_tile_bytes<min(tile_size,source.shape[0])*min(tile_size,source.shape[1])*256):
        raise ValueError('LAYER_PREPARE_INVALID')
    for reference,reader in ((source_generated_ref,read_source_generated),(proposal_generated_ref,read_proposal_generated)):
        if reference is not None and (not isinstance(reference,ArtifactRef) or reader is None):
            raise ValueError('LAYER_GENERATED_BINDING_INVALID')
    if type(source_alpha_converted) is not bool or source_alpha_converted and source_alpha is None:
        raise ValueError("LAYER_ALPHA_CONVERSION_INVALID")
    h,w=source.shape[:2]
    for array in (source_alpha,proposal_alpha):
        if array is not None and (not isinstance(array,np.ndarray) or array.dtype!=np.float32 or array.shape!=(h,w)):
            raise ValueError('LAYER_ALPHA_INVALID')
    has_alpha=source_alpha is not None or proposal_alpha is not None
    has_generated=read_source_generated is not None or read_proposal_generated is not None
    with scratch.reserve(h*w*((20 if has_alpha else 16)+(4 if has_generated else 0))+8192) as reservation:
        rgb_path=reservation.path('layer-rgb','.npy');changed_path=reservation.path('layer-changed','.npy')
        alpha_path=reservation.path('layer-alpha','.npy') if has_alpha else None
        generated_path=reservation.path('layer-generated','.npy') if has_generated else None
        any_changed=False;rgb_changed=False;alpha_changed=False
        with ExitStack() as mappings:
            def allocate(path,shape):
                mapped=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=shape)
                mappings.callback(mapped._mmap.close);return mapped
            rgb=allocate(rgb_path,(h,w,3));changed=allocate(changed_path,(h,w))
            alpha=allocate(alpha_path,(h,w)) if has_alpha else None
            generated=allocate(generated_path,(h,w)) if has_generated else None
            for y in range(0,h,tile_size):
                for x in range(0,w,tile_size):
                    check_active();th,tw=min(tile_size,h-y),min(tile_size,w-x);part=np.s_[y:y+th,x:x+tw]
                    mask=read_mask(x,y,tw,th)
                    protected=read_protected(x,y,tw,th) if read_protected is not None else None
                    block=compose_layer_tile(source[part],proposal[part],mask,color=color,opacity=opacity,blend=blend,
                        source_alpha=source_alpha[part] if source_alpha is not None else None,
                        proposal_alpha=proposal_alpha[part] if proposal_alpha is not None else None,protected=protected,
                        max_tile_bytes=max_tile_bytes,check_active=check_active)
                    rgb[part]=block.rgb;changed[part]=block.changed
                    if alpha is not None:alpha[part]=1 if block.alpha is None else block.alpha
                    any_changed=any_changed or bool(np.any(block.changed))
                    rgb_changed=rgb_changed or bool(np.any(block.rgb.view(np.uint32)!=source[part].view(np.uint32)))
                    if block.alpha is not None:
                        original_alpha=np.ones((th,tw),np.float32) if source_alpha is None else source_alpha[part]
                        alpha_changed=alpha_changed or bool(np.any(block.alpha.view(np.uint32)!=original_alpha.view(np.uint32)))
                    del block
                    if generated is not None:
                        generated[part]=generated_coverage(mask,opacity=opacity,blend=blend,
                            source=read_source_generated(x,y,tw,th) if read_source_generated is not None else None,
                            proposal=read_proposal_generated(x,y,tw,th) if read_proposal_generated is not None else None,
                            protected=protected,max_tile_bytes=max_tile_bytes,check_active=check_active)
                    reservation.validate()
            for array in (rgb,changed,alpha,generated):
                if array is not None:array.flush()
            reservation.validate()
        check_active()
        for path in (rgb_path,changed_path,alpha_path,generated_path):
            if path is not None:reservation.validate_file(path)
        yield LayerFiles(rgb_path,alpha_path,changed_path,reservation,any_changed,rgb_changed,alpha_changed,generated_path,source_generated_ref,proposal_generated_ref,source_alpha_converted)
        check_active()
