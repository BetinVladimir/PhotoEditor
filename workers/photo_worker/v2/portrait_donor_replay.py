"""Independent original-input donor reconstruction before relational adoption."""
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.donor_confirmation import DonorConfirmation
from photo_worker.contracts.manual_mask import RasterizedMask,MaskRasterParams
from photo_worker.operations.session.contracts import FaceObservations
from photo_worker.operations.optical.contracts import ImageReport
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.operations.portrait.donor_runtime_lock import verify_installed_runtime
from .portrait_donor_adoption import verify_donor_output_arrays,verify_donor_result_metadata,_document,_ref
from .portrait_donor_evidence import verify_open_eyes_faces
from .portrait_donor_manual import read_verified_donor_manual_masks
from .portrait_donor_mask_patch import plan_donor_mask_patch,select_donor_mask_patch
from .portrait_open_eyes_material import prepare_open_eyes_material_patch
from .array_evidence import ArrayEvidenceSession
from .adoption import AdoptionInvalid


def compare_donor_replay(source,output,changed,patch,origin,*,check_active):
    """Compare source outside patch and independently reconstructed RGB inside."""
    x,y=origin;ph,pw=patch.pixels.shape[:2];h,w=source.shape[:2]
    if (output.shape!=source.shape or changed.shape!=(h,w) or x<0 or y<0
            or x+pw>w or y+ph>h):raise AdoptionInvalid('DONOR_REPLAY_INVALID')
    for ty in range(0,h,512):
        for tx in range(0,w,512):
            check_active();th,tw=min(512,h-ty),min(512,w-tx)
            expected=source[ty:ty+th,tx:tx+tw].copy()
            expected_changed=np.zeros((th,tw),np.float32)
            ix0,iy0=max(tx,x),max(ty,y);ix1,iy1=min(tx+tw,x+pw),min(ty+th,y+ph)
            if ix1>ix0 and iy1>iy0:
                a=np.s_[iy0-ty:iy1-ty,ix0-tx:ix1-tx]
                b=np.s_[iy0-y:iy1-y,ix0-x:ix1-x]
                expected[a]=patch.pixels[b];expected_changed[a]=patch.changed_mask[b]
            if (not np.array_equal(expected.view(np.uint32),output[ty:ty+th,tx:tx+tw].view(np.uint32))
                    or not np.array_equal(expected_changed,changed[ty:ty+th,tx:tx+tw])):
                raise AdoptionInvalid('DONOR_REPLAY_MISMATCH')
    check_active()


