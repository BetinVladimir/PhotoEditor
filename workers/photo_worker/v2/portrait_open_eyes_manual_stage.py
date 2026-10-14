"""Original manual raster proof to donor output staging; deployment remains separate."""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.portrait_open_eyes import PortraitOpenEyesParams
from .portrait_donor_manual import read_verified_donor_manual_masks
from .portrait_donor_mask_patch import select_donor_mask_patch,plan_donor_mask_patch
from .portrait_donor_images import verify_donor_image_pair
from .portrait_donor_evidence import verify_open_eyes_faces
from .portrait_open_eyes_stage import prepare_and_stage_open_eyes_patch
from photo_worker.operations.portrait.donor_resources import admit_donor_portrait
from photo_worker.operations.portrait.donor_runtime_lock import DonorPortraitRuntimeLock,verify_installed_runtime
from photo_worker.runtime.executor import OperationFailure
from copy import deepcopy


def prepare_and_stage_manual_open_eyes(params,source_raw,donor_raw,confirmation_raw,
        source_faces_raw,donor_faces_raw,target_raw,protected_raw,root_ref,root_raw,
        producer_params_ref,producer_params_raw,reader,artifacts,scratch,*,
        vectors_raw=None,source_origin,patch_size,min_face_edge_px,
        mask_scratch_root,mask_max_bytes,material_scratch_root,material_max_bytes,
        check_active,authority,producer_authority,alpha_mode,profile,runtime_lock,expected_runtime_sha):
    """Caller must establish trusted profile capacity, anatomy and live lease.

    Producer and donor callbacks must implement independent current relational
    authority. No worker registration, activation or adoption happens here.
    """
    if not all(callable(c) for c in (check_active,authority,producer_authority)):
        raise ValueError('DONOR_AUTHORITY')
    owned=PortraitOpenEyesParams.model_validate_json(canonical_bytes(params))
    owned_profile=deepcopy(profile)
    owned_lock=DonorPortraitRuntimeLock.model_validate_json(canonical_bytes(runtime_lock))
    if not isinstance(owned_profile,dict) or owned_profile.get('portrait_runtime_sha')!=expected_runtime_sha:
        raise OperationFailure('RUNTIME_LOCK_MISMATCH')
    check_active();verify_installed_runtime(owned_lock,expected_runtime_sha);check_active()
    estimate=owned_profile.get('resource_estimate') if isinstance(owned_profile,dict) else None
    if (not isinstance(estimate,dict) or
            any(type(estimate.get(k)) is not int or not 1<=estimate[k]<=2**53-1
                for k in ('mask_scratch_bytes','material_scratch_bytes','output_scratch_bytes','scratch_bytes')) or
            type(mask_max_bytes) is not int or mask_max_bytes<1 or
            type(material_max_bytes) is not int or material_max_bytes<1):
        raise OperationFailure('CONFIG_INVALID')
    if sum(estimate[k] for k in ('mask_scratch_bytes','material_scratch_bytes','output_scratch_bytes'))>estimate['scratch_bytes']:
        raise OperationFailure('SCRATCH_ADMISSION_EXCEEDED')
    mask_max_bytes=min(mask_max_bytes,estimate['mask_scratch_bytes'])
    material_max_bytes=min(material_max_bytes,estimate['material_scratch_bytes'])
    source,donor,proof,sfaces,dfaces,target,guard,root,producer=map(bytes,
        (source_raw,donor_raw,confirmation_raw,source_faces_raw,donor_faces_raw,
         target_raw,protected_raw,root_raw,producer_params_raw))
    root_pin=root_ref.model_copy(deep=True);params_pin=producer_params_ref.model_copy(deep=True)
    vectors=None if vectors_raw is None else bytes(vectors_raw)
    def combined(p,r):
        check_active()
        if producer_authority(p.model_copy(deep=True),root_pin.model_copy(deep=True),
                              params_pin.model_copy(deep=True)) is not True:
            return False
        check_active()
        return authority(p.model_copy(deep=True),r) is True
    if not combined(owned,proof):raise ValueError('DONOR_AUTHORITY')
    source_image,donor_image=verify_donor_image_pair(owned,source,donor)
    geometry=verify_open_eyes_faces(owned,proof,sfaces,dfaces,
        (source_image.canvas.width,source_image.canvas.height),
        (donor_image.canvas.width,donor_image.canvas.height),min_face_edge_px)
    with read_verified_donor_manual_masks(owned,source,target,guard,root_pin,root,
            params_pin,producer,reader,vectors_raw=vectors,scratch_root=mask_scratch_root,
            max_bytes=mask_max_bytes,check_active=check_active,
            producer_authority=producer_authority) as material:
        planned_origin,planned_size=plan_donor_mask_patch(material[2],geometry.source_diagonal,
            check_active=check_active)
        if source_origin is None and patch_size is None:
            source_origin,patch_size=planned_origin,planned_size
        else:
            if (not isinstance(source_origin,tuple) or not isinstance(patch_size,tuple) or
                    len(source_origin)!=2 or len(patch_size)!=2 or
                    any(type(v) is not int or v<0 for v in source_origin) or
                    any(type(v) is not int or v<1 for v in patch_size)):
                raise ValueError('DONOR_MASK_PATCH')
            x,y=source_origin;w,h=patch_size;px,py=planned_origin;pw,ph=planned_size
            if x>px or y>py or x+w<px+pw or y+h<py+ph:
                raise ValueError('DONOR_MASK_PATCH_HALO')
        mask,protected=select_donor_mask_patch(material[2],material[3],source_origin,
            patch_size,check_active=check_active)
        required=admit_donor_portrait(owned_profile,source_image.canvas.width,source_image.canvas.height,
            donor_image.canvas.width,donor_image.canvas.height,*patch_size)
        if mask_max_bytes<required['mask_scratch_bytes'] or material_max_bytes<required['material_scratch_bytes']:
            raise OperationFailure('SCRATCH_ADMISSION_EXCEEDED')
        result=prepare_and_stage_open_eyes_patch(owned,source,donor,proof,sfaces,dfaces,
            reader,mask,protected,artifacts,scratch,source_origin=source_origin,
            min_face_edge_px=min_face_edge_px,material_scratch_root=material_scratch_root,
            material_max_bytes=material_max_bytes,check_active=check_active,
            authority=combined,alpha_mode=alpha_mode)
    verify_installed_runtime(owned_lock,expected_runtime_sha);check_active()
    return result


