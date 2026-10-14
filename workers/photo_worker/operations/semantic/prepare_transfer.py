"""Owned and admitted planar mask stage, without proposal publication."""
from contextlib import contextmanager,ExitStack
from dataclasses import dataclass
from pathlib import Path
from copy import deepcopy
import numpy as np
from photo_worker.algorithms.semantic_transfer import warp_planar_mask_into
from photo_worker.runtime.executor import OperationFailure


@dataclass(frozen=True)
class PreparedTransferredMask:
    path: Path
    reservation: object
    pixels: np.ndarray
    source_pixels: int
    target_pixels: int


def planar_mask_resources(source_shape,target_shape):
    """Mask-stage retained mappings and bounded native tile workspace only."""
    for shape in (source_shape,target_shape):
        if (not isinstance(shape,tuple) or len(shape)!=2
                or any(type(v) is not int or v<8 for v in shape) or shape[0]*shape[1]>200000000):
            raise ValueError('SEMANTIC_MASK_DIMENSIONS_INVALID')
    source=source_shape[0]*source_shape[1];target=target_shape[0]*target_shape[1]
    # Source mask/protection and target protection/output mappings coexist.
    return dict(ram_bytes=8*(source+target)+64*1048576,
        scratch_bytes=4*target+8192,vram_bytes=0,output_bytes=0)


@contextmanager
def prepare_planar_mask_transfer(source,matrix,target_shape,*,source_protected,target_protected,
        scratch,execution_profile,check_active=lambda:None):
    """Caller supplies verified source/target protection and qualified geometry.

    Resource cost covers this mask stage, not upstream RGB/proxy/model owners.
    Output remains private until every warp/protection check has succeeded.
    """
    check_active()
    if not isinstance(source,np.ndarray):raise ValueError('SEMANTIC_MASK_INVALID')
    required=planar_mask_resources(source.shape,target_shape)
    profile=deepcopy(execution_profile)
    estimate=profile.get('resource_estimate') if isinstance(profile,dict) else None
    if (not isinstance(estimate,dict) or any(type(estimate.get(k)) is not int
            or not 0<=estimate[k]<=2**53-1 for k in required)):
        raise OperationFailure('CONFIG_INVALID')
    for key,code in [('ram_bytes','RAM_ADMISSION_EXCEEDED'),('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),
            ('vram_bytes','VRAM_ADMISSION_EXCEEDED'),('output_bytes','OUTPUT_ADMISSION_EXCEEDED')]:
        if estimate[key]<required[key]:raise OperationFailure(code)
    def positive_count(value):
        count=0
        for y in range(0,value.shape[0],256):
            for x in range(0,value.shape[1],256):
                check_active();count+=int(np.count_nonzero(value[y:y+256,x:x+256]>0))
        return count
    source_pixels=positive_count(source)
    if source_pixels==0:raise ValueError('SEMANTIC_MASK_EMPTY')
    matrix=np.array(matrix,dtype=np.float64,copy=True)
    with scratch.reserve(required['scratch_bytes']) as owner:
        path=owner.path('semantic-transferred-mask','.npy')
        output=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=target_shape)
        try:
            warp_planar_mask_into(source,matrix,output,source_protected=source_protected,
                target_protected=target_protected,check_active=check_active)
            target_pixels=positive_count(output)
            if target_pixels==0:raise ValueError('SEMANTIC_MASK_EMPTY')
            owner.validate();output.flush();output.flags.writeable=False
            check_active()
            yield PreparedTransferredMask(path,owner,output,source_pixels,target_pixels)
            check_active()
        finally:output._mmap.close()


@dataclass(frozen=True)
class PreparedPlanarTransfer:
    match: object
    mask: PreparedTransferredMask | None


def planar_transfer_resources(source_shape,target_shape):
    """Retained linear RGB and mask owners plus bounded proxy/ORB buffers."""
    costs=planar_mask_resources(source_shape,target_shape)
    costs['ram_bytes']+=12*(source_shape[0]*source_shape[1]+target_shape[0]*target_shape[1])+256*1048576
    return costs


