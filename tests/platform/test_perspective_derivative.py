"""Authored preview compiler tests; no live scheduler or image qualification."""
from uuid import uuid4
from hashlib import sha256
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.contracts.images import Canvas, ColorSpace, TransformChain, WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.operations.optical.contracts import ImageReport, ImageResult
from photo_worker.operations.optical.perspective_result import perspective_working_image
from photo_worker.algorithms.perspective import manual_perspective
from photo_worker.v2.perspective_derivative import compile_perspective_derivative


def ref():
    return ArtifactRef(artifact_id=uuid4(), sha256='a'*64)


def document(value):
    raw = canonical_bytes(value)
    return ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest()), raw


def fixture(identity=False):
    source = WorkingImage(pixels=ref(), alpha=None, dtype='float32',
        canvas=Canvas(width=20, height=10, orientation=1, space_id='original', source_sha256='b'*64),
        color=ColorSpace(profile_id='linear_srgb_v1', encoding='linear', icc_sha256='c'*64),
        transform_chain=TransformChain.create([]))
    source_ref, source_raw = document(source)
    binding = VersionBinding(asset_version_id=uuid4(), source=source_ref, canvas_hash=canonical_hash(source.canvas))
    if identity:
        image_ref, image_raw, transform_ref, transform_raw = source_ref, source_raw, None, None
    else:
        geometry = manual_perspective([[.1, 0], [.9, 0], [.9, 1], [.1, 1]], 20, 10,
            max_crop_fraction=1, max_pixels=200)
        image, transform = perspective_working_image(source, ref(), ref(), geometry)
        image_ref, image_raw = document(image)
        transform_ref, transform_raw = document(transform)
    report = ImageReport(schema_version=1, status='pass' if identity else 'review',
        operation='optical.perspective', skipped_count=0,
        reason=None if identity else 'perspective_manual', outside_mask_max_error=0,
        clipping_fraction=0, identity=None, transform=transform_ref,
        sources=[binding], registrations=[])
    report_ref, report_raw = document(report)
    result = ImageResult(schema_version=1, working_image=image_ref, report=report_ref,
        changed_mask=ref(), generated_mask=None)
    result_ref, result_raw = document(result)
    return dict(result_ref=result_ref, result_bytes=result_raw, image_bytes=image_raw,
        report_bytes=report_raw, source_bytes=source_raw, binding=binding,
        srgb_sha256='d'*64, transform_bytes=transform_raw)


def test_preview_plan_uses_existing_color_managed_media_abi_and_owned_roots():
    values = fixture()
    plan = compile_perspective_derivative(**values)
    assert plan.requires_review
    assert plan.media.source_result == values['result_ref']
    assert len(plan.authority_inputs) == 6
    assert plan.media.params_sha256 == sha256(plan.media.params_bytes).hexdigest()
    original = plan.binding_bytes
    values['binding'].canvas_hash = 'e'*64
    assert plan.binding_bytes == original


def test_identity_preview_keeps_original_source_and_no_transform():
    values = fixture(identity=True)
    plan = compile_perspective_derivative(**values, variant='preview2560')
    assert not plan.requires_review and len(plan.authority_inputs) == 4
    assert plan.media.inputs[0] == values['binding'].source
    values['transform_bytes'] = b'{}'
    with pytest.raises(ValueError, match='IDENTITY_INVALID'):
        compile_perspective_derivative(**values)


@pytest.mark.parametrize('key', ['result_bytes', 'image_bytes', 'report_bytes', 'source_bytes', 'transform_bytes'])
def test_modified_original_document_refused(key):
    values = fixture()
    values[key] = values[key] + b' '
    with pytest.raises(ValueError, match='DOCUMENT_INVALID'):
        compile_perspective_derivative(**values)


def test_report_review_downgrade_and_source_substitution_refused():
    for mode in ('status', 'source'):
        values = fixture()
        report = ImageReport.model_validate_json(values['report_bytes'])
        if mode == 'status':
            report.status = 'pass'
        else:
            report.sources[0].canvas_hash = 'e'*64
        report_ref, values['report_bytes'] = document(report)
        result = ImageResult.model_validate_json(values['result_bytes'])
        result.report = report_ref
        values['result_ref'], values['result_bytes'] = document(result)
        with pytest.raises(ValueError):
            compile_perspective_derivative(**values)
