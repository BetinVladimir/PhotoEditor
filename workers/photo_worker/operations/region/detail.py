from photo_worker.contracts.base import canonical_bytes
"""Authored portrait-detail replay on the actual Region color/beauty canvas."""
from contextlib import contextmanager,ExitStack
import numpy as np
from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.portrait_detail import PortraitParams
from photo_worker.operations.portrait.prepare_detail import prepare_detail
from photo_worker.runtime.arrays import read_array


def detail_entry(effective,verified):
    requested=effective.extensions.portrait_detail
    entries=[entry for entry in verified if entry[0].operation=='portrait.detail']
    if len(entries)!=int(requested is not None and requested.enabled):
        raise ValueError('REGION_DETAIL_STAGE_COVERAGE_MISMATCH')
    if not entries:return None
    stage,raw,source,target,policy=entries[0]
    params=PortraitParams.model_validate_json(canonical_bytes(raw));resolved=policy.get('resolved')
    if (resolved is None or resolved.modules.extensions.portrait_detail!=requested
            or resolved.strength!=effective.strength):
        raise ValueError('REGION_DETAIL_EFFECTIVE_POLICY_MISMATCH')
    for mask in [*(region.mask for region in params.regions),*params.protected]:
        if mask.canvas!=source.canvas or mask.transform_chain!=source.transform_chain:
            raise ValueError('REGION_DETAIL_MASK_GEOMETRY_MISMATCH')
    if target.alpha!=source.alpha:
        raise ValueError('REGION_DETAIL_ALPHA_CHANGED')
    from photo_worker.algorithms.effective import resolve_effective_params
    return stage,params,source,target,resolve_effective_params(resolved).modules.extensions.portrait_detail


@contextmanager
def prepare_region_detail(artifacts,effective,verified,rgb,alpha,metadata,*,check_active=lambda:None):
    entry=detail_entry(effective,verified)
    if entry is None:
        yield rgb;check_active();return
    _,params,source,_,detail=entry
    if (source.canvas!=metadata.canvas or source.transform_chain!=metadata.transform_chain
            or rgb.shape!=(source.canvas.height,source.canvas.width,3)):
        raise ValueError('REGION_DETAIL_SOURCE_GEOMETRY_MISMATCH')
    with ExitStack() as contexts:
        masks={}
        def read_mask(ref):
            check_active()
            key=canonical_hash(ref)
            if key not in masks:
                artifacts.require_declared_input(ref)
                local=contexts.enter_context(artifacts.input(ref,suffix='.npy'))
                masks[key]=contexts.enter_context(read_array(local.path,rgb.shape[:2],'float32',check_active,unit_interval=True))
            return masks[key]
        owner=contexts.enter_context(artifacts.scratch.reserve(rgb.shape[0]*rgb.shape[1]*4+8192))
        path=owner.path('region-detail-protection','.npy')
        protected=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=rgb.shape[:2])
        try:
            for y in range(0,rgb.shape[0],256):
                check_active();protected[y:y+256]=0
            for mask in params.protected:
                values=read_mask(mask.artifact)
                for y in range(0,rgb.shape[0],256):
                    check_active();np.maximum(protected[y:y+256],values[y:y+256],out=protected[y:y+256])
            protected.flush();owner.validate_file(path)
            with prepare_detail(rgb,params.regions,detail,read_mask,protected,check_active=check_active) as result:
                yield result.rgb;check_active()
        finally:protected._mmap.close()
