"""Pinned removal result linkage only; physical media and SQL authority are external."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic_preview import (
    SemanticRemovalPreviewInput,RemovalPreviewNumericalEvidence,RemovalPreviewPresentation)
from .preview_binding import bind_removal_preview_source


def bind_removal_preview_result(params,presentation_ref,presentation_raw,evidence_ref,evidence_raw,
        image_ref,image_raw,source_raw):
    owned=SemanticRemovalPreviewInput.model_validate_json(canonical_bytes(params))
    def document(ref,raw,model):
        if not isinstance(raw,bytes) or not 2<=len(raw)<=1048576 or sha256(raw).hexdigest()!=ref.sha256:
            raise ValueError('SEMANTIC_REMOVAL_RESULT_BYTES_INVALID')
        value=model.model_validate_json(canonical_bytes(parse_json(raw)))
        if canonical_bytes(value)!=raw:
            raise ValueError('SEMANTIC_REMOVAL_RESULT_BYTES_INVALID')
        return value
    presentation=document(presentation_ref,presentation_raw,RemovalPreviewPresentation)
    evidence=document(evidence_ref,evidence_raw,RemovalPreviewNumericalEvidence)
    image=document(image_ref,image_raw,WorkingImage)
    source=bind_removal_preview_source(owned,source_raw)
    scale = min(1.0, 2560 / max(image.canvas.width, image.canvas.height))
    width = max(1, round(image.canvas.width * scale))
    height = max(1, round(image.canvas.height * scale))
    if (canonical_bytes(presentation.params)!=canonical_bytes(owned)
            or canonical_bytes(evidence.params)!=canonical_bytes(owned)
            or presentation.numerical_evidence!=evidence_ref
            or presentation.working_image!=image_ref or evidence.working_image!=image_ref
            or presentation.pixels!=image.pixels or evidence.pixels!=image.pixels
            or canonical_bytes(image.model_copy(update={'pixels':source.pixels}))!=canonical_bytes(source)
            or presentation.width != width or presentation.height != height
            or evidence.roi[2]>image.canvas.width or evidence.roi[3]>image.canvas.height):
        raise ValueError('SEMANTIC_REMOVAL_RESULT_BINDING_INVALID')
    refs=(owned.plan,owned.intent,owned.candidate,owned.source.source,owned.protection,
        presentation_ref,evidence_ref,image_ref,image.pixels,source.pixels,
        presentation.before,presentation.after,presentation.affected,presentation.protected)
    if (any(ref.artifact_id.int==0 for ref in refs)
            or len({ref.artifact_id for ref in refs})!=len(refs)
            or source.alpha is not None and source.alpha.artifact_id in {ref.artifact_id for ref in refs}):
        raise ValueError('SEMANTIC_REMOVAL_RESULT_ALIAS')
    return owned,presentation,evidence,image
