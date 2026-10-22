"""Own retouch projection surface; generation and validation not run in coding phase."""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview_projection import PREVIEW_MODELS, project_bundle, standalone_schema
from test_v27_retouch_preview_contract import retouch_params
from test_v27_retouch_preview_evidence import evidence, presentation
from jsonschema import Draft202012Validator, FormatChecker
import pytest


@pytest.mark.parametrize('name,filename', [
    ('semantic-retouch-preview-input', 'retouch_preview_params.json'),
    ('retouch-preview-numerical-evidence', 'retouch_preview_evidence.json'),
    ('retouch-preview-presentation', 'retouch_preview_presentation.json'),
    ('shown-retouch-adoption-input', 'retouch_preview_adoption_params.json'),
    ('retouch-preview-adapter-context', 'retouch_preview_context.json'),
])
def test_shared_go_wire_is_accepted_by_worker_and_materialized_schemas(name, filename):
    import json
    from pathlib import Path
    from pydantic import ValidationError
    from jsonschema import ValidationError as SchemaValidationError
    root = Path(__file__).resolve().parents[1]
    raw = (root / 'backend/internal/semanticreview/testdata' / filename).read_bytes()
    value = json.loads(raw)
    model = dict(PREVIEW_MODELS)[name]
    owned = model.model_validate_json(raw)
    assert canonical_bytes(owned) == raw
    standalone = json.loads((root / 'packages/contracts/generated' / (name + '.schema.json')).read_text())
    bundle = json.loads((root / 'docs/implementation-v2/contracts/schemas.json').read_text())
    validators = [Draft202012Validator(standalone, format_checker=FormatChecker()),
        Draft202012Validator({**bundle, '$ref': '#/$defs/' + model.__name__}, format_checker=FormatChecker())]
    for validator in validators:
        validator.validate(value)
    # None of these content contracts can acquire approval authority through
    # an additional wire field, even when all structural data is otherwise valid.
    extra = {**value, 'approval': True}
    with pytest.raises(ValidationError):
        model.model_validate_json(canonical_bytes(extra))
    for validator in validators:
        with pytest.raises(SchemaValidationError):
            validator.validate(extra)


@pytest.mark.parametrize('name,value',[
    ('semantic-retouch-preview-input',retouch_params),
    ('retouch-preview-numerical-evidence',evidence),
    ('retouch-preview-presentation',presentation),
])
def test_own_retouch_projection_validates_exact_model_wire(name,value):
    models = dict(PREVIEW_MODELS)
    model = models[name]
    owned = model.model_validate_json(canonical_bytes(value()))
    schema = standalone_schema(name,model)
    Draft202012Validator(schema,format_checker=FormatChecker()).validate(owned.model_dump(mode='json'))
    assert schema['$id'].endswith('/'+name+'.schema.json')


def test_retouch_bundle_keeps_existing_definitions_and_adds_own_context():
    original = {'$defs':{'ExistingUnrelated':{'type':'string'}}}
    projected = project_bundle(original)
    assert original == {'$defs':{'ExistingUnrelated':{'type':'string'}}}
    assert projected['$defs']['ExistingUnrelated'] == {'type':'string'}
    for name in ('SemanticRetouchPreviewInput','RetouchPreviewNumericalEvidence',
                 'RetouchPreviewPresentation','ShownRetouchAdoptionInput','RetouchPreviewAdapterContext'):
        assert name in projected['$defs']
    context = projected['$defs']['RetouchPreviewAdapterContext']
    assert context['properties']['faces']['maxItems'] == 64
    assert context['properties']['algorithm']['const'] == 'beautyfilter_v1'
    assert project_bundle(projected) == projected


def test_retouch_catalog_projection_preserves_existing_operations_and_is_idempotent():
    import json
    from pathlib import Path
    from photo_worker.contracts.retouch_preview_catalog_projection import project_retouch_preview_catalog
    path = Path(__file__).resolve().parents[1]/'docs/implementation-v2/contracts/operations.json'
    original = json.loads(path.read_text())
    projected = project_retouch_preview_catalog(original)
    assert [op for op in projected['operations'] if op['id'] != 'semantic.preview_retouch_region'] == [op for op in original['operations'] if op['id'] != 'semantic.preview_retouch_region']
    op = next(op for op in projected['operations'] if op['id'] == 'semantic.preview_retouch_region')
    assert op['resource_class'] == op['execution_pool'] == 'cpu'
    assert op['params'] == 'SemanticRetouchPreviewInput' and op['max_output_count'] == 8
    assert op['role_policies']['preview_evidence']['contract'] == 'RetouchPreviewNumericalEvidence'
    assert project_retouch_preview_catalog(projected) == projected


@pytest.mark.parametrize('name,value',[
    ('SemanticRetouchPreviewInput',retouch_params),
    ('RetouchPreviewNumericalEvidence',evidence),
    ('RetouchPreviewPresentation',presentation),
])
def test_materialized_bundle_matches_own_wire_and_current_pin(name,value):
    import json
    from pathlib import Path
    from hashlib import sha256
    from photo_worker.v2.contract_lock import SCHEMAS_SHA256,COPY_FENCE_SCHEMAS_SHA256,RETOUCH_PREVIOUS_SCHEMAS_SHA256
    root = Path(__file__).resolve().parents[1]
    raw = (root/'docs/implementation-v2/contracts/schemas.json').read_bytes()
    assert sha256(raw).hexdigest() == SCHEMAS_SHA256
    assert RETOUCH_PREVIOUS_SCHEMAS_SHA256 == '3ceaa93f206f65f3c5dd568e6a8fac587a8e76dc646a0f4309452a0d2afa1f9c'
    bundle = json.loads(raw)
    Draft202012Validator({**bundle,'$ref':'#/$defs/'+name},format_checker=FormatChecker()).validate(value())
    migration = (root/'backend/db/migrations/02362_v27_retouch_preview_contract_pin.sql').read_text()
    rollout = (root/'backend/db/migrations/02570_copy_fence_current_schema_pin.sql').read_text()
    # API-only additive request changed the build bundle, not the stored worker ABI.
    assert COPY_FENCE_SCHEMAS_SHA256 in rollout and 'seal_semantic_retouch_preview_inputs' in rollout
    # Historical migration retains its original bundle; the forward rollout owns current pins.
    assert '886cb3e1b7316c40d6696ab57e59bcc2a8ec714b50ffd8d51c0a1cb9cf799f90' in migration and 'CREATE OR REPLACE FUNCTION photo.seal_semantic_retouch_preview_inputs' in migration


def test_retouch_registry_stays_disabled_until_remaining_authority_is_installed():
    from pathlib import Path
    root = Path(__file__).resolve().parents[1]
    sql = (root/'backend/db/migrations/02361_v27_retouch_preview_catalog.sql').read_text()
    assert "'cpu','cpu.semantic_preview_retouch_region',NULL,1800,'[\"cpu\"]',false" in sql
    assert sql.count("'semantic.preview_retouch_region','1','") == 9
