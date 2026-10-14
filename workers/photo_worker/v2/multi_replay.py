"""Independent original-pixel multi replay; no SQL adoption authority is granted."""
from contextlib import ExitStack
from uuid import uuid4
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.session.contracts import Calibration
from photo_worker.operations.optical.multi_request import decode_multi_request,prepare_frozen_multi_request
from photo_worker.operations.optical.multi_result import multi_clipping_fraction
from photo_worker.runtime.scratch import ScratchSpace
from .adoption import AdoptionInvalid
from .array_evidence import ArrayEvidenceSession
from .lens_bypass import _document,_ref
from .multi_adoption import verify_multi_result_metadata


def compare_multi_prepared(prepared,metadata,output,alpha,changed,fields,*,check_active):
    plan=prepared.automatic.prepared
    if (plan.shape!=(metadata.image.canvas.height,metadata.image.canvas.width)
            or len(plan.registrations)!=len(metadata.registrations)
            or prepared.automatic.registration_inconsistent!=metadata.registration_inconsistent
            or plan.result.motion_fraction!=metadata.motion_fraction
            or len(prepared.projections)!=len(fields)):
        raise AdoptionInvalid('MULTI_REPLAY_GEOMETRY_MISMATCH')
    try:
        fraction=multi_clipping_fraction(output,check_active=check_active)
    except ValueError as error:
        raise AdoptionInvalid('MULTI_REPLAY_ARRAY_INVALID') from error
    if fraction!=metadata.report.clipping_fraction:
        raise AdoptionInvalid('MULTI_REPLAY_CLIPPING_MISMATCH')
    for actual,expected in zip(metadata.registrations,plan.registrations):
        check_active()
        if not np.array_equal(np.asarray(actual.homography,np.float64).reshape(3,3),expected.source_to_output):
            raise AdoptionInvalid('MULTI_REPLAY_REGISTRATION_MISMATCH')
    with ExitStack() as owners:
        def compare(path,actual):
            expected=np.load(path,mmap_mode='r',allow_pickle=False)
            owners.callback(expected._mmap.close)
            if actual.dtype!=np.float32 or expected.dtype!=np.float32 or actual.shape!=expected.shape:
                raise AdoptionInvalid('MULTI_REPLAY_ARRAY_INVALID')
            for y in range(0,expected.shape[0],256):
                for x in range(0,expected.shape[1],256):
                    check_active();region=(slice(y,y+256),slice(x,x+256))
                    if not np.array_equal(actual[region].view(np.uint32),expected[region].view(np.uint32)):
                        raise AdoptionInvalid('MULTI_PIXEL_REPLAY_MISMATCH')
        for path,actual in ((prepared.encoded.rgb,output),(prepared.encoded.alpha,alpha),(prepared.encoded.changed_mask,changed)):
            compare(path,actual)
        for projection,actual in zip(prepared.projections,fields):compare(projection.inverse_field,actual)
    check_active()


def verify_multi_pixel_replay(reader,params_ref,root_ref,*,operation,model_lock_sha,dataset_sha,
        max_pixels,scratch_root,max_bytes,ram_max_bytes,verify_icc,check_active):
    """Replay registration from source proxies, never trust submitted matrices.

    Caller must independently prove current qualification/report/SQL scope and
    pin implementation bytes. Budgets include materialized input/output NPYs.
    """
    if (not callable(verify_icc) or any(type(v) is not int or not 1<=v<=2**53-1
            for v in (max_bytes,ram_max_bytes))):raise AdoptionInvalid('MULTI_REPLAY_RESOURCE_INVALID')
    metadata=verify_multi_result_metadata(reader,params_ref,root_ref,operation=operation,
        max_pixels=max_pixels,check_active=check_active)
    calibration=_document(reader,_ref(metadata.params.calibration),Calibration,'input')
    request=decode_multi_request(operation,canonical_bytes(metadata.params),canonical_bytes(calibration),
        model_lock_sha=model_lock_sha,dataset_sha=dataset_sha,max_pixels=max_pixels)
    shape=(metadata.image.canvas.height,metadata.image.canvas.width)
    with ArrayEvidenceSession(reader,scratch_root,max_bytes,check_active) as arrays:
        rgbs=[];alphas=[]
        for image in metadata.sources:
            source_shape=(image.canvas.height,image.canvas.width)
            rgbs.append(arrays.array(_ref(image.pixels),(*source_shape,3),source='input',legacy_input_npy=True))
            alphas.append(None if image.alpha is None else arrays.array(_ref(image.alpha),source_shape,
                source='input',unit=True,legacy_alpha=True,legacy_input_npy=True))
        output=arrays.array(_ref(metadata.image.pixels),(*shape,3),source='output',role='pixels')
        alpha=arrays.array(_ref(metadata.image.alpha),shape,source='output',role='alpha',unit=True)
        changed=arrays.array(_ref(metadata.changed.artifact),shape,source='output',role='mask_pixels',unit=True)
        fields=[]
        for image,registration in zip(metadata.sources,metadata.registrations):
            if registration.inverse_field is not None:
                fields.append(arrays.array(_ref(registration.inverse_field),(image.canvas.height,image.canvas.width,2),
                    source='output',role='warp_field',inverse_field=True))
        disk=max_bytes-arrays.used;ram=ram_max_bytes-arrays.used
        if disk<=0 or ram<=0:raise AdoptionInvalid('MULTI_REPLAY_RESOURCE_EXCEEDED')
        with ScratchSpace(scratch_root,uuid4(),uuid4(),1,disk) as scratch:
            with prepare_frozen_multi_request(request,metadata.sources,rgbs,alphas,scratch,
                    verify_icc=verify_icc,ram_max_bytes=ram,check_active=check_active) as prepared:
                compare_multi_prepared(prepared,metadata,output,alpha,changed,fields,check_active=check_active)
    check_active();return metadata


def verify_admitted_multi_replay(reader,params_ref,root_ref,*,ctx,admission,
        scratch_root,max_bytes,ram_max_bytes):
    """Require independently installed admission and uncached live authority.

    For completed tasks, resolver must implement adoption authority, not a live
    worker lease check. Final SQL transaction must recheck the same authority.
    """
    from copy import deepcopy
    from photo_worker.operations.optical.multi_admission import PinnedMultiImageAdmission
    if not isinstance(admission,PinnedMultiImageAdmission):
        raise AdoptionInvalid('MULTI_REPLAY_QUALIFICATION_UNAVAILABLE')
    metadata=verify_multi_result_metadata(reader,params_ref,root_ref,operation=admission.operation,
        max_pixels=admission.max_pixels,check_active=ctx.check_active)
    params=deepcopy(metadata.params);sources=deepcopy(metadata.sources)
    before=admission.admit(ctx,params,sources)
    result=verify_multi_pixel_replay(reader,params_ref,root_ref,operation=admission.operation,
        model_lock_sha=admission.pin.model_lock_sha,dataset_sha=admission.pin.dataset_sha,
        max_pixels=admission.max_pixels,scratch_root=scratch_root,max_bytes=max_bytes,
        ram_max_bytes=ram_max_bytes,verify_icc=admission.verify_color,check_active=ctx.check_active)
    after=admission.admit(ctx,params,sources)
    if after!=before:raise AdoptionInvalid('MULTI_REPLAY_QUALIFICATION_CHANGED')
    if canonical_bytes(result.params)!=before.params_jcs:
        raise AdoptionInvalid('MULTI_REPLAY_PARAMS_CHANGED')
    ctx.check_active();return result