def prepare_and_stage_qualified_manual_open_eyes(*args,profile,report_raw,calibration_raw,
        qualification_policy,load_qualification_pin,check_active,**kwargs):
    """Qualified entry point; SQL lease/object authority is independently required.

    Derive face threshold from exact Calibration and repeat current qualification
    at every activity checkpoint used by the existing manual donor pipeline.
    """
    from .portrait_donor_qualification import verify_donor_qualification,donor_runtime_authority,donor_compiler_profile
    from photo_worker.contracts.base import parse_json
    if 'min_face_edge_px' in kwargs:
        raise ValueError('DONOR_CALIBRATION_THRESHOLD_OVERRIDE')
    owned_profile=parse_json(canonical_bytes(profile),1<<20)
    pin,calibration=verify_donor_qualification(owned_profile,bytes(report_raw),bytes(calibration_raw),
        qualification_policy,check_active=check_active)
    if kwargs.get('expected_runtime_sha')!=pin.runtime_lock_sha:
        raise OperationFailure('RUNTIME_LOCK_MISMATCH')
    qualification_authority=donor_runtime_authority(load_qualification_pin,frozen_pin=pin,check_active=check_active)
    def guarded_check():
        qualification_authority()
    guarded_check()
    return prepare_and_stage_manual_open_eyes(*args,profile=owned_profile,
        min_face_edge_px=calibration.min_face_edge_px,check_active=guarded_check,**kwargs)


