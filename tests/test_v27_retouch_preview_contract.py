"""Private retouch proposal contract; no inference/qualification execution."""
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput
from test_v27_exposure_preview_binding import fixture


def retouch_params():
    params, _, _ = fixture()
    return dict(params, kind='retouch_region')


def test_retouch_preview_owns_kind_and_exact_revision():
    params = retouch_params()
    owned = SemanticRetouchPreviewInput.model_validate_json(canonical_bytes(params))
    assert owned.kind == 'retouch_region'
    assert owned.target_revision == '9007199254740993'
    assert SemanticRetouchPreviewInput.model_validate_json(canonical_bytes(owned)) == owned
    params['source']['canvas_hash'] = 'f' * 64
    assert owned.source.canvas_hash != params['source']['canvas_hash']


@pytest.mark.parametrize('field,value', [
    ('kind','remove_object'), ('kind','background_cleanup'), ('kind','local_exposure'),
    ('model_id','big_lama'), ('context_pixels',128), ('feather_pixels',8),
    ('confirmed',True), ('approval_id','not-authority'),
    ('target_revision','01'), ('workflow_revision',str(2**63-1)),
    ('schema_version',True),
])
def test_retouch_preview_rejects_foreign_operation_and_apply_fields(field,value):
    params = retouch_params()
    params[field] = value
    with pytest.raises(ValueError):
        SemanticRetouchPreviewInput.model_validate_json(canonical_bytes(params))


def test_retouch_preview_shared_go_wire_fixture():
    from pathlib import Path
    raw=(Path(__file__).resolve().parents[1]/'backend/internal/semanticreview/testdata/retouch_preview_params.json').read_bytes()
    owned=SemanticRetouchPreviewInput.model_validate_json(raw)
    assert owned.kind=='retouch_region'
    assert owned.workflow_revision=='9007199254740993'
    assert owned.target_revision=='9223372036854775806'
    assert canonical_bytes(owned)==raw
