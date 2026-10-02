"""Resolve same-geometry parent documents and verified binary lineage inputs."""
from contextlib import contextmanager,ExitStack
from dataclasses import dataclass
from typing import Callable
import numpy as np
from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.operations.optical.contracts import ImageResult
from photo_worker.runtime.arrays import read_array
from .contracts import LayerComposeParams
from .reports import parent_review

@dataclass(frozen=True)
class LayerParents:
    source:WorkingImage
    proposal:WorkingImage
    source_result:ImageResult|None
    proposal_result:ImageResult
    read_source_generated:Callable|None
    read_proposal_generated:Callable|None


@contextmanager
def resolve_layer_parents(params:LayerComposeParams,artifacts,*,check_active=lambda:None,before_lineage=lambda source,proposal:None):
    """Resolve declared artifacts only; SQL must authorize null original lineage.

    All geometry/color checks precede lineage NPY reads. Binary coverage is
    validated across the complete file in bounded blocks before yielding readers.
    Borrowed tile views must be consumed inside this context.
    """
    check_active()
    if not isinstance(params,LayerComposeParams):raise ValueError('LAYER_PARAMS_INVALID')
    source=artifacts.contract(params.base.source,WorkingImage)
    source_result=artifacts.contract(params.source_result,ImageResult) if params.source_result is not None else None
    proposal_result=artifacts.contract(params.proposal_result,ImageResult)
    if source_result is not None and source_result.working_image!=params.base.source:
        raise ValueError('LAYER_SOURCE_PARENT_INVALID')
    proposal=artifacts.contract(proposal_result.working_image,WorkingImage)
    if (source.dtype!='float32' or proposal.dtype!='float32' or
            canonical_hash(source.canvas)!=params.base.canvas_hash or
            source.canvas!=proposal.canvas or source.transform_chain!=proposal.transform_chain or source.color!=proposal.color):
        raise ValueError('LAYER_PARENT_GEOMETRY_INVALID')
    parent_review(artifacts,source_result,proposal_result,params.base)
    before_lineage(source,proposal)
    check_active()
    h,w=source.canvas.height,source.canvas.width
    masks={}
    # Resolve and check every descriptor before downloading any coverage array.
    for parent in (source_result,proposal_result):
        if parent is None or parent.generated_mask is None:continue
        reference=parent.generated_mask;key=(reference.artifact_id,reference.sha256)
        if key in masks:continue
        mask=artifacts.contract(reference,MaskRef)
        if mask.canvas!=source.canvas or mask.transform_chain!=source.transform_chain:
            raise ValueError('LAYER_GENERATED_GEOMETRY_INVALID')
        masks[key]=mask
    with ExitStack() as stack:
        arrays={}
        for key,mask in masks.items():
            check_active()
            local=stack.enter_context(artifacts.input(mask.artifact,suffix='.npy'))
            array=stack.enter_context(read_array(local.path,(h,w),'float32',check_active,unit_interval=True))
            flat=array.reshape(-1)
            for start in range(0,flat.size,262144):
                check_active();block=flat[start:start+262144]
                if not np.all((block==0)|(block==1)):raise ValueError('LAYER_GENERATED_NOT_BINARY')
            arrays[key]=array
        is_open=True
        def reader(parent):
            if parent is None or parent.generated_mask is None:return None
            ref=parent.generated_mask;key=(ref.artifact_id,ref.sha256)
            def tile(x,y,width,height):
                if not is_open:raise ValueError('LAYER_PARENTS_CLOSED')
                check_active()
                if (any(type(n) is not int for n in (x,y,width,height)) or min(x,y)<0 or
                        min(width,height)<1 or x+width>w or y+height>h):raise ValueError('LAYER_TILE_INVALID')
                return arrays[key][y:y+height,x:x+width]
            return tile
        try:
            check_active()
            yield LayerParents(source,proposal,source_result,proposal_result,reader(source_result),reader(proposal_result))
            check_active()
        finally:
            is_open=False;arrays.clear()