@contextmanager
def prepare_planar_transfer(reference,target,source_mask,*,source_protected,target_protected,
        scratch,execution_profile,match_error_px_max,match_inlier_min,verify_scene,
        check_active=lambda:None):
    """Join geometry/mask owners, requiring independent scene authority first.

    Installed verify_scene checks pinned source/calibration/runtime plus planar
    background and occlusion evidence, raising on missing/unsupported evidence.
    No geometric match or callback return value constitutes human approval.
    """
    from photo_worker.algorithms.semantic_transfer import match_planar_linear_images
    check_active()
    if not callable(verify_scene):raise ValueError('SEMANTIC_SCENE_VERIFIER_REQUIRED')
    for rgb in (reference,target):
        if not isinstance(rgb,np.ndarray) or rgb.dtype!=np.float32 or rgb.ndim!=3 or rgb.shape[2]!=3:
            raise ValueError('SEMANTIC_PROXY_INPUT_INVALID')
    costs=planar_transfer_resources(reference.shape[:2],target.shape[:2])
    profile=deepcopy(execution_profile)
    estimate=profile.get('resource_estimate') if isinstance(profile,dict) else None
    if not isinstance(estimate,dict) or any(type(estimate.get(k)) is not int or not 0<=estimate[k]<=2**53-1 for k in costs):
        raise OperationFailure('CONFIG_INVALID')
    for key,code in [('ram_bytes','RAM_ADMISSION_EXCEEDED'),('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),
            ('vram_bytes','VRAM_ADMISSION_EXCEEDED'),('output_bytes','OUTPUT_ADMISSION_EXCEEDED')]:
        if estimate[key]<costs[key]:raise OperationFailure(code)
    if not isinstance(source_mask,np.ndarray) or source_mask.shape!=reference.shape[:2]:
        raise ValueError('SEMANTIC_MASK_INVALID')
    verify_scene();check_active()
    match=match_planar_linear_images(reference,target,match_error_px_max=match_error_px_max,
        match_inlier_min=match_inlier_min,check_active=check_active)
    if match.reference_to_target is None:
        yield PreparedPlanarTransfer(match,None)
        check_active();return
    with ExitStack() as owners:
        try:
            transferred=owners.enter_context(prepare_planar_mask_transfer(
                source_mask,match.reference_to_target,target.shape[:2],
                source_protected=source_protected,target_protected=target_protected,scratch=scratch,
                execution_profile=profile,check_active=check_active))
        except ValueError as error:
            # Only a completed protection gate produces this unsupported result.
            # Consumer/staging errors occur outside this catch and must propagate.
            if error.args!=('SEMANTIC_PROTECTED_OVERLAP',):raise
            from photo_worker.algorithms.semantic_transfer import PlanarMatch
            check_active()
            yield PreparedPlanarTransfer(PlanarMatch(None,match.inlier_fraction,
                'SEMANTIC_PROTECTED_OVERLAP'),None)
            check_active();return
        yield PreparedPlanarTransfer(match,transferred)
        check_active()


@contextmanager
def prepare_calibrated_planar_transfer(params,intent,calibration,reference,target,source_mask,*,
        source_protected,target_protected,scratch,execution_profile,verify_scene,
        check_active=lambda:None):
    """Bind typed frozen documents to geometry/mask processing.

    Installed verifier receives owned copies to prove source/mask/target snapshot,
    qualified runtime and planar/occlusion evidence; content binding is separate.
    """
    from photo_worker.contracts.semantic import bind_semantic_transfer_documents
    check_active()
    if not callable(verify_scene):raise ValueError('SEMANTIC_SCENE_VERIFIER_REQUIRED')
    params,intent,calibration=bind_semantic_transfer_documents(params,intent,calibration)
    profile=deepcopy(execution_profile)
    def verified():
        check_active()
        verify_scene(deepcopy(params),deepcopy(intent),deepcopy(calibration))
        check_active()
    with prepare_planar_transfer(reference,target,source_mask,source_protected=source_protected,
            target_protected=target_protected,scratch=scratch,execution_profile=profile,
            match_error_px_max=calibration.match_error_px_max,match_inlier_min=calibration.match_inlier_min,
            verify_scene=verified,check_active=check_active) as result:
        yield result
        check_active()