def prepare_and_stage_leased_manual_open_eyes(database,lease,params_raw,*material_args,
        report_raw,calibration_raw,qualification_policy,load_qualification_pin,check_active,**kwargs):
    """Join exact leased Params/profile, qualification and manual donor execution.

    Current consent/source/producer callbacks remain required by the downstream
    pipeline; this entry point does not install or activate a worker operation.
    """
    from .portrait_donor_qualification import verify_donor_qualification,donor_runtime_authority,donor_compiler_profile
    from .portrait_donor_attempt import donor_attempt_checkpoint
    from photo_worker.contracts.base import parse_json
    if any(key in kwargs for key in ('profile','min_face_edge_px','authority')):
        raise ValueError('DONOR_LEASE_INPUT_OVERRIDE')
    if not isinstance(params_raw,bytes) or not 2<=len(params_raw)<=65536:
        raise ValueError('DONOR_PARAMS_INVALID')
    if not callable(check_active):raise ValueError('DONOR_AUTHORITY')
    check_active()
    owned_lease=deepcopy(lease)
    body=bytes(params_raw)
    from hashlib import sha256
    if (owned_lease.command.operation!='portrait.open_eyes' or owned_lease.command.operation_version!='1'
            or sha256(body).hexdigest()!=owned_lease.command.params.sha256):
        raise ValueError('DONOR_PARAMS_INVALID')
    params=PortraitOpenEyesParams.model_validate_json(body)
    if canonical_bytes(params)!=body:raise ValueError('DONOR_PARAMS_INVALID')
    profile=donor_compiler_profile(owned_lease.execution_profile)
    pin,calibration=verify_donor_qualification(profile,report_raw,calibration_raw,
        qualification_policy,check_active=check_active)
    if kwargs.get('expected_runtime_sha')!=pin.runtime_lock_sha:
        raise OperationFailure('RUNTIME_LOCK_MISMATCH')
    authority=donor_runtime_authority(load_qualification_pin,frozen_pin=pin,check_active=check_active)
    checkpoint=donor_attempt_checkpoint(database,owned_lease,body,authority,check_active)
    from .portrait_donor_source import donor_source_authority
    source_authority=donor_source_authority(database,owned_lease,body,checkpoint)
    checkpoint()
    result=prepare_and_stage_manual_open_eyes(params,*material_args,profile=profile,
        min_face_edge_px=calibration.min_face_edge_px,check_active=checkpoint,authority=source_authority,**kwargs)
    checkpoint()
    return result


def prepare_and_stage_sql_manual_open_eyes(database,lease,params_raw,source_raw,donor_raw,
        confirmation_raw,source_faces_raw,donor_faces_raw,target_raw,protected_raw,
        root_ref,root_raw,producer_params_ref,producer_params_raw,reader,artifacts,scratch,*,
        qualification_objects,bucket,check_active,**kwargs):
    """Resolve qualification from leased SQL and original object versions.

    Own direct documents before catalog/object callbacks; no caller qualification
    document or source-authority override is accepted by this entry point.
    """
    from .portrait_donor_qualification_loader import load_donor_qualification
    if any(key in kwargs for key in ('report_raw','calibration_raw','qualification_policy',
            'load_qualification_pin','profile','min_face_edge_px','authority','producer_authority')):
        raise ValueError('DONOR_LEASE_INPUT_OVERRIDE')
    if not callable(check_active):raise ValueError('DONOR_AUTHORITY')
    owned_lease=deepcopy(lease)
    raw_inputs=(params_raw,source_raw,donor_raw,confirmation_raw,source_faces_raw,
                donor_faces_raw,target_raw,protected_raw,root_raw,producer_params_raw)
    for i,value in enumerate(raw_inputs):
        maximum=65536 if i in (0,3) else 1<<20
        if not isinstance(value,(bytes,bytearray)) or not 2<=len(value)<=maximum:
            raise ValueError('DONOR_DOCUMENT_INVALID')
    body,source,donor,proof,sfaces,dfaces,target,guard,root,producer=map(bytes,raw_inputs)
    root_pin=root_ref.model_copy(deep=True);producer_pin=producer_params_ref.model_copy(deep=True)
    options=dict(kwargs)
    if options.get('vectors_raw') is not None:
        vectors=options['vectors_raw']
        if not isinstance(vectors,(bytes,bytearray)) or not 2<=len(vectors)<=1<<20:
            raise ValueError('DONOR_DOCUMENT_INVALID')
        options['vectors_raw']=bytes(vectors)
    from .portrait_donor_manual_authority import donor_manual_authority
    options['producer_authority']=donor_manual_authority(database,owned_lease,body,source,target,guard,
        root_pin,producer_pin,producer,check_active)
    check_active()
    report,calibration,policy,current_pin=load_donor_qualification(database,owned_lease,body,proof,
        qualification_objects,bucket,check_active=check_active)
    result=prepare_and_stage_leased_manual_open_eyes(database,owned_lease,body,source,donor,proof,
        sfaces,dfaces,target,guard,root_pin,root,producer_pin,producer,reader,artifacts,scratch,
        report_raw=report,calibration_raw=calibration,qualification_policy=policy,
        load_qualification_pin=current_pin,check_active=check_active,**options)
    current_pin();check_active()
    return result
