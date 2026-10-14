"""Negative metadata proof; qualified pixel replay/live SQL are still required."""
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.contracts.optical_negative import OpticalNegativeParams
from photo_worker.operations.optical.contracts import ImageResult,ImageReport
from photo_worker.operations.optical.negative_result import build_negative_metadata
from .adoption import AdoptionInvalid
from .lens_bypass import _document as read_document,_ref


def verify_negative_result_metadata(reader,params_ref,root_ref,*,check_active):
    if not callable(check_active):raise ValueError('NEGATIVE_ACTIVITY_REQUIRED')
    def document(ref,model,origin):
        check_active();value=read_document(reader,ref,model,origin)
        if canonical_hash(value)!=ref.sha256:raise AdoptionInvalid('NEGATIVE_DOCUMENT_NONCANONICAL')
        check_active();return value
    p=document(params_ref,OpticalNegativeParams,'input')
    source=document(_ref(p.base.source),WorkingImage,'input')
    result=document(root_ref,ImageResult,'output')
    image=document(_ref(result.working_image),WorkingImage,'output')
    report=document(_ref(result.report),ImageReport,'output')
    mask=document(_ref(result.changed_mask),MaskRef,'output')
    refs=(root_ref,_ref(result.working_image),_ref(result.report),_ref(result.changed_mask),_ref(image.pixels),_ref(mask.artifact))
    roles=('document','working_image','report','mask','pixels','mask_pixels')
    if len({r.artifact_id for r in refs})!=6:raise AdoptionInvalid('NEGATIVE_OUTPUT_ALIAS')
    for ref,role in zip(refs,roles):
        check_active();evidence=reader.evidence(ref)
        if evidence.ref!=ref or evidence.source!='output' or evidence.role!=role:
            raise AdoptionInvalid('NEGATIVE_OUTPUT_EVIDENCE_INVALID')
    expected,expected_report=build_negative_metadata(p,canonical_bytes(source),image.pixels,
        clipping_fraction=report.clipping_fraction)
    expected_mask=MaskRef(artifact=mask.artifact,canvas=source.canvas,transform_chain=source.transform_chain,
        object_id=None,selector='changed',confidence=1.,model_lock_id=None,protected=False)
    if (result.generated_mask is not None or canonical_bytes(image)!=canonical_bytes(expected)
            or canonical_bytes(report)!=canonical_bytes(expected_report)
            or canonical_bytes(mask)!=canonical_bytes(expected_mask)):
        raise AdoptionInvalid('NEGATIVE_METADATA_MISMATCH')
    check_active();return p,result


def verify_negative_pixel_replay(reader,params_ref,root_ref,*,scratch_root,max_bytes,
        ram_max_bytes,epsilon,check_active):
    """Independent original-input replay; live qualification/SQL still separate."""
    import numpy as np
    from .array_evidence import ArrayEvidenceSession
    from photo_worker.contracts.optical_negative import NegativeCalibration
    from photo_worker.operations.optical.negative import invert_pinned_negative_tile
    p,result=verify_negative_result_metadata(reader,params_ref,root_ref,check_active=check_active)
    source=read_document(reader,_ref(p.base.source),WorkingImage,'input')
    output=read_document(reader,_ref(result.working_image),WorkingImage,'output')
    mask=read_document(reader,_ref(result.changed_mask),MaskRef,'output')
    calibration=read_document(reader,_ref(p.calibration),NegativeCalibration,'input')
    calibration_raw=canonical_bytes(calibration)
    if canonical_hash(calibration)!=p.calibration.sha256:raise AdoptionInvalid('NEGATIVE_CALIBRATION_NONCANONICAL')
    h,w=source.canvas.height,source.canvas.width;pixels=h*w
    owners=28+(4 if source.alpha is not None else 0)
    if (pixels>200_000_000 or type(ram_max_bytes) is not int or not 1<=ram_max_bytes<=2**53-1
            or type(max_bytes) is not int or not 1<=max_bytes<=2**53-1):
        raise AdoptionInvalid('NEGATIVE_RESOURCE_INVALID')
    if ram_max_bytes<owners*pixels+128*min(pixels,512*512)+65536:
        raise AdoptionInvalid('NEGATIVE_RAM_EXCEEDED')
    if max_bytes<owners*pixels+32768:raise AdoptionInvalid('NEGATIVE_SCRATCH_REQUIRED')
    with ArrayEvidenceSession(reader,scratch_root,max_bytes,check_active) as arrays:
        before=arrays.array(_ref(source.pixels),(h,w,3),source='input',legacy_input_npy=True)
        after=arrays.array(_ref(output.pixels),(h,w,3),source='output',role='pixels')
        changed=arrays.array(_ref(mask.artifact),(h,w),source='output',role='mask_pixels',unit=True)
        if source.alpha is not None:
            arrays.array(_ref(source.alpha),(h,w),source='input',unit=True,legacy_input_npy=True)
        for y in range(0,h,512):
            for x in range(0,w,512):
                check_active();tile=np.s_[y:y+512,x:x+512]
                expected=invert_pinned_negative_tile(before[tile],p,calibration_raw,
                    input_icc_sha=source.color.icc_sha256,epsilon=epsilon)
                expected_mask=np.any(expected.view(np.uint32)!=before[tile].view(np.uint32),axis=-1)
                if (not np.array_equal(expected.view(np.uint32),after[tile].view(np.uint32))
                        or not np.array_equal(expected_mask,changed[tile])):
                    raise AdoptionInvalid('NEGATIVE_REPLAY_MISMATCH')
        from photo_worker.operations.optical.negative_statistics import negative_clipping_fraction
        report=read_document(reader,_ref(result.report),ImageReport,'output')
        if report.clipping_fraction!=negative_clipping_fraction(after,check_active=check_active):
            raise AdoptionInvalid('NEGATIVE_STATISTICS_MISMATCH')
        check_active()
    return p,result


def verify_pinned_negative_replay(reader,params_ref,root_ref,*,runtime_lock,runtime_sha,
        scratch_root,max_bytes,ram_max_bytes,check_active):
    """Verify installed implementation and use its pinned epsilon for replay."""
    from photo_worker.operations.optical.negative_runtime_lock import NegativeRuntimeLock,verify_installed_runtime
    owned=NegativeRuntimeLock.model_validate_json(canonical_bytes(runtime_lock))
    check_active();verify_installed_runtime(owned,runtime_sha);check_active()
    result=verify_negative_pixel_replay(reader,params_ref,root_ref,scratch_root=scratch_root,
        max_bytes=max_bytes,ram_max_bytes=ram_max_bytes,epsilon=owned.epsilon,check_active=check_active)
    check_active();verify_installed_runtime(owned,runtime_sha);check_active()
    return result
