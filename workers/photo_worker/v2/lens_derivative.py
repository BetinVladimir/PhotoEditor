"""Lens authority metadata for the shared, color-managed media preview pipeline.

This compiler creates no qualification, task or authorization. The scheduler
must separately resolve every exact current adopted occurrence and verify array
locators before publishing the shared media.preview plan.
"""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage, DenseWarpTransform
from photo_worker.operations.optical.contracts import ImageResult, ImageReport
from photo_worker.operations.optical.result import lens_working_image
from photo_worker.operations.session.contracts import VersionBinding
from .render_derivative import RenderDerivativePlan, compile_render_derivative


@dataclass(frozen=True)
class LensDerivativePlan:
    media: RenderDerivativePlan
    binding_bytes: bytes
    authority_inputs: tuple[ArtifactRef, ...]
    requires_review: bool


def _document(kind, raw, ref):
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 1048576
            or not isinstance(ref, ArtifactRef)
            or sha256(raw).hexdigest() != ref.sha256):
        raise ValueError('LENS_DERIVATIVE_DOCUMENT_INVALID')
    value = kind.model_validate_json(raw)
    if canonical_bytes(value) != raw:
        raise ValueError('LENS_DERIVATIVE_DOCUMENT_INVALID')
    return value


def compile_lens_derivative(result_ref, result_bytes, image_bytes,
        report_bytes, source_bytes, binding, srgb_sha256, *, variant='preview1600'):
    if not isinstance(binding, VersionBinding):
        raise ValueError('LENS_DERIVATIVE_BINDING_INVALID')
    binding = binding.model_copy(deep=True)
    result = _document(ImageResult, result_bytes, result_ref)
    source = _document(WorkingImage, source_bytes, binding.source)
    image = _document(WorkingImage, image_bytes, result.working_image)
    report = _document(ImageReport, report_bytes, result.report)
    if (canonical_hash(source.canvas) != binding.canvas_hash
            or source.dtype != 'float32' or image.dtype != 'float32'
            or image.color != source.color or report.operation != 'optical.lens'
            or report.sources != [binding] or report.identity is not None
            or report.transform is not None or report.registrations
            or report.parent_reports is not None or result.generated_mask is not None):
        raise ValueError('LENS_DERIVATIVE_LINEAGE_INVALID')
    identity = result.working_image == binding.source
    if identity:
        if image != source or report.status != 'pass' or report.reason is not None:
            raise ValueError('LENS_DERIVATIVE_IDENTITY_INVALID')
    else:
        if (result.working_image.artifact_id == binding.source.artifact_id
                or image.alpha is None or not image.transform_chain.steps
                or (report.status, report.reason) not in
                    (('pass', None), ('review', 'uncovered_area'))):
            raise ValueError('LENS_DERIVATIVE_GEOMETRY_INVALID')
        step = image.transform_chain.steps[-1]
        if not isinstance(step, DenseWarpTransform):
            raise ValueError('LENS_DERIVATIVE_GEOMETRY_INVALID')
        # Strength affects pixel generation; the published field fixes geometry.
        expected = lens_working_image(source, image.pixels, image.alpha,
                                      step.inverse_field, strength=1)
        if image != expected:
            raise ValueError('LENS_DERIVATIVE_GEOMETRY_INVALID')
    roots = [result_ref, result.report, result.changed_mask,
             binding.source, result.working_image]
    seen = {}
    for ref in roots:
        prior = seen.get(ref.artifact_id)
        if prior is not None and (prior != ref or not identity or ref != binding.source):
            raise ValueError('LENS_DERIVATIVE_ALIAS_INVALID')
        seen[ref.artifact_id] = ref
    media = compile_render_derivative(result_ref, result_bytes, image_bytes,
                                     srgb_sha256, variant)
    return LensDerivativePlan(media, canonical_bytes(binding),
        tuple(ref.model_copy(deep=True) for ref in seen.values()), report.status == 'review')
