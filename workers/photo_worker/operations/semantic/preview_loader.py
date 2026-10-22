"""Exact-artifact loader for preview only; no borrowed Apply admission."""
from contextlib import ExitStack
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage,MaskRef,ResolvedTransform
from photo_worker.contracts.semantic import TransferCandidate,EditIntent
from photo_worker.contracts.semantic_preview import SemanticExposurePreviewInput
from photo_worker.runtime.arrays import read_array
from .preview_binding import (bind_exposure_preview_documents,
    bind_exposure_preview_geometry,bind_exposure_preview_protection)
from .preview_exposure import PreviewExposureArrays
from .local_exposure import local_exposure_memory


def load_proposal_preview_arrays(artifacts,params,*,input_model,bind_documents,bind_geometry,
        bind_protection,estimate_peak,array_type,verify_admission,verify_origin,
        verify_color,max_bytes,check_active):
    if not all(callable(value) for value in (verify_admission,verify_origin,verify_color,check_active)):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    owned=input_model.model_validate_json(canonical_bytes(params))
    if type(max_bytes) is not int or not 64*1024**2<=max_bytes<=2**53-1:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    def admitted():
        check_active()
        if verify_admission(owned.model_copy(deep=True)) is not True:
            raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
        check_active()
    def read_contract(ref, model):
        admitted()
        raw = artifacts.contract_bytes(ref, model)
        admitted()
        return raw
    admitted()
    candidate_raw=read_contract(owned.candidate,TransferCandidate)
    intent_raw=read_contract(owned.intent,EditIntent)
    owned,candidate,intent=bind_documents(owned,candidate_raw,intent_raw)
    source_raw=read_contract(owned.source.source,WorkingImage)
    reference_raw=read_contract(intent.reference.source,WorkingImage)
    mask_raw=read_contract(candidate.mask,MaskRef)
    transform_raw=read_contract(candidate.transform,ResolvedTransform)
    image,mask,_=bind_geometry(owned,candidate_raw,intent_raw,
        source_raw,mask_raw,transform_raw,reference_raw)
    protection_raw=read_contract(owned.protection,MaskRef)
    protection=bind_protection(owned,source_raw,protection_raw,
        verify_origin=verify_origin,check_active=check_active)
    binaries=(image.pixels,mask.artifact,protection.artifact)
    if len({ref.artifact_id for ref in binaries})!=3:
        raise ValueError('SEMANTIC_PREVIEW_ARTIFACT_ALIAS')
    shape=(image.canvas.height,image.canvas.width)
    peak=estimate_peak(shape)
    if peak>max_bytes:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    if verify_color(image.color.model_copy(deep=True)) is not True:
        raise ValueError('SEMANTIC_EXPOSURE_ICC_UNQUALIFIED')
    admitted()
    with ExitStack() as stack:
        def array(ref,dimensions,unit):
            admitted()
            local=stack.enter_context(artifacts.input(ref,suffix='.npy'))
            admitted()
            return stack.enter_context(read_array(local.path,dimensions,'float32',check_active,unit_interval=unit))
        original=array(image.pixels,(*shape,3),False)
        weights=array(mask.artifact,shape,True)
        protected=array(protection.artifact,shape,True)
        for y in range(0,shape[0],256):
            for x in range(0,shape[1],256):
                check_active();tile=np.s_[y:y+256,x:x+256]
                if np.any(original[tile]<0):
                    raise ValueError('SEMANTIC_EXPOSURE_ARRAYS_INVALID')
                if np.any((weights[tile]>0)&(protected[tile]>0)):
                    raise ValueError('SEMANTIC_EXPOSURE_PROTECTED_OVERLAP')
        rgb=np.array(original,dtype=np.float32,copy=True,order='C')
        mask_values=np.array(weights,dtype=np.float32,copy=True,order='C')
        protection_values=np.array(protected,dtype=np.float32,copy=True,order='C')
    admitted()
    return array_type(rgb,mask_values,protection_values,peak,0)


def load_exposure_preview_arrays(artifacts,params,*,verify_admission,verify_origin,
        verify_color,max_bytes,check_active):
    return load_proposal_preview_arrays(artifacts,params,input_model=SemanticExposurePreviewInput,
        bind_documents=bind_exposure_preview_documents,bind_geometry=bind_exposure_preview_geometry,
        bind_protection=bind_exposure_preview_protection,
        estimate_peak=lambda shape:local_exposure_memory(shape)+64*1024**2,array_type=PreviewExposureArrays,
        verify_admission=verify_admission,verify_origin=verify_origin,verify_color=verify_color,
        max_bytes=max_bytes,check_active=check_active)


