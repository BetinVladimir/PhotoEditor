"""Declared, byte-verified artifact loader for approved local exposure.

Installation supplies live source/protection authority and exact ICC proof.
Returned arrays are owned; materialized files/mappings never escape this call.
"""
from contextlib import ExitStack
from copy import deepcopy
import numpy as np
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import WorkingImage,MaskRef,ResolvedTransform
from photo_worker.contracts.semantic import ApprovedSemanticEdit,TransferCandidate,EditIntent
from photo_worker.runtime.arrays import read_array
from photo_worker.operations.optical.color import to_working_linear
from .approved_binding import bind_approved_edit_documents
from .exposure_geometry import bind_local_exposure_geometry
from .exposure_protection import bind_local_exposure_protection
from .execute_exposure import LocalExposureArrays
from .local_exposure import local_exposure_memory


def load_local_exposure_arrays(artifacts,edit,*,protection_ref,verify_origin,verify_color,verify_admission,max_bytes,check_active):
    if not all(callable(value) for value in (verify_origin,verify_color,verify_admission,check_active)):
        raise ValueError('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
    edit=ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit))
    root=ArtifactRef.model_validate_json(canonical_bytes(protection_ref))
    if type(max_bytes) is not int or not 64*1024**2<=max_bytes<=2**53-1:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    def active_scope():
        check_active()
        if verify_admission(edit.model_copy(deep=True)) is not True:
            raise ValueError('SEMANTIC_EXPOSURE_ADMISSION_DENIED')
        check_active()
    active_scope()
    candidate_raw=artifacts.contract_bytes(edit.candidate,TransferCandidate)
    intent_raw=artifacts.contract_bytes(edit.intent,EditIntent)
    edit,_,intent=bind_approved_edit_documents(edit,candidate_raw,intent_raw)
    source_raw=artifacts.contract_bytes(edit.source.source,WorkingImage)
    reference_raw=artifacts.contract_bytes(intent.reference.source,WorkingImage)
    mask_raw=artifacts.contract_bytes(edit.mask,MaskRef)
    transform_raw=artifacts.contract_bytes(edit.transform,ResolvedTransform)
    image,mask,_=bind_local_exposure_geometry(edit,candidate_raw,intent_raw,source_raw,mask_raw,transform_raw,reference_raw)
    protection_raw=artifacts.contract_bytes(root,MaskRef)
    protection=bind_local_exposure_protection(edit,source_raw,root,protection_raw,verify_origin=verify_origin,check_active=check_active)
    shape=(image.canvas.height,image.canvas.width)
    # Conservative full document/NPY/transfer peak. No decoder/model is run.
    peak=local_exposure_memory(shape)+64*1024**2
    if peak>max_bytes:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    if verify_color(deepcopy(image.color)) is not True:
        raise ValueError('SEMANTIC_EXPOSURE_ICC_UNQUALIFIED')
    active_scope()
    with ExitStack() as stack:
        def array(ref,dimensions,unit):
            local=stack.enter_context(artifacts.input(ref,suffix='.npy'))
            return stack.enter_context(read_array(local.path,dimensions,'float32',check_active,unit_interval=unit))
        rgb=array(image.pixels,(*shape,3),False)
        weights=array(mask.artifact,shape,True)
        protected=array(protection.artifact,shape,True)
        for y in range(0,shape[0],256):
            for x in range(0,shape[1],256):
                check_active()
                if np.any((weights[y:y+256,x:x+256]>0)&(protected[y:y+256,x:x+256]>0)):
                    raise ValueError('SEMANTIC_EXPOSURE_PROTECTED_OVERLAP')
        linear=np.empty((*shape,3),dtype=np.float32)
        for y in range(0,shape[0],256):
            for x in range(0,shape[1],256):
                check_active()
                tile=rgb[y:y+256,x:x+256]
                if np.any(tile<0):raise ValueError('SEMANTIC_EXPOSURE_ARRAYS_INVALID')
                linear[y:y+256,x:x+256]=to_working_linear(tile,image.color)
        owned_weights=np.array(weights,dtype=np.float32,copy=True,order='C')
        owned_protected=np.array(protected,dtype=np.float32,copy=True,order='C')
    active_scope()
    return LocalExposureArrays(linear,owned_weights,owned_protected,loader_peak_bytes=peak,retained_loader_bytes=0)
