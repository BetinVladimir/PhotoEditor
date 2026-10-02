"""Owned same-canvas adjustment pixel phase; publication/lineage is separate."""
from contextlib import contextmanager,ExitStack
from dataclasses import dataclass,field
import numpy as np
from photo_worker.operations.layers.prepare import prepare_layer
from photo_worker.runtime.arrays import read_array

@dataclass(frozen=True)
class AdjustmentPixels:
    rgb:np.ndarray=field(repr=False)
    alpha:np.ndarray|None=field(repr=False)
    generated:np.ndarray|None=field(default=None,repr=False)


def _borrow(array):
    if array is None:return None
    view=array.view();view.flags.writeable=False;return view


@contextmanager
def compose_render_adjustments(source,alpha,layers,masks,scratch,render_proposal,*,color,max_tile_bytes:int,tile_size:int=512,generated=None,check_active=lambda:None):
    """Execute active qualified same-geometry proposals in authored order.

    render_proposal is a runtime-qualified context factory accepting a loaded
    layer and borrowed current pixels. It owns proposal lifetime. Geometry
    operators require their explicit transform stage before this phase. This
    helper never stages results or claims inherited generation/report lineage.
    """
    geometry={'optical.lens','optical.perspective','optical.panorama','optical.focus_stack'}
    for item in layers:
        if item.layer.enabled and item.layer.opacity>0 and item.layer.operation in geometry:
            raise ValueError('RENDER_GEOMETRY_STAGE_REQUIRED')
    owner=ExitStack();current=AdjustmentPixels(_borrow(source),_borrow(alpha),_borrow(generated))
    try:
        for item in layers:
            check_active();layer=item.layer
            if not layer.enabled or layer.opacity==0:continue
            next_owner=ExitStack()
            try:
                with render_proposal(item,current) as proposal:
                    check_active()
                    files=next_owner.enter_context(prepare_layer(current.rgb,proposal.rgb,
                        lambda x,y,w,h:masks.layer(layer.id,x,y,w,h),scratch,color=color,
                        opacity=layer.opacity,blend=layer.blend,source_alpha=current.alpha,
                        proposal_alpha=proposal.alpha,read_protected=masks.protection,
                        read_source_generated=(lambda x,y,w,h:current.generated[y:y+h,x:x+w]) if current.generated is not None else None,
                        read_proposal_generated=(lambda x,y,w,h:proposal.generated[y:y+h,x:x+w]) if proposal.generated is not None else None,
                        max_tile_bytes=max_tile_bytes,tile_size=tile_size,check_active=check_active))
                shape=current.rgb.shape
                rgb=next_owner.enter_context(read_array(files.rgb,shape,'float32',check_active))
                out_alpha=next_owner.enter_context(read_array(files.alpha,shape[:2],'float32',check_active,unit_interval=True)) if files.alpha is not None else None
                out_generated=next_owner.enter_context(read_array(files.generated_mask,shape[:2],'float32',check_active,unit_interval=True)) if files.generated_mask is not None else None
                replacement=AdjustmentPixels(_borrow(rgb),_borrow(out_alpha),_borrow(out_generated));check_active()
            except BaseException:
                next_owner.close();raise
            owner.close();owner=next_owner;current=replacement
        check_active();yield current;check_active()
    finally:owner.close()
