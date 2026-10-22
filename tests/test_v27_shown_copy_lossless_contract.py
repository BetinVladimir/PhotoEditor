"""Own removal/retouch maximum-fence cross-language fixtures, deferred execution."""
from pathlib import Path
import pytest
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.semantic_preview import ShownRemovalAdoptionInput, ShownRetouchAdoptionInput


@pytest.mark.parametrize('kind,model', [('removal',ShownRemovalAdoptionInput),('retouch',ShownRetouchAdoptionInput)])
def test_copy_contract_maximum_fence_shared_wire(kind,model):
    path = Path(__file__).resolve().parents[1] / f'backend/internal/semanticreview/testdata/{kind}_preview_adoption_params.json'
    raw = path.read_bytes()
    value = model.model_validate_json(raw)
    assert value.preview_fence == 9223372036854775807
    assert type(value.preview_fence) is int
    assert canonical_bytes(value) == raw
    assert parse_json(raw)['preview_fence'] == '9223372036854775807'


@pytest.mark.parametrize('kind,model', [('removal',ShownRemovalAdoptionInput),('retouch',ShownRetouchAdoptionInput)])
def test_copy_contract_safe_numeric_wire_remains_unchanged(kind,model):
    path = Path(__file__).resolve().parents[1] / f'backend/internal/semanticreview/testdata/{kind}_preview_adoption_params.json'
    value = parse_json(path.read_bytes()); value['preview_fence'] = 9007199254740991
    raw = canonical_bytes(value)
    restored = model.model_validate_json(raw)
    assert canonical_bytes(restored) == raw


@pytest.mark.parametrize('kind,model', [('removal',ShownRemovalAdoptionInput),('retouch',ShownRetouchAdoptionInput)])
@pytest.mark.parametrize('wire', [9007199254740992,9223372036854775807,'1','01',
    '9007199254740991','9223372036854775808',True,None])
def test_copy_contract_refuses_unsafe_or_ambiguous_fence_wire(kind,model,wire):
    import json
    from pydantic import ValidationError
    path = Path(__file__).resolve().parents[1] / f'backend/internal/semanticreview/testdata/{kind}_preview_adoption_params.json'
    value = parse_json(path.read_bytes()); value['preview_fence'] = wire
    with pytest.raises(ValidationError): model.model_validate_json(json.dumps(value))


@pytest.mark.parametrize('kind,model', [('removal',ShownRemovalAdoptionInput),('retouch',ShownRetouchAdoptionInput)])
def test_copy_fence_schema_projection_matches_model_and_normative_contract(kind,model):
    import json
    root = Path(__file__).resolve().parents[1]
    own = model.model_json_schema()['properties']['preview_fence']
    standalone = json.loads((root / f'packages/contracts/generated/shown-{kind}-adoption-input.schema.json').read_text())
    normative = json.loads((root / 'docs/implementation-v2/contracts/schemas.json').read_text())['$defs']
    assert standalone['properties']['preview_fence'] == own
    matches = [value for value in normative.values() if value.get('title') == model.__name__]
    assert matches
    assert all(value['properties']['preview_fence'] == own for value in matches)
    assert own['oneOf'][0]['maximum'] == 9007199254740991


@pytest.mark.parametrize('kind', ['removal', 'retouch'])
def test_complete_preview_family_projection_matches_checked_in_contracts(kind):
    import json
    from photo_worker.contracts.semantic_preview_projection import (
        PREVIEW_MODELS, standalone_schema, project_bundle,
    )
    root = Path(__file__).resolve().parents[1]
    bundle = json.loads((root / 'docs/implementation-v2/contracts/schemas.json').read_text())
    projected = project_bundle(bundle)
    selected = [(name, model) for name, model in PREVIEW_MODELS if name in {
        f'semantic-{kind}-preview-input', f'{kind}-preview-numerical-evidence',
        f'{kind}-preview-presentation', f'shown-{kind}-adoption-input',
    }]
    assert len(selected) == 4
    for name, model in selected:
        stored = json.loads((root / f'packages/contracts/generated/{name}.schema.json').read_text())
        assert stored == standalone_schema(name, model)
        assert bundle['$defs'][model.__name__] == projected['$defs'][model.__name__]
    # Shared dependency changes must not leave stale definitions in the bundle.
    for name, value in projected['$defs'].items():
        if name.startswith('SemanticPreview_'):
            assert bundle['$defs'][name] == value
