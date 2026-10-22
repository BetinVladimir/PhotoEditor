from photo_worker.contracts.base import canonical_bytes
from uuid import uuid4
import pytest
from photo_worker.contracts.semantic_preview import RemovalPreviewNumericalEvidence
from test_v27_removal_preview_binding import removal_fixture


def evidence():
    params, _, _ = removal_fixture()
    return dict(schema_version=1, params=params,
        working_image=dict(artifact_id=str(uuid4()), sha256='a'*64),
        pixels=dict(artifact_id=str(uuid4()), sha256='b'*64),
        algorithm='big_lama_work_srgb_delta_v1', changed_pixels=2,
        protected_pixels_unchanged=True, outside_mask_unchanged=True,
        global_pass=False, roi=[0, 0, 8, 8])


def test_evidence_owns_removal_parameters_without_approval():
    result = RemovalPreviewNumericalEvidence.model_validate_json(canonical_bytes(evidence()))
    assert result.params.kind == 'remove_object'
    assert 'approval_id' not in result.model_dump()


@pytest.mark.parametrize('field,value', [
    ('protected_pixels_unchanged', 1), ('outside_mask_unchanged', False),
    ('changed_pixels', True), ('changed_pixels', 65),
    ('roi', [0, 0, 0, 8]), ('global_pass', 1),
    ('algorithm', 'local_exposure_exact_replay_v1'),
])
def test_invalid_preservation_claims_are_rejected(field, value):
    data = evidence(); data[field] = value
    with pytest.raises(ValueError):
        RemovalPreviewNumericalEvidence.model_validate_json(canonical_bytes(data))


def test_outputs_cannot_alias_proposal_inputs():
    data = evidence(); data['pixels'] = data['params']['candidate']
    with pytest.raises(ValueError, match='ARTIFACT_ALIAS'):
        RemovalPreviewNumericalEvidence.model_validate_json(canonical_bytes(data))
