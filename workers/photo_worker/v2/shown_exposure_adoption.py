"""Independent shown-copy output binding before transactional adoption.

Readers must establish exact input/output provenance and immutable object SHA.
The live receipt authority callback belongs to the installed task lease.
"""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic_preview import ShownExposureAdoptionInput
from .adoption import ArtifactRef,AdoptionInvalid,require_adoption_active


def verify_shown_exposure_output(reader,params_ref,root_ref,*,verify_admission,check_active):
    if not callable(verify_admission) or not callable(check_active):
        raise AdoptionInvalid('SEMANTIC_SHOWN_ADOPTION_CONTEXT_REQUIRED')
    def active():
        require_adoption_active(check_active)
    def document(ref,model,source,role=None):
        active();e=reader.evidence(ref);active()
        if (e.ref!=ref or e.source!=source or e.media_type!='application/json'
                or role is not None and e.role!=role):
            raise AdoptionInvalid('SEMANTIC_SHOWN_EVIDENCE_INVALID')
        raw=reader.read_json(ref,1048576);active()
        if (not isinstance(raw,bytes) or not 2<=len(raw)<=1048576
                or len(raw)!=e.bytes or sha256(raw).hexdigest()!=ref.sha256):
            raise AdoptionInvalid('SEMANTIC_SHOWN_BYTES_INVALID')
        try:
            return model.model_validate_json(canonical_bytes(parse_json(raw,max_bytes=1048576))),raw
        except ValueError:
            raise AdoptionInvalid('SEMANTIC_SHOWN_CONTRACT_INVALID') from None
    params,raw=document(params_ref,ShownExposureAdoptionInput,'input')
    def admitted():
        active()
        if verify_admission(bytes(raw)) is not True:
            raise AdoptionInvalid('SEMANTIC_SHOWN_ADMISSION_DENIED')
        active()
    admitted()
    shown_ref=ArtifactRef(params.working_image.artifact_id,params.working_image.sha256)
    shown,_=document(shown_ref,WorkingImage,'input')
    output,_=document(root_ref,WorkingImage,'output','working_image')
    preserved={params_ref.artifact_id,shown_ref.artifact_id,params.pixels.artifact_id}
    if shown.alpha is not None:preserved.add(shown.alpha.artifact_id)
    if (shown.pixels!=params.pixels or root_ref.artifact_id.int==0
            or output.pixels.artifact_id.int==0 or root_ref.artifact_id in preserved
            or output.pixels.artifact_id in preserved or output.pixels.artifact_id==root_ref.artifact_id
            or output.pixels.sha256!=shown.pixels.sha256
            or canonical_bytes(output.model_copy(update={'pixels':shown.pixels}))!=canonical_bytes(shown)):
        raise AdoptionInvalid('SEMANTIC_SHOWN_OUTPUT_CHANGED')
    active()
    original=reader.evidence(ArtifactRef(shown.pixels.artifact_id,shown.pixels.sha256))
    copied=reader.evidence(ArtifactRef(output.pixels.artifact_id,output.pixels.sha256))
    active()
    maximum=12*shown.canvas.width*shown.canvas.height+16384
    if (original.ref!=ArtifactRef(shown.pixels.artifact_id,shown.pixels.sha256)
            or copied.ref!=ArtifactRef(output.pixels.artifact_id,output.pixels.sha256)
            or original.source!='input' or copied.source!='output' or copied.role!='pixels'
            or original.media_type!='application/x-npy' or copied.media_type!='application/x-npy'
            or type(original.bytes) is not int or type(copied.bytes) is not int
            or not 1<=original.bytes<=maximum or copied.bytes!=original.bytes):
        raise AdoptionInvalid('SEMANTIC_SHOWN_PIXELS_CHANGED')
    admitted()
    return params,shown,output
