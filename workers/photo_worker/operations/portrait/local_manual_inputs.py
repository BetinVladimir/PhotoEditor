"""Pinned manual proof reads; origin refs must come from SQL canonical lineage."""
from contextlib import ExitStack
from dataclasses import dataclass

from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.manual_mask import MaskRasterParams,ManualMask,RasterizedMask
from photo_worker.runtime.arrays import read_array
from .local_manual_proof import verify_manual_pair


@dataclass(frozen=True)
class ManualPairOrigin:
    root: ArtifactRef
    params: ArtifactRef
    vectors: ArtifactRef | None


def read_verified_manual_pair(ctx,artifacts,source_ref,source,target,guard,origin):
    """Re-read exact producer documents and independently replay original pixels.

    This does not authorize caller-supplied origin locators. SQL admission must
    bind these refs to the canonical original producer and current lease.
    """
    ctx.check_active()
    if not isinstance(origin,ManualPairOrigin):
        raise ValueError('PORTRAIT_MANUAL_ORIGIN_INVALID')
    required=[source_ref,origin.root,origin.params,target.artifact,guard.artifact]
    if origin.vectors is not None:required.append(origin.vectors)
    if any(ref not in ctx.lease.command.inputs for ref in required):
        raise ValueError('PORTRAIT_MANUAL_INPUT_UNDECLARED')
    root=artifacts.contract(origin.root,RasterizedMask)
    raw=artifacts.contract_bytes(origin.params,MaskRasterParams)
    params=MaskRasterParams.model_validate_json(raw)
    if params.vectors!=origin.vectors:
        raise ValueError('PORTRAIT_MANUAL_ORIGIN_INVALID')
    vectors_raw=None if origin.vectors is None else artifacts.contract_bytes(origin.vectors,ManualMask)
    h,w=source.canvas.height,source.canvas.width
    with ExitStack() as stack:
        def raster(ref):
            path=stack.enter_context(artifacts.input(ref,suffix='.npy')).path
            return stack.enter_context(read_array(path,(h,w),'float32',ctx.check_active,unit_interval=True))
        target_pixels=raster(target.artifact)
        guard_pixels=raster(guard.artifact)
        nonzero=verify_manual_pair(source_ref,source,origin.params,raw,root,target,guard,
                                  target_pixels,guard_pixels,vectors_raw=vectors_raw,
                                  check_active=ctx.check_active)
    ctx.check_active()
    return nonzero
