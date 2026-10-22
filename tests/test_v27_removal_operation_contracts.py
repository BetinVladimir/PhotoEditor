from photo_worker.contracts.base import canonical_bytes
"""Cross-boundary removal catalog contracts; no database or GPU required."""
import ast
import json
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]


def test_removal_operation_registry_matches_worker_entrypoints():
    registry=json.loads((ROOT/'packages/contracts/operations.json').read_text())['operations']
    normative=json.loads((ROOT/'docs/implementation-v2/contracts/operations.json').read_text())['operations']
    for name in ('semantic.preview_remove_object','semantic.adopt_shown_removal','semantic.preview_background_cleanup'):
        runtime=[item for item in registry if item['name']==name]
        documented=[item for item in normative if item['id']==name]
        assert len(runtime)==len(documented)==1
        runtime,documented=runtime[0],documented[0]
        assert runtime['resource_class']==documented['resource_class']
        assert runtime['params_type']==documented['params']
        assert runtime['output_type']==documented['output']
        tree=ast.parse((ROOT/runtime['module']).read_text())
        functions={node.name for node in tree.body if isinstance(node,(ast.FunctionDef,ast.AsyncFunctionDef))}
        assert set(runtime['methods'])<=functions
        assert documented['max_output_count']==sum(item['max_count'] for item in documented['role_policies'].values())
        assert documented['max_output_bytes']>=sum(item['max_bytes']*item['max_count'] for item in documented['role_policies'].values())


def test_removal_standalone_schemas_bind_distinct_operations():
    for name,required in (
        ('semantic-removal-preview-input',{'kind','model_id','context_pixels','feather_pixels'}),
        ('shown-removal-adoption-input',{'approved_edit','preview_task_id','preview_result_id','preview_fence'}),
        ('removal-preview-numerical-evidence',{'algorithm','params','outside_mask_unchanged','protected_pixels_unchanged'}),
        ('removal-preview-presentation',{'numerical_evidence','working_image','pixels'}),
    ):
        schema=json.loads((ROOT/f'packages/contracts/generated/{name}.schema.json').read_text())
        assert required<=set(schema['required'])
        assert schema['additionalProperties'] is False


def test_removal_presentation_schema_matches_renderer_edge_limit():
    standalone=json.loads((ROOT/'packages/contracts/generated/removal-preview-presentation.schema.json').read_text())
    normative=json.loads((ROOT/'docs/implementation-v2/contracts/schemas.json').read_text())['$defs']['RemovalPreviewPresentation']
    for schema in (standalone,normative):
        for field in ('width','height'):
            assert schema['properties'][field]['minimum']==1
            assert schema['properties'][field]['maximum']==2560


def test_removal_presentation_runtime_dimensions_are_strict_and_bounded():
    import pytest
    from uuid import uuid4
    from pydantic import ValidationError
    from photo_worker.contracts.semantic_preview import RemovalPreviewPresentation
    from test_v27_removal_preview_binding import removal_fixture
    params,_,_=removal_fixture()
    value=dict(schema_version=1,params=params,width=2560,height=2560,
               icc_lock_sha='a'*64,srgb_sha='b'*64,
               resize='area_linear_premult_v1',overlay='max_coverage_ceil_alpha_v1')
    for role in ('numerical_evidence','working_image','pixels','before','after','affected','protected'):
        value[role]=dict(artifact_id=str(uuid4()),sha256='c'*64)
    assert RemovalPreviewPresentation.model_validate_json(canonical_bytes(value)).width==2560
    for field in ('width','height'):
        for invalid in (0,2561,4096,True,'2560',2560.0):
            with pytest.raises(ValidationError):
                RemovalPreviewPresentation.model_validate_json(json.dumps(dict(value,**{field:invalid})).encode())


def test_canonical_operation_catalog_survives_general_regeneration():
    source=(ROOT/'docs/implementation/contracts/operations.json').read_text()
    assert (ROOT/'packages/contracts/operations.json').read_text()==source
    assert (ROOT/'backend/internal/workflow/operations.json').read_text()==source
    catalog=json.loads(source)['operations']
    keys=[(row['name'],row['version']) for row in catalog]
    assert len(keys)==len(set(keys))
    for operation in ('semantic.adopt_shown_exposure','semantic.preview_remove_object','semantic.adopt_shown_removal'):
        assert (operation,'1') in keys


def test_both_generators_emit_identical_current_preview_schemas(monkeypatch):
    monkeypatch.syspath_prepend(str(ROOT/'scripts'))
    import runpy
    general=runpy.run_path(str(ROOT/'scripts/generate-contracts.py'))
    preview=runpy.run_path(str(ROOT/'scripts/generate-semantic-preview-contracts.py'))
    general_models=dict(general['SEMANTIC_PREVIEW_MODELS'])
    preview_models=dict(preview['MODELS'])
    assert general_models==preview_models
    # Pure output projection: no writes, migrations, GPU or object-store IO.
    generated=general['outputs']()
    for name,model in preview_models.items():
        path=ROOT/f'packages/contracts/generated/{name}.schema.json'
        actual=json.loads(path.read_text())
        expected=model.model_json_schema(by_alias=True) | {
            '$schema':'https://json-schema.org/draft/2020-12/schema',
            '$id':f'https://photoeditor.local/contracts/{name}.schema.json',
        }
        assert json.loads(generated[path])==expected
        assert actual==expected


def test_preview_projection_preserves_other_contracts_and_owns_output():
    from copy import deepcopy
    from photo_worker.contracts.semantic_preview_projection import project_bundle
    source={'$defs':{'ExistingContract':{'type':'object','properties':{'keep':{'const':1}}}},
            '$id':'existing-bundle'}
    before=deepcopy(source)
    projected=project_bundle(source)
    assert source==before
    assert projected['$defs']['ExistingContract']==source['$defs']['ExistingContract']
    projected['$defs']['ExistingContract']['properties']['keep']['const']=2
    assert source==before
    assert 'RemovalPreviewPresentation' in projected['$defs']


def test_preview_projection_rejects_conflicting_shared_dependencies(monkeypatch):
    import pytest
    from photo_worker.contracts import semantic_preview_projection as projection
    class First:
        @classmethod
        def model_json_schema(cls, **kwargs):
            return {'$defs':{'Shared':{'const':1}},'type':'object'}
    class Second:
        @classmethod
        def model_json_schema(cls, **kwargs):
            return {'$defs':{'Shared':{'const':2}},'type':'object'}
    monkeypatch.setattr(projection,'PREVIEW_MODELS',(('first',First),('second',Second)))
    with pytest.raises(ValueError,match='PREVIEW_SCHEMA_DEPENDENCY_CONFLICT'):
        projection.project_bundle({'$defs':{}})


def test_general_and_preview_generators_project_same_normative_bundle(monkeypatch):
    monkeypatch.syspath_prepend(str(ROOT/'scripts'))
    import runpy
    general=runpy.run_path(str(ROOT/'scripts/generate-contracts.py'))['outputs']()
    preview=runpy.run_path(str(ROOT/'scripts/generate-semantic-preview-contracts.py'))['outputs']()
    for path,body in preview.items():
        assert general[path]==body
