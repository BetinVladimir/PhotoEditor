"""Verify nonzero lens result semantics and bounded, version-pinned NPY bytes."""
from hashlib import sha256
from io import BytesIO
import numpy as np

from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import WorkingImage, MaskRef, DenseWarpTransform
from photo_worker.operations.optical.contracts import OpticalLensParams, ImageResult, ImageReport
from photo_worker.operations.optical.result import lens_working_image
from .adoption import AdoptionInvalid
from .lens_bypass import _document, _ref


def verify_lens_array(reader, ref, shape, role, *, unit=False, binary=False, check_active=lambda:None):
    header=BytesIO()
    np.lib.format.write_array_header_1_0(header,{'descr':'<f4','fortran_order':False,'shape':shape})
    expected_header=header.getvalue()
    size=len(expected_header)+int(np.prod(shape))*4
    evidence=reader.evidence(ref)
    if (evidence.ref!=ref or evidence.source!='output' or evidence.role!=role or
            evidence.media_type!='application/x-npy' or evidence.bytes!=size):
        raise AdoptionInvalid('LENS_ARRAY_EVIDENCE_INVALID')
    stream=getattr(reader,'iter_bytes',None)
    if not callable(stream):raise AdoptionInvalid('LENS_ARRAY_READER_REQUIRED')
    digest=sha256();offset=0;pending=b''
    iterator=iter(stream(ref,size))
    try:
        for block in iterator:
            check_active()
            if not isinstance(block,bytes) or not block or len(block)>65536 or offset+len(block)>size:
                raise AdoptionInvalid('LENS_ARRAY_STREAM_INVALID')
            digest.update(block)
            head=min(len(block),max(0,len(expected_header)-offset))
            if block[:head]!=expected_header[offset:offset+head]:
                raise AdoptionInvalid('LENS_ARRAY_HEADER_INVALID')
            offset+=len(block)
            pending+=block[head:]
            aligned=len(pending)//4*4
            values=np.frombuffer(pending[:aligned],dtype='<f4')
            if not np.isfinite(values).all() or (unit and ((values<0).any() or (values>1).any())):
                raise AdoptionInvalid('LENS_ARRAY_VALUES_INVALID')
            if binary and ((values!=0)&(values!=1)).any():
                raise AdoptionInvalid('LENS_ARRAY_VALUES_INVALID')
            pending=pending[aligned:]
        if offset!=size or pending or digest.hexdigest()!=ref.sha256:
            raise AdoptionInvalid('LENS_ARRAY_BYTES_INVALID')
    finally:
        close=getattr(iterator,'close',None)
        if close is not None:close()


def verify_lens_correction(reader, params_ref, root_ref):
    params=_document(reader,params_ref,OpticalLensParams,'input')
    if params.strength==0:raise AdoptionInvalid('LENS_CORRECTION_IDENTITY')
    source=_document(reader,_ref(params.base.source),WorkingImage,'input')
    if source.canvas.width*source.canvas.height>200_000_000 or source.dtype!='float32' or params.base.canvas_hash!=canonical_hash(source.canvas):
        raise AdoptionInvalid('LENS_CORRECTION_SOURCE_INVALID')
    result=_document(reader,root_ref,ImageResult,'output')
    image=_document(reader,_ref(result.working_image),WorkingImage,'output')
    if result.generated_mask is not None or image.alpha is None or not image.transform_chain.steps:
        raise AdoptionInvalid('LENS_CORRECTION_GEOMETRY_INVALID')
    step=image.transform_chain.steps[-1]
    if not isinstance(step,DenseWarpTransform) or step.pixel_centers!='half_pixel':
        raise AdoptionInvalid('LENS_CORRECTION_GEOMETRY_INVALID')
    expected=lens_working_image(source,image.pixels,image.alpha,step.inverse_field,strength=params.strength)
    if image!=expected:raise AdoptionInvalid('LENS_CORRECTION_GEOMETRY_INVALID')
    mask=_document(reader,_ref(result.changed_mask),MaskRef,'output')
    if (mask.canvas!=image.canvas or mask.transform_chain!=image.transform_chain or
            mask.selector!='changed' or mask.object_id is not None or mask.protected or
            mask.confidence!=1 or mask.model_lock_id is not None):
        raise AdoptionInvalid('LENS_CORRECTION_MASK_INVALID')
    report=_document(reader,_ref(result.report),ImageReport,'output')
    if (report.operation!='optical.lens' or report.skipped_count!=0 or
            report.outside_mask_max_error!=0 or report.clipping_fraction!=0 or
            report.identity is not None or report.transform is not None or
            report.registrations or report.sources!=[params.base] or
            (report.status,report.reason) not in (('pass',None),('review','uncovered_area'))):
        raise AdoptionInvalid('LENS_CORRECTION_REPORT_INVALID')
    shape=(image.canvas.height,image.canvas.width)
    verify_lens_array(reader,_ref(image.pixels),(*shape,3),'pixels')
    verify_lens_array(reader,_ref(image.alpha),shape,'alpha',unit=True)
    verify_lens_array(reader,_ref(step.inverse_field),(*shape,2),'warp_field')
    verify_lens_array(reader,_ref(mask.artifact),shape,'mask_pixels',unit=True,binary=True)
