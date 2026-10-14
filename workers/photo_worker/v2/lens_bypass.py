"""Evidence-backed lens identity eligibility; never trust a worker bypass flag."""
from io import BytesIO
from hashlib import sha256
import numpy as np
from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import WorkingImage, MaskRef
from photo_worker.operations.optical.contracts import OpticalLensParams, ImageResult, ImageReport
from .adoption import ArtifactRef, AdoptionInvalid, MAX_JSON_BYTES, require_adoption_active


def _document(reader, ref, model, source, *, check_active=lambda:None):
    require_adoption_active(check_active)
    evidence=reader.evidence(ref)
    require_adoption_active(check_active)
    if evidence.ref!=ref or evidence.source!=source or evidence.media_type!='application/json':
        raise AdoptionInvalid('LENS_BYPASS_EVIDENCE_INVALID')
    raw=reader.read_json(ref,MAX_JSON_BYTES)
    require_adoption_active(check_active)
    if not isinstance(raw,bytes) or len(raw)>MAX_JSON_BYTES or len(raw)!=evidence.bytes or sha256(raw).hexdigest()!=ref.sha256:
        raise AdoptionInvalid('LENS_BYPASS_BYTES_INVALID')
    try:return model.model_validate_json(raw)
    except ValueError as error:raise AdoptionInvalid('LENS_BYPASS_CONTRACT_INVALID') from error


def _ref(value):return ArtifactRef(value.artifact_id,value.sha256)


def verify_zero_npy_mask(reader, ref, shape, *, check_active, error_code):
    """Prove canonical float32 zero mask from the pinned physical object bytes."""
    header=BytesIO()
    np.lib.format.write_array_header_1_0(header,{'descr':'<f4','fortran_order':False,
        'shape':shape})
    expected_header=header.getvalue()
    size=len(expected_header)+shape[0]*shape[1]*4
    require_adoption_active(check_active)
    evidence=reader.evidence(ref)
    require_adoption_active(check_active)
    if (evidence.source!='output' or evidence.role!='mask_pixels' or
            evidence.media_type!='application/x-npy' or evidence.bytes!=size or
            evidence.ref!=ref):
        raise AdoptionInvalid(error_code)
    stream=getattr(reader,'iter_bytes',None)
    if not callable(stream):raise AdoptionInvalid('ZERO_MASK_READER_REQUIRED')
    digest=sha256();offset=0
    iterator=iter(stream(ref,size))
    try:
        for block in iterator:
            require_adoption_active(check_active)
            if not isinstance(block,bytes) or not block or len(block)>65536 or offset+len(block)>size:
                raise AdoptionInvalid(error_code)
            header_bytes=min(len(block),max(0,len(expected_header)-offset))
            if (block[:header_bytes]!=expected_header[offset:offset+header_bytes]
                    or any(block[header_bytes:])):
                raise AdoptionInvalid(error_code)
            digest.update(block);offset+=len(block)
        require_adoption_active(check_active)
        if offset!=size or digest.hexdigest()!=ref.sha256:
            raise AdoptionInvalid(error_code)
    finally:
        close=getattr(iterator,'close',None)
        if close is not None:close()
    require_adoption_active(check_active)


def verify_lens_identity(reader, params_ref, root_ref, *, check_active=lambda:None):
    """Return source ref only for exact identity result with canonical zero NPY.

    Params ref must come from authoritative task metadata, never from the worker
    result. The reader must enforce task/attempt/fence bindings on every ref.
    This function alone neither changes required-role policies nor adopts output.
    """
    require_adoption_active(check_active)
    params=_document(reader,params_ref,OpticalLensParams,'input',check_active=check_active)
    if params.strength!=0:raise AdoptionInvalid('LENS_BYPASS_NOT_IDENTITY')
    result=_document(reader,root_ref,ImageResult,'output',check_active=check_active)
    if result.working_image!=params.base.source or result.generated_mask is not None:
        raise AdoptionInvalid('LENS_BYPASS_NOT_IDENTITY')
    image=_document(reader,_ref(params.base.source),WorkingImage,'input',check_active=check_active)
    if image.canvas.width*image.canvas.height>200_000_000:
        raise AdoptionInvalid('LENS_BYPASS_PIXEL_LIMIT')
    if params.base.canvas_hash!=canonical_hash(image.canvas):
        raise AdoptionInvalid('LENS_BYPASS_SOURCE_MISMATCH')
    report=_document(reader,_ref(result.report),ImageReport,'output',check_active=check_active)
    if (report.operation!='optical.lens' or report.status!='pass' or report.reason is not None or
            report.skipped_count!=0 or report.outside_mask_max_error!=0 or report.clipping_fraction!=0 or
            report.identity is not None or report.transform is not None or
            report.registrations or report.sources!=[params.base]):
        raise AdoptionInvalid('LENS_BYPASS_REPORT_INVALID')
    mask=_document(reader,_ref(result.changed_mask),MaskRef,'output',check_active=check_active)
    if (mask.canvas!=image.canvas or mask.transform_chain!=image.transform_chain or
            mask.selector!='changed' or mask.object_id is not None or mask.protected or
            mask.confidence!=1 or mask.model_lock_id is not None):
        raise AdoptionInvalid('LENS_BYPASS_MASK_INVALID')
    verify_zero_npy_mask(reader,_ref(mask.artifact),(image.canvas.height,image.canvas.width),
        check_active=check_active,error_code='LENS_BYPASS_MASK_NONZERO')
    return _ref(params.base.source)
