"""Removal output contract invariants; authored without inference or database IO."""
from pathlib import Path
from uuid import uuid4
import pytest
from pydantic import ValidationError
from photo_worker.contracts.semantic_preview import SemanticRemovalPreviewInput, RemovalPreviewNumericalEvidence


def evidence():
    raw = (Path(__file__).resolve().parents[1] / 'backend/internal/semanticreview/testdata/removal_preview_params.json').read_bytes()
    return dict(schema_version=1, params=SemanticRemovalPreviewInput.model_validate_json(raw),
        working_image=dict(artifact_id=uuid4(), sha256='a'*64),
        pixels=dict(artifact_id=uuid4(), sha256='b'*64),
        algorithm='big_lama_work_srgb_delta_v1', changed_pixels=4,
        protected_pixels_unchanged=True, outside_mask_unchanged=True,
        global_pass=False, roi=[0,0,2,2])


def test_removal_evidence_accepts_safe_integer_roi_boundary():
    value = evidence(); value['roi'] = [9007199254740989,0,9007199254740991,2]
    assert RemovalPreviewNumericalEvidence.model_validate(value).changed_pixels == 4


@pytest.mark.parametrize('roi', [[0,0,9007199254740992,2],[0,0,10**100,2],
    [-1,0,2,2],[0,0,0,2],[0,0,2,0],[0,0,1,1],[True,0,2,2],[0,0,2.0,2],[0,0,2]])
def test_removal_evidence_refuses_invalid_roi(roi):
    value = evidence(); value['roi'] = roi
    with pytest.raises(ValidationError): RemovalPreviewNumericalEvidence.model_validate(value)


@pytest.mark.parametrize('field,bad', [('changed_pixels',True),('changed_pixels',-1),
    ('changed_pixels',200000001),('outside_mask_unchanged',1),
    ('protected_pixels_unchanged',False),('global_pass',0),('algorithm','other')])
def test_removal_evidence_requires_strict_numerical_and_preservation_proof(field,bad):
    value = evidence(); value[field] = bad
    with pytest.raises(ValidationError): RemovalPreviewNumericalEvidence.model_validate(value)


@pytest.mark.parametrize('field', ['numerical_evidence','working_image','pixels','before','after','affected','protected'])
def test_removal_presentation_rejects_alias_to_any_declared_input(field):
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.contracts.semantic_preview import RemovalPreviewPresentation
    from test_v27_removal_preview_result_binding import fixture
    display = RemovalPreviewPresentation.model_validate_json(fixture()[2])
    for ref in (display.params.plan,display.params.intent,display.params.candidate,
                display.params.source.source,display.params.protection):
        value = display.model_dump(mode='python'); value[field] = ref
        with pytest.raises(ValidationError): RemovalPreviewPresentation.model_validate(value)


@pytest.mark.parametrize('field,bad', [('width',0),('width',2561),('width',True),
    ('width','8'),('height',0),('height',2561),('height',8.0),
    ('icc_lock_sha','invalid'),('srgb_sha','invalid'),('resize','nearest'),('overlay','average')])
def test_removal_presentation_requires_exact_display_policy(field,bad):
    from photo_worker.contracts.semantic_preview import RemovalPreviewPresentation
    from test_v27_removal_preview_result_binding import fixture
    value = RemovalPreviewPresentation.model_validate_json(fixture()[2]).model_dump(mode='python')
    value[field] = bad
    with pytest.raises(ValidationError): RemovalPreviewPresentation.model_validate(value)
