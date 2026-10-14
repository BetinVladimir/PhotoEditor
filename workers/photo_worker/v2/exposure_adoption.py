"""Document binding for approved exposure outputs; pixel replay is separate."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.operations.semantic.exposure_source import bind_local_exposure_source
from .adoption import ArtifactRef, AdoptionInvalid, MAX_JSON_BYTES


def verify_exposure_documents(reader, params_ref, root_ref, *, check_active):
    def document(ref, model, source, role=None):
        check_active()
        evidence = reader.evidence(ref)
        if (evidence.ref != ref or evidence.source != source
                or evidence.media_type != 'application/json'
                or (role is not None and evidence.role != role)):
            raise AdoptionInvalid('SEMANTIC_EXPOSURE_EVIDENCE_INVALID')
        raw = reader.read_json(ref, MAX_JSON_BYTES)
        check_active()
        if (not isinstance(raw, bytes) or len(raw) != evidence.bytes
                or len(raw) > MAX_JSON_BYTES or sha256(raw).hexdigest() != ref.sha256):
            raise AdoptionInvalid('SEMANTIC_EXPOSURE_BYTES_INVALID')
        try:
            return model.model_validate_json(canonical_bytes(parse_json(raw,max_bytes=MAX_JSON_BYTES))), raw
        except ValueError as error:
            raise AdoptionInvalid('SEMANTIC_EXPOSURE_CONTRACT_INVALID') from error
    edit, _ = document(params_ref, ApprovedSemanticEdit, 'input')
    source_ref = ArtifactRef(edit.source.source.artifact_id, edit.source.source.sha256)
    _, source_raw = document(source_ref, WorkingImage, 'input')
    try:
        source = bind_local_exposure_source(edit, source_raw)
    except ValueError as error:
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_SOURCE_INVALID') from error
    output, _ = document(root_ref, WorkingImage, 'output', 'working_image')
    if (root_ref.artifact_id.int == 0 or root_ref.artifact_id == source_ref.artifact_id
            or output.pixels.artifact_id.int == 0
            or output.pixels.artifact_id == source.pixels.artifact_id
            or output.pixels.artifact_id == root_ref.artifact_id
            or canonical_bytes(output.model_copy(update={'pixels': source.pixels})) != canonical_bytes(source)):
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_OUTPUT_CHANGED')
    pixel_ref = ArtifactRef(output.pixels.artifact_id, output.pixels.sha256)
    evidence = reader.evidence(pixel_ref)
    if (evidence.ref != pixel_ref or evidence.source != 'output'
            or evidence.role != 'pixels' or evidence.media_type != 'application/x-npy'):
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_PIXELS_INVALID')
    check_active()
    return edit, source, output


def verify_local_exposure(reader, params_ref, root_ref, *, protection_ref,
        verify_admission, verify_origin, verify_color, scratch_root, max_bytes,
        check_active):
    """Full content gate; installer supplies live task/runtime and origin authority.

    Does not commit adoption. Reader must use the current attempt's authoritative
    inventory and immutable version-pinned bytes throughout verification.
    """
    from photo_worker.contracts.base import ArtifactRef as ContractRef
    from photo_worker.contracts.images import MaskRef, ResolvedTransform
    from photo_worker.contracts.semantic import TransferCandidate, EditIntent
    from photo_worker.operations.semantic.approved_binding import bind_approved_edit_documents
    from photo_worker.operations.semantic.exposure_geometry import bind_local_exposure_geometry
    from photo_worker.operations.semantic.exposure_protection import bind_local_exposure_protection
    from .exposure_pixels import verify_exposure_pixels
    if not all(callable(value) for value in
               (verify_admission, verify_origin, verify_color, check_active)):
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
    edit, source, output = verify_exposure_documents(reader, params_ref, root_ref,
        check_active=check_active)
    def admitted():
        check_active()
        if verify_admission(edit.model_copy(deep=True)) is not True:
            raise AdoptionInvalid('SEMANTIC_EXPOSURE_ADMISSION_DENIED')
        check_active()
    admitted()
    def raw_document(value, model):
        ref = ArtifactRef(value.artifact_id, value.sha256)
        check_active()
        evidence = reader.evidence(ref)
        if evidence.ref != ref or evidence.source != 'input' or evidence.media_type != 'application/json':
            raise AdoptionInvalid('SEMANTIC_EXPOSURE_EVIDENCE_INVALID')
        raw = reader.read_json(ref, MAX_JSON_BYTES)
        check_active()
        if (not isinstance(raw, bytes) or len(raw) != evidence.bytes
                or not 2 <= len(raw) <= 1048576 or sha256(raw).hexdigest() != ref.sha256):
            raise AdoptionInvalid('SEMANTIC_EXPOSURE_BYTES_INVALID')
        model.model_validate_json(canonical_bytes(parse_json(raw)))
        return raw
    try:
        root = ContractRef.model_validate_json(canonical_bytes(protection_ref))
        candidate_raw = raw_document(edit.candidate, TransferCandidate)
        intent_raw = raw_document(edit.intent, EditIntent)
        _, _, intent = bind_approved_edit_documents(edit, candidate_raw, intent_raw)
        source_raw = raw_document(edit.source.source, WorkingImage)
        reference_raw = raw_document(intent.reference.source, WorkingImage)
        mask_raw = raw_document(edit.mask, MaskRef)
        transform_raw = raw_document(edit.transform, ResolvedTransform)
        _, mask, _ = bind_local_exposure_geometry(edit, candidate_raw, intent_raw,
            source_raw, mask_raw, transform_raw, reference_raw)
        protection_raw = raw_document(root, MaskRef)
        protection = bind_local_exposure_protection(edit, source_raw, root, protection_raw,
            verify_origin=verify_origin, check_active=check_active)
    except ValueError as error:
        if isinstance(error, AdoptionInvalid):raise
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_DOCUMENT_BINDING_INVALID') from error
    admitted()
    verify_exposure_pixels(reader, edit, source, output, mask, protection,
        scratch_root=scratch_root, max_bytes=max_bytes, verify_color=verify_color,
        check_active=check_active)
    admitted()
    return output


def verify_exposure_image_result(reader, params_ref, root_ref, **options):
    from photo_worker.contracts.base import parse_json
    from photo_worker.operations.optical.contracts import ImageResult, ImageReport
    check_active=options['check_active']
    def output_document(ref, model, role):
        check_active();evidence=reader.evidence(ref)
        if (evidence.ref!=ref or evidence.source!='output' or evidence.role!=role
                or evidence.media_type!='application/json'):
            raise AdoptionInvalid('SEMANTIC_EXPOSURE_EVIDENCE_INVALID')
        raw=reader.read_json(ref,MAX_JSON_BYTES);check_active()
        if (not isinstance(raw,bytes) or len(raw)!=evidence.bytes or len(raw)>MAX_JSON_BYTES
                or sha256(raw).hexdigest()!=ref.sha256):
            raise AdoptionInvalid('SEMANTIC_EXPOSURE_BYTES_INVALID')
        try:return model.model_validate_json(canonical_bytes(parse_json(raw)))
        except ValueError as error:
            raise AdoptionInvalid('SEMANTIC_EXPOSURE_CONTRACT_INVALID') from error
    result=output_document(root_ref,ImageResult,'document')
    image_ref=ArtifactRef(result.working_image.artifact_id,result.working_image.sha256)
    report_ref=ArtifactRef(result.report.artifact_id,result.report.sha256)
    if (root_ref.artifact_id.int==0 or len({root_ref.artifact_id,image_ref.artifact_id,
            report_ref.artifact_id})!=3 or image_ref.artifact_id.int==0 or report_ref.artifact_id.int==0):
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_OUTPUT_CHANGED')
    report=output_document(report_ref,ImageReport,'report')
    edit,_,_=verify_exposure_documents(reader,params_ref,image_ref,check_active=check_active)
    if (result.changed_mask!=edit.mask or result.generated_mask is not None
            or report.operation!='semantic.local_exposure' or report.status!='pass'
            or report.sources!=[edit.source] or report.skipped_count!=0
            or report.reason is not None or report.outside_mask_max_error!=0
            or report.clipping_fraction!=0 or report.identity is not None
            or report.transform is not None or report.registrations
            or report.parent_reports is not None):
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_REPORT_CHANGED')
    verify_local_exposure(reader,params_ref,image_ref,**options)
    check_active()
    return result


from dataclasses import dataclass


@dataclass(frozen=True)
class ExposureAdoptionInstallation:
    params: ArtifactRef
    protection: object
    verify_admission: object
    verify_origin: object
    verify_color: object

    def __post_init__(self):
        from photo_worker.contracts.base import ArtifactRef as ContractRef
        if (not isinstance(self.params,ArtifactRef) or self.params.artifact_id.int==0
                or not all(callable(value) for value in
                    (self.verify_admission,self.verify_origin,self.verify_color))):
            raise ValueError('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
        owned=ContractRef.model_validate_json(canonical_bytes(self.protection))
        if owned.artifact_id.int==0:raise ValueError('SEMANTIC_EXPOSURE_PROTECTION_INVALID')
        object.__setattr__(self,'protection',owned)
