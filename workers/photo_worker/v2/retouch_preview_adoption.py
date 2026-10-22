"""Independent exact retouch preview document binding before canonical adoption."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput, RetouchPreviewPresentation
from photo_worker.operations.semantic.preview_retouch_result_binding import bind_retouch_preview_result
from .adoption import ArtifactRef, AdoptionInvalid, require_adoption_active


def verify_retouch_preview_output(reader, params_ref, root_ref, *, verify_admission, check_active):
    if not all(callable(fn) for fn in (verify_admission, check_active)):
        raise AdoptionInvalid('SEMANTIC_RETOUCH_ADOPTION_CONTEXT_REQUIRED')
    def active():require_adoption_active(check_active)
    def ref(value):return ArtifactRef(value.artifact_id,value.sha256)
    def document(value, source, role=None):
        active();record=reader.evidence(value);active()
        if (record.ref!=value or record.source!=source or record.media_type!='application/json'
                or type(record.bytes) is not int or not 2<=record.bytes<=1048576
                or role is not None and record.role!=role):
            raise AdoptionInvalid('SEMANTIC_RETOUCH_ADOPTION_EVIDENCE_INVALID')
        raw=reader.read_json(value,1048576);active()
        if (not isinstance(raw,bytes) or len(raw)!=record.bytes or sha256(raw).hexdigest()!=value.sha256):
            raise AdoptionInvalid('SEMANTIC_RETOUCH_ADOPTION_BYTES_INVALID')
        return raw
    raw=document(params_ref,'input')
    params=SemanticRetouchPreviewInput.model_validate_json(raw)
    if canonical_bytes(params)!=raw:
        raise AdoptionInvalid('SEMANTIC_RETOUCH_ADOPTION_BYTES_INVALID')
    def admitted():
        active()
        if verify_admission(bytes(raw)) is not True:
            raise AdoptionInvalid('SEMANTIC_RETOUCH_ADOPTION_DENIED')
        active()
    admitted()
    display_raw=document(root_ref,'output','preview_presentation')
    display=RetouchPreviewPresentation.model_validate_json(display_raw)
    admitted()
    proof_ref,image_ref=ref(display.numerical_evidence),ref(display.working_image)
    proof_raw=document(proof_ref,'output','preview_evidence')
    image_raw=document(image_ref,'output','working_image')
    source_raw=document(ref(params.source.source),'input')
    admitted()
    owned,presentation,proof,image=bind_retouch_preview_result(params,root_ref,display_raw,
        proof_ref,proof_raw,image_ref,image_raw,source_raw)
    for role,value,media,maximum in [('pixels',image.pixels,'application/x-npy',12*image.canvas.width*image.canvas.height+16384),
            *[(role,getattr(presentation,role),'image/png',33882112 if role in ('before','after') else 32833536)
              for role in ('before','after','affected','protected')]]:
        admitted();value=ref(value);record=reader.evidence(value);active()
        if (record.ref!=value or record.source!='output' or record.role!=role or record.media_type!=media
                or type(record.bytes) is not int or not 1<=record.bytes<=maximum):
            raise AdoptionInvalid('SEMANTIC_RETOUCH_ADOPTION_EVIDENCE_INVALID')
    admitted()
    return owned,presentation,proof,image