def verify_donor_reconstruction(reader,params_ref,root_ref,*,manual_root_ref,producer_ref,
        min_face_edge_px,runtime_lock,runtime_sha,resource_profile,ram_max_bytes,mask_scratch_root,mask_max_bytes,
        material_scratch_root,material_max_bytes,output_scratch_root,output_max_bytes,
        authority,producer_authority,check_active,alpha_mode='require_float32'):
    """Trusted caller derives Calibration threshold and repeated current authority.

    This reconstructs original manual masks and deterministic donor algorithm;
    it neither grants SQL permissions nor commits/adopts a submitted result.
    """
    if not all(callable(c) for c in (authority,producer_authority,check_active)):
        raise ValueError('DONOR_AUTHORITY')
    from copy import deepcopy
    profile=deepcopy(resource_profile)
    estimate=profile.get('resource_estimate') if isinstance(profile,dict) else None
    ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if (type(ram_max_bytes) is not int or not 1<=ram_max_bytes<=2**53-1
            or type(ram) is not int or not 1<=ram<=2**53-1):
        raise AdoptionInvalid('DONOR_ADOPTION_RESOURCE_INVALID')
    if ram>ram_max_bytes:
        raise AdoptionInvalid('DONOR_ADOPTION_RAM_REQUIRED')
    check_active();verify_installed_runtime(runtime_lock,runtime_sha)
    params,result=verify_donor_result_metadata(reader,params_ref,root_ref,check_active=check_active)
    def body(ref,model):return canonical_bytes(_document(reader,_ref(ref),model,'input'))
    source=body(params.base.source,WorkingImage);donor=body(params.donor.source,WorkingImage)
    proof=body(params.confirmation,DonorConfirmation)
    sfaces=body(params.source_faces,FaceObservations);dfaces=body(params.donor_faces,FaceObservations)
    target=body(params.eyes_mask,MaskRef);guard=body(params.protected,MaskRef)
    root=body(manual_root_ref,RasterizedMask);producer=body(producer_ref,MaskRasterParams)
    from photo_worker.contracts.manual_mask import ManualMask
    p=MaskRasterParams.model_validate_json(producer)
    vectors=None if p.vectors is None else body(p.vectors,ManualMask)
    simage=WorkingImage.model_validate_json(source);dimage=WorkingImage.model_validate_json(donor)
    # Reject insufficient disk budgets before any NPY stream or mapping.
    source_count=simage.canvas.width*simage.canvas.height
    donor_count=dimage.canvas.width*dimage.canvas.height
    if (any(type(n) is not int or n<1 for n in
            (mask_max_bytes,material_max_bytes,output_max_bytes))
            or mask_max_bytes<8*source_count+8192
            or material_max_bytes<20*(source_count+donor_count)+24576
            or output_max_bytes<36*source_count+32768):
        raise AdoptionInvalid('DONOR_ADOPTION_SCRATCH_REQUIRED')
    verify_donor_output_arrays(reader,params_ref,root_ref,
        scratch_root=output_scratch_root,max_bytes=output_max_bytes,check_active=check_active)
    geometry=verify_open_eyes_faces(params,proof,sfaces,dfaces,
        (simage.canvas.width,simage.canvas.height),(dimage.canvas.width,dimage.canvas.height),min_face_edge_px)
    with read_verified_donor_manual_masks(params,source,target,guard,manual_root_ref,root,
            producer_ref,producer,reader,vectors_raw=vectors,scratch_root=mask_scratch_root,
            max_bytes=mask_max_bytes,check_active=check_active,producer_authority=producer_authority) as masks:
        origin,size=plan_donor_mask_patch(masks[2],geometry.source_diagonal,check_active=check_active)
        mask,protected=select_donor_mask_patch(masks[2],masks[3],origin,size,check_active=check_active)
        from photo_worker.operations.portrait.donor_resources import admit_donor_portrait
        required=admit_donor_portrait(profile,simage.canvas.width,simage.canvas.height,
            dimage.canvas.width,dimage.canvas.height,*size)
        if (mask_max_bytes<required['mask_scratch_bytes']
                or material_max_bytes<required['material_scratch_bytes']
                or output_max_bytes<36*simage.canvas.width*simage.canvas.height+32768):
            raise AdoptionInvalid('DONOR_ADOPTION_SCRATCH_REQUIRED')
        with prepare_open_eyes_material_patch(params,source,donor,proof,sfaces,dfaces,reader,mask,protected,
                source_origin=origin,min_face_edge_px=min_face_edge_px,scratch_root=material_scratch_root,
                max_bytes=material_max_bytes,check_active=check_active,authority=authority,alpha_mode=alpha_mode) as (patch,material):
            output=_document(reader,_ref(result.working_image),WorkingImage,'output')
            changed=_document(reader,_ref(result.changed_mask),MaskRef,'output')
            shape=(simage.canvas.height,simage.canvas.width)
            with ArrayEvidenceSession(reader,output_scratch_root,output_max_bytes,check_active) as arrays:
                rgb=arrays.array(_ref(output.pixels),(*shape,3),source='output',role='pixels')
                changes=arrays.array(_ref(changed.artifact),shape,source='output',role='mask_pixels',unit=True)
                compare_donor_replay(material.source_rgb,rgb,changes,patch,origin,check_active=check_active)
            report=_document(reader,_ref(result.report),ImageReport,'output')
            if report.reason!=(patch.color.warning or 'donor_review'):
                raise AdoptionInvalid('DONOR_REPLAY_REPORT_MISMATCH')
    verify_installed_runtime(runtime_lock,runtime_sha);check_active()
    return params,result
