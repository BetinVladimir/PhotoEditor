"""Perspective review preview plan over exact adopted original documents.

Pure compilation only: the scheduler must prove current adopted producer,
qualification, immutable locator versions and quotas before dispatching media.preview.
The existing color-managed media operation owns encoding and alpha handling.
"""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage, ResolvedTransform
from photo_worker.operations.optical.contracts import ImageResult, ImageReport
from photo_worker.operations.session.contracts import VersionBinding
from .render_derivative import RenderDerivativePlan, compile_render_derivative


@dataclass(frozen=True)
class PerspectiveDerivativePlan:
    media: RenderDerivativePlan
    binding_bytes: bytes
    authority_inputs: tuple[ArtifactRef, ...]
    requires_review: bool


def _document(kind, raw, ref):
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 1048576
            or not isinstance(ref, ArtifactRef)
            or sha256(raw).hexdigest() != ref.sha256):
        raise ValueError('PERSPECTIVE_DERIVATIVE_DOCUMENT_INVALID')
    value = kind.model_validate_json(raw)
    if canonical_bytes(value) != raw:
        raise ValueError('PERSPECTIVE_DERIVATIVE_DOCUMENT_INVALID')
    return value


def compile_perspective_derivative(result_ref, result_bytes, image_bytes,
        report_bytes, source_bytes, binding, srgb_sha256, *,
        transform_bytes=None, variant='preview1600') -> PerspectiveDerivativePlan:
    if not isinstance(binding, VersionBinding):
        raise ValueError('PERSPECTIVE_DERIVATIVE_BINDING_INVALID')
    binding = binding.model_copy(deep=True)
    result = _document(ImageResult, result_bytes, result_ref)
    source = _document(WorkingImage, source_bytes, binding.source)
    image = _document(WorkingImage, image_bytes, result.working_image)
    report = _document(ImageReport, report_bytes, result.report)
    if (canonical_hash(source.canvas) != binding.canvas_hash
            or source.dtype != 'float32' or image.dtype != 'float32'
            or image.color != source.color
            or image.canvas.source_sha256 != source.canvas.source_sha256
            or report.operation != 'optical.perspective'
            or report.sources != [binding] or report.identity is not None
            or report.registrations or report.parent_reports is not None
            or result.generated_mask is not None):
        raise ValueError('PERSPECTIVE_DERIVATIVE_LINEAGE_INVALID')
    roots = [result_ref, result.report, result.changed_mask]
    if (len({ref.artifact_id for ref in roots}) != len(roots)
            or any(ref.artifact_id in (binding.source.artifact_id,
                result.working_image.artifact_id) for ref in roots)):
        raise ValueError('PERSPECTIVE_DERIVATIVE_ALIAS_INVALID')
    identity = result.working_image == binding.source
    if identity:
        if (report.status != 'pass' or report.reason is not None
                or report.transform is not None or transform_bytes is not None):
            raise ValueError('PERSPECTIVE_DERIVATIVE_IDENTITY_INVALID')
    else:
        if (result.working_image.artifact_id == binding.source.artifact_id
                or report.status != 'review'
                or report.reason not in ('perspective_manual', 'perspective_auto')
                or report.transform is None or image.alpha is None):
            raise ValueError('PERSPECTIVE_DERIVATIVE_REVIEW_INVALID')
        transform = _document(ResolvedTransform, transform_bytes, report.transform)
        prefix = source.transform_chain.steps
        steps = image.transform_chain.steps
        if (transform.kind != 'homography'
                or transform.from_canvas_hash != binding.canvas_hash
                or transform.to_canvas_hash != canonical_hash(image.canvas)
                or len(steps) != len(prefix) + 1 or steps[:-1] != prefix):
            raise ValueError('PERSPECTIVE_DERIVATIVE_GEOMETRY_INVALID')
        step = steps[-1]
        if (step.kind != 'matrix' or step.from_canvas != source.canvas
                or step.to_canvas != image.canvas or step.invertibility != 'partial'
                or step.forward_matrix != transform.matrix
                or (step.valid_region.x, step.valid_region.y,
                    step.valid_region.width, step.valid_region.height)
                    != (0, 0, image.canvas.width, image.canvas.height)):
            raise ValueError('PERSPECTIVE_DERIVATIVE_GEOMETRY_INVALID')
        roots.append(report.transform)
    roots.extend([binding.source, result.working_image])
    seen = {}
    for ref in roots:
        prior = seen.get(ref.artifact_id)
        if prior is not None and (prior != ref or not identity
                or ref != binding.source):
            raise ValueError('PERSPECTIVE_DERIVATIVE_ALIAS_INVALID')
        seen[ref.artifact_id] = ref
    media = compile_render_derivative(result_ref, result_bytes, image_bytes,
                                     srgb_sha256, variant)
    # Authority roots are retained separately from media.preview's pixel inputs;
    # they must not be omitted from scheduler provenance/retention checks.
    return PerspectiveDerivativePlan(media, canonical_bytes(binding),
        tuple(ref.model_copy(deep=True) for ref in seen.values()), not identity)
