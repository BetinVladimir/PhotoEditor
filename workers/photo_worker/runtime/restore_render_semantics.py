"""Strict semantics for physically pinned direct render result documents."""
from dataclasses import dataclass
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes,parse_json,canonical_hash
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.operations.optical.contracts import ImageReport
from photo_worker.operations.session.contracts import VersionBinding
from .restore_authority import require_restore_active


@dataclass(frozen=True)
class RestoreRenderSemanticEvidence:
    schema_version: str
    status: str
    canvas_sha256: str
    pixel_artifact_id: UUID
    changed_mask_artifact_id: UUID
    generated_mask_artifact_id: UUID | None


def validate_restore_render_documents(image_raw,report_raw,changed_raw,generated_raw,*,binding,check_active):
    """Caller proves SQL producer authority and each document's physical SHA.

    This validates metadata only: nested NPY bytes and measured image quality
    require additional verification and are not synthesized from report status.
    """
    if not isinstance(binding,VersionBinding) or not callable(check_active):
        raise ValueError('RESTORE_RENDER_SEMANTIC_INSTALLATION')
    def decode(raw,kind):
        require_restore_active(check_active)
        if not isinstance(raw,bytes) or not 2<=len(raw)<=1048576:
            raise ValueError('RESTORE_RENDER_SEMANTIC_SIZE')
        value=parse_json(raw);document=kind.model_validate_json(raw)
        if canonical_bytes(value)!=raw or canonical_bytes(document)!=raw:
            raise ValueError('RESTORE_RENDER_SEMANTIC_CANONICAL')
        require_restore_active(check_active);return document
    image=decode(image_raw,WorkingImage);report=decode(report_raw,ImageReport)
    changed=decode(changed_raw,MaskRef)
    generated=None if generated_raw is None else decode(generated_raw,MaskRef)
    if (report.operation!='render.compose' or report.status not in ('pass','review')
            or report.sources!=[binding] or report.registrations):
        raise ValueError('RESTORE_RENDER_SEMANTIC_REPORT')
    canvas=canonical_hash(image.canvas)
    if canvas!=binding.canvas_hash or image.dtype!='float32':
        raise ValueError('RESTORE_RENDER_SEMANTIC_SOURCE_CANVAS')
    if any(canonical_hash(mask.canvas)!=canvas for mask in (changed,generated) if mask is not None):
        raise ValueError('RESTORE_RENDER_SEMANTIC_CANVAS')
    for mask,selector in ((changed,'changed'),(generated,'generated')):
        if mask is not None and (mask.transform_chain!=image.transform_chain or mask.selector!=selector
                or mask.protected or mask.object_id is not None or mask.confidence!=1 or mask.model_lock_id is not None):
            raise ValueError('RESTORE_RENDER_SEMANTIC_MASK')
    refs=[image.pixels,changed.artifact]
    if image.alpha is not None:refs.append(image.alpha)
    if generated is not None:refs.append(generated.artifact)
    if any(r.artifact_id.int==0 for r in refs):raise ValueError('RESTORE_RENDER_SEMANTIC_ZERO_REF')
    require_restore_active(check_active)
    return RestoreRenderSemanticEvidence('restore-render-semantic/v1',report.status,canvas,
        image.pixels.artifact_id,changed.artifact.artifact_id,None if generated is None else generated.artifact.artifact_id)
