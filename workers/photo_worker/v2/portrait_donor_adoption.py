"""Independent donor result metadata proof; pixel replay and SQL remain required."""
from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.contracts.portrait_open_eyes import PortraitOpenEyesParams
from photo_worker.operations.optical.contracts import ImageResult,ImageReport
from .adoption import AdoptionInvalid
from .lens_bypass import _document as _read_document,_ref


def _document(reader,ref,model,source):
    value=_read_document(reader,ref,model,source)
    if canonical_hash(value)!=ref.sha256:
        raise AdoptionInvalid('DONOR_ADOPTION_DOCUMENT_NONCANONICAL')
    return value


def verify_donor_result_metadata(reader,params_ref,root_ref,*,check_active):
    if not callable(check_active):raise ValueError('DONOR_AUTHORITY')
    check_active()
    params=_document(reader,params_ref,PortraitOpenEyesParams,'input')
    source=_document(reader,_ref(params.base.source),WorkingImage,'input')
    result=_document(reader,root_ref,ImageResult,'output')
    if result.changed_mask is None or result.generated_mask is not None:
        raise AdoptionInvalid('DONOR_ADOPTION_METADATA_INVALID')
    image=_document(reader,_ref(result.working_image),WorkingImage,'output')
    report=_document(reader,_ref(result.report),ImageReport,'output')
    changed=_document(reader,_ref(result.changed_mask),MaskRef,'output')
    refs=[root_ref,_ref(result.working_image),_ref(image.pixels),_ref(result.report),
          _ref(result.changed_mask),_ref(changed.artifact)]
    if len({ref.artifact_id for ref in refs})!=6:
        raise AdoptionInvalid('DONOR_ADOPTION_METADATA_INVALID')
    roles=('document','working_image','pixels','report','mask','mask_pixels')
    for ref,role in zip(refs,roles):
        check_active();evidence=reader.evidence(ref)
        if evidence.ref!=ref or evidence.source!='output' or evidence.role!=role:
            raise AdoptionInvalid('DONOR_ADOPTION_METADATA_INVALID')
    expected=source.model_copy(update={'pixels':image.pixels},deep=True)
    if (canonical_hash(image)!=canonical_hash(expected) or params.base.canvas_hash!=canonical_hash(source.canvas)
            or changed.canvas!=source.canvas or changed.transform_chain!=source.transform_chain
            or changed.selector!='changed' or changed.protected is not False
            or changed.object_id is not None or changed.model_lock_id is not None or changed.confidence!=1
            or report.operation!='portrait.open_eyes' or report.status!='review'
            or report.reason not in ('donor_review','COLOR_MATCH_UNAVAILABLE')
            or report.sources!=[params.base,params.donor] or report.identity is not None
            or report.transform is not None or report.registrations or report.skipped_count!=0
            or report.outside_mask_max_error!=0 or report.clipping_fraction!=0):
        raise AdoptionInvalid('DONOR_ADOPTION_METADATA_INVALID')
    check_active()
    return params,result


def verify_donor_pixel_tiles(source,output,target,protected,changed,*,strength,check_active):
    """Exact bit preservation and binary changed-mask proof in bounded tiles."""
    import numpy as np
    if (not callable(check_active) or isinstance(strength,bool) or not isinstance(strength,(int,float))
            or not 0<=strength<=1 or source.dtype!=np.float32 or output.dtype!=np.float32
            or source.ndim!=3 or source.shape[-1]!=3 or source.shape!=output.shape
            or min(source.shape[:2])<1 or source.shape[0]*source.shape[1]>200_000_000
            or any(value.dtype!=np.float32 or value.shape!=source.shape[:2] for value in (target,protected,changed))):
        raise AdoptionInvalid('DONOR_ADOPTION_ARRAY_INVALID')
    h,w=source.shape[:2]
    for y in range(0,h,512):
        for x in range(0,w,512):
            check_active();part=np.s_[y:y+512,x:x+512]
            before,after=source[part],output[part]
            mask,guard,actual=target[part],protected[part],changed[part]
            if (not np.isfinite(before).all() or not np.isfinite(after).all()
                    or any(not np.isfinite(v).all() or np.any(v<0) or np.any(v>1) for v in (mask,guard,actual))):
                raise AdoptionInvalid('DONOR_ADOPTION_ARRAY_INVALID')
            expected=np.any(before.view(np.uint32)!=after.view(np.uint32),axis=2)
            if not np.array_equal(actual,expected.astype(np.float32)):
                raise AdoptionInvalid('DONOR_ADOPTION_CHANGED_MASK_INVALID')
            if np.any(expected & ((mask==0)|(guard>0)|(strength==0))):
                raise AdoptionInvalid('DONOR_ADOPTION_OUTSIDE_MASK')
    check_active()


def verify_donor_output_arrays(reader,params_ref,root_ref,*,scratch_root,max_bytes,check_active):
    """Physical invariant proof; donor replay/current SQL authority still required."""
    from .array_evidence import ArrayEvidenceSession
    params,result=verify_donor_result_metadata(reader,params_ref,root_ref,check_active=check_active)
    source=_document(reader,_ref(params.base.source),WorkingImage,'input')
    output=_document(reader,_ref(result.working_image),WorkingImage,'output')
    target=_document(reader,_ref(params.eyes_mask),MaskRef,'input')
    guard=_document(reader,_ref(params.protected),MaskRef,'input')
    changed=_document(reader,_ref(result.changed_mask),MaskRef,'output')
    if (target.canvas!=source.canvas or guard.canvas!=source.canvas
            or target.transform_chain!=source.transform_chain or guard.transform_chain!=source.transform_chain
            or target.protected is not False or guard.protected is not True):
        raise AdoptionInvalid('DONOR_ADOPTION_METADATA_INVALID')
    shape=(source.canvas.height,source.canvas.width)
    if shape[0]*shape[1]>200_000_000 or source.dtype!='float32':
        raise AdoptionInvalid('DONOR_ADOPTION_ARRAY_INVALID')
    with ArrayEvidenceSession(reader,scratch_root,max_bytes,check_active) as arrays:
        before=arrays.array(_ref(source.pixels),(*shape,3),source='input',legacy_input_npy=True)
        after=arrays.array(_ref(output.pixels),(*shape,3),source='output',role='pixels')
        mask=arrays.array(_ref(target.artifact),shape,source='input',unit=True,legacy_input_npy=True)
        protected=arrays.array(_ref(guard.artifact),shape,source='input',unit=True,legacy_input_npy=True)
        changes=arrays.array(_ref(changed.artifact),shape,source='output',role='mask_pixels',unit=True)
        verify_donor_pixel_tiles(before,after,mask,protected,changes,strength=params.strength,check_active=check_active)
    return params,result
