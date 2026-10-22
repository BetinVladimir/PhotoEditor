"""Own shared Go/Python canonical wire and strict removal input boundaries."""
from pathlib import Path
import pytest
from pydantic import ValidationError
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.semantic_preview import SemanticRemovalPreviewInput

WIRE = Path(__file__).resolve().parents[1] / 'backend/internal/semanticreview/testdata/removal_preview_params.json'


def test_removal_shared_wire_preserves_large_string_revisions():
    raw = WIRE.read_bytes()
    value = SemanticRemovalPreviewInput.model_validate_json(raw)
    assert value.workflow_revision == '9007199254740993'
    assert value.target_revision == '9223372036854775806'
    assert canonical_bytes(value) == raw


@pytest.mark.parametrize('context,feather', [(32,1),(512,64)])
def test_removal_exact_control_boundaries(context, feather):
    value = SemanticRemovalPreviewInput.model_validate_json(WIRE.read_bytes()).model_dump(mode='python')
    value.update(context_pixels=context, feather_pixels=feather)
    assert SemanticRemovalPreviewInput.model_validate(value).context_pixels == context


@pytest.mark.parametrize('field,bad', [
    ('context_pixels',31),('context_pixels',513),('context_pixels',True),
    ('context_pixels','128'),('context_pixels',128.0),('feather_pixels',0),
    ('feather_pixels',65),('feather_pixels',True),('feather_pixels','8'),
    ('model_id','sd15_inpaint'),('kind','retouch_region'),('confirmed',True),
    ('approval_id','fake'),('workflow_revision','0'),('workflow_revision','01'),
    ('workflow_revision','9223372036854775807'),('workflow_revision',9007199254740993),
    ('tenant_id','00000000-0000-0000-0000-000000000000'),
])
def test_removal_wire_rejects_foreign_or_unsafe_values(field, bad):
    value = SemanticRemovalPreviewInput.model_validate_json(WIRE.read_bytes()).model_dump(mode='python'); value[field] = bad
    with pytest.raises(ValidationError): SemanticRemovalPreviewInput.model_validate(value)


@pytest.mark.parametrize('field', ['plan','intent','candidate','protection'])
def test_removal_wire_refuses_artifact_aliases(field):
    value = SemanticRemovalPreviewInput.model_validate_json(WIRE.read_bytes()).model_dump(mode='python'); value[field] = dict(value['source']['source'])
    with pytest.raises(ValidationError): SemanticRemovalPreviewInput.model_validate(value)