def execute_artifact_exposure_preview(artifacts,params,*,verify_admission,
        verify_origin,verify_color,max_bytes,check_active):
    """Complete byte-backed compute entry point; publisher/SQL install separately."""
    from .preview_exposure import execute_exposure_preview
    if not all(callable(value) for value in (verify_admission,verify_origin,verify_color,check_active)):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    owned=SemanticExposurePreviewInput.model_validate_json(canonical_bytes(params))
    if type(max_bytes) is not int or not 64*1024**2<=max_bytes<=2**53-1:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    check_active()
    if verify_admission(owned.model_copy(deep=True)) is not True:
        raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
    check_active()
    candidate_raw=artifacts.contract_bytes(owned.candidate,TransferCandidate)
    intent_raw=artifacts.contract_bytes(owned.intent,EditIntent)
    source_raw=artifacts.contract_bytes(owned.source.source,WorkingImage)
    def load(value,candidate,image,*,max_bytes):
        if canonical_bytes(value)!=canonical_bytes(owned):
            raise ValueError('SEMANTIC_PREVIEW_BINDING_INVALID')
        return load_exposure_preview_arrays(artifacts,value,
            verify_admission=verify_admission,verify_origin=verify_origin,
            verify_color=verify_color,max_bytes=max_bytes,check_active=check_active)
    return execute_exposure_preview(owned,candidate_raw,intent_raw,source_raw,
        verify_admission=verify_admission,load_verified_arrays=load,
        verify_color=verify_color,max_bytes=max_bytes,check_active=check_active)


def load_exposure_preview_alpha(artifacts,params,source_raw,*,verify_admission,
        max_bytes,check_active):
    from .preview_binding import bind_exposure_preview_source
    return _load_preview_alpha(artifacts,params,source_raw,
        input_model=SemanticExposurePreviewInput,bind_source=bind_exposure_preview_source,
        verify_admission=verify_admission,max_bytes=max_bytes,check_active=check_active)


def load_removal_preview_alpha(artifacts,params,source_raw,*,verify_admission,
        max_bytes,check_active):
    from photo_worker.contracts.semantic_preview import SemanticRemovalPreviewInput
    from .preview_binding import bind_removal_preview_source
    return _load_preview_alpha(artifacts,params,source_raw,
        input_model=SemanticRemovalPreviewInput,bind_source=bind_removal_preview_source,
        verify_admission=verify_admission,max_bytes=max_bytes,check_active=check_active)


def _load_preview_alpha(artifacts,params,source_raw,*,input_model,bind_source,
        verify_admission,max_bytes,check_active):
    """Own inherited opacity with the existing float32/legacy uint16 parser."""
    from photo_worker.runtime.arrays import read_alpha_array
    if not callable(verify_admission) or not callable(check_active):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    owned=input_model.model_validate_json(canonical_bytes(params))
    image=bind_source(owned,source_raw)
    pixels=image.canvas.width*image.canvas.height
    if type(max_bytes) is not int or not 8*pixels+16*1024**2<=max_bytes<=2**53-1:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    def admitted():
        check_active()
        if verify_admission(owned.model_copy(deep=True)) is not True:
            raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
        check_active()
    admitted()
    if image.alpha is None:return None
    with artifacts.input(image.alpha,suffix='.npy') as local:
        admitted()
        with read_alpha_array(local.path,(image.canvas.height,image.canvas.width),check_active) as values:
            admitted()
            opacity=np.array(values,dtype=np.float32,copy=True,order='C')
            if values.dtype==np.uint16:opacity/=65535
    admitted()
    return opacity


def load_background_cleanup_preview_alpha(artifacts,params,source_raw,*,verify_admission,
        max_bytes,check_active):
    from photo_worker.contracts.semantic_preview import SemanticBackgroundCleanupPreviewInput
    from .preview_binding import bind_background_cleanup_preview_source
    return _load_preview_alpha(artifacts,params,source_raw,
        input_model=SemanticBackgroundCleanupPreviewInput,bind_source=bind_background_cleanup_preview_source,
        verify_admission=verify_admission,max_bytes=max_bytes,check_active=check_active)
