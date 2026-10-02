"""Conservative binary generated-pixel dependency coverage for adjustment layers."""
import math
import numpy as np


def generated_coverage(mask,*,opacity:float,blend:str,source=None,proposal=None,protected=None,
                       max_tile_bytes:int,check_active=lambda:None):
    """Normal full replacement removes source dependency; other blends retain it.

    This is dependency coverage, not a photographic authenticity assertion.
    Proposal contributes wherever E>0, including bit-identical generated proposals.
    """
    check_active()
    if (not isinstance(mask,np.ndarray) or mask.dtype!=np.float32 or mask.ndim!=2 or
        min(mask.shape)<1 or max(mask.shape)>1024 or type(opacity) not in (float,int) or
        not math.isfinite(opacity) or not 0<=opacity<=1 or blend not in ('normal','multiply','screen') or
        type(max_tile_bytes) is not int or max_tile_bytes<mask.size*64):raise ValueError('LINEAGE_TILE_INVALID')
    for value in (mask,source,proposal,protected):
        if value is not None and (not isinstance(value,np.ndarray) or value.shape!=mask.shape or
            value.dtype!=np.float32 or not np.isfinite(value).all()):raise ValueError('LINEAGE_MASK_INVALID')
    for value in (source,proposal):
        if value is not None and not np.all((value==0)|(value==1)):raise ValueError('LINEAGE_MASK_NOT_BINARY')
    if source is None and proposal is None:return None
    effect=np.clip(mask,0,1).astype(np.float64)*opacity
    if protected is not None:
        effect*=1-np.clip(protected,0,1);effect[protected>0]=0
    output=np.zeros(mask.shape,np.float32)
    if source is not None:
        inherited=(effect<1) if blend=='normal' else np.ones(mask.shape,bool)
        output[(source>0)&inherited]=1
    if proposal is not None:output[(proposal>0)&(effect>0)]=1
    check_active()
    return output
