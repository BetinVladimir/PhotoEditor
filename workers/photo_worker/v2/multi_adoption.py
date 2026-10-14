"""Multi-image metadata proof; pixel replay and live SQL authority are mandatory."""
from dataclasses import dataclass
from math import isfinite
import numpy as np
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.contracts.optical_multi import OpticalPanoramaParams,OpticalFocusStackParams
from photo_worker.operations.optical.contracts import ImageResult,ImageReport
from photo_worker.operations.optical.multi_lineage import RegisteredTransform,compile_multi_lineage
from photo_worker.operations.optical.multi_result import multi_image_report
from .adoption import AdoptionInvalid
from .lens_bypass import _document,_ref


@dataclass(frozen=True)
class MultiMetadata:
    params:object
    sources:tuple
    result:ImageResult
    image:WorkingImage
    changed:MaskRef
    report:ImageReport
    registrations:tuple
    motion_fraction:float
    registration_inconsistent:bool


def verify_multi_result_metadata(reader,params_ref,root_ref,*,operation,max_pixels,check_active):
    if (operation not in ('optical.panorama','optical.focus_stack') or not callable(check_active)
            or type(max_pixels) is not int or not 1<=max_pixels<=200000000):
        raise ValueError('MULTI_ADOPTION_AUTHORITY_INVALID')
    def document(ref,model,origin):
        check_active();value=_document(reader,ref,model,origin)
        if canonical_hash(value)!=ref.sha256:raise AdoptionInvalid('MULTI_DOCUMENT_NONCANONICAL')
        return value
    model=OpticalPanoramaParams if operation=='optical.panorama' else OpticalFocusStackParams
    params=document(params_ref,model,'input')
    if params.max_pixels>max_pixels:raise AdoptionInvalid('MULTI_PIXEL_LIMIT')
    sources=tuple(document(_ref(binding.source),WorkingImage,'input') for binding in params.frames)
    result=document(root_ref,ImageResult,'output')
    if result.generated_mask is not None:raise AdoptionInvalid('MULTI_GENERATED_CONTENT')
    image=document(_ref(result.working_image),WorkingImage,'output')
    report=document(_ref(result.report),ImageReport,'output')
    changed=document(_ref(result.changed_mask),MaskRef,'output')
    if image.alpha is None or len(report.registrations)!=len(sources):raise AdoptionInvalid('MULTI_METADATA_INVALID')
    registrations=tuple(document(_ref(ref),RegisteredTransform,'output') for ref in report.registrations)
    shape=(image.canvas.height,image.canvas.width)
    if shape[0]*shape[1]>params.max_pixels:raise AdoptionInvalid('MULTI_PIXEL_LIMIT')
    matrices=[np.asarray(value.homography,np.float64).reshape(3,3) for value in registrations]
    fields=[value.inverse_field for value in registrations]
    cylindrical=operation=='optical.panorama' and params.projection=='cylindrical'
    if any((field is not None)!=cylindrical for field in fields):raise AdoptionInvalid('MULTI_PROJECTION_MISMATCH')
    if operation=='optical.focus_stack':
        reference=sources[params.reference_ordinal]
        if shape!=(reference.canvas.height,reference.canvas.width) or not np.array_equal(matrices[params.reference_ordinal],np.eye(3)):
            raise AdoptionInvalid('MULTI_GEOMETRY_MISMATCH')
    expected_image,expected_registrations=compile_multi_lineage(sources,params.frames,matrices,image.pixels,image.alpha,
        operation=operation,reference=params.reference_ordinal,output_shape=shape,inverse_fields=fields)
    if canonical_bytes(image)!=canonical_bytes(expected_image) or any(canonical_bytes(a)!=canonical_bytes(b) for a,b in zip(registrations,expected_registrations)):
        raise AdoptionInvalid('MULTI_GEOMETRY_MISMATCH')
    prefix=operation.removeprefix('optical.').upper()+'_REVIEW;motion_fraction='
    reason=report.reason or '';suffix=';REGISTRATION_INCONSISTENT'
    inconsistent=reason.endswith(suffix)
    numeric=reason[len(prefix):-len(suffix)] if inconsistent else reason[len(prefix):]
    try:motion=float(numeric)
    except ValueError:raise AdoptionInvalid('MULTI_REPORT_INVALID') from None
    if not reason.startswith(prefix) or not isfinite(motion) or not 0<=motion<=1:
        raise AdoptionInvalid('MULTI_REPORT_INVALID')
    try:
        expected_report=multi_image_report(operation.removeprefix('optical.'),params.frames,report.registrations,
            motion_fraction=motion,registration_inconsistent=inconsistent,
            clipping_fraction=report.clipping_fraction)
    except ValueError as error:
        raise AdoptionInvalid('MULTI_REPORT_INVALID') from error
    expected_mask=MaskRef(artifact=changed.artifact,canvas=image.canvas,transform_chain=image.transform_chain,
        object_id=None,selector='changed',confidence=1.,model_lock_id=None,protected=False)
    if canonical_bytes(report)!=canonical_bytes(expected_report) or canonical_bytes(changed)!=canonical_bytes(expected_mask):
        raise AdoptionInvalid('MULTI_METADATA_INVALID')
    refs=[root_ref,_ref(result.working_image),_ref(result.report),_ref(result.changed_mask),_ref(image.pixels),_ref(image.alpha),_ref(changed.artifact)]
    roles=['document','working_image','report','mask','pixels','alpha','mask_pixels']
    refs.extend(_ref(ref) for ref in report.registrations);roles.extend(['registration']*len(registrations))
    refs.extend(_ref(ref) for ref in fields if ref is not None);roles.extend(['warp_field']*sum(ref is not None for ref in fields))
    if len({ref.artifact_id for ref in refs})!=len(refs):raise AdoptionInvalid('MULTI_OUTPUT_ALIAS')
    for ref,role in zip(refs,roles):
        check_active();evidence=reader.evidence(ref)
        if evidence.ref!=ref or evidence.source!='output' or evidence.role!=role:
            raise AdoptionInvalid('MULTI_OUTPUT_EVIDENCE_INVALID')
    check_active()
    return MultiMetadata(params,sources,result,image,changed,report,registrations,motion,inconsistent)
