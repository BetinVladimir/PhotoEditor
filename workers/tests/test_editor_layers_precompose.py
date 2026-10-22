"""Authored source-only scenarios; no runtime qualification is asserted."""
import json
from uuid import UUID
import pytest
from pydantic import ValidationError
from photo_worker.contracts.editor_precompose import EditorLayersPrecomposeParams


def test_precompose_requires_nonzero_original_identity():
    with pytest.raises(ValidationError):
        EditorLayersPrecomposeParams.model_validate_json(json.dumps({'schema_version':1,'build_run_id':str(UUID(int=0)),
            'original_recipe':{'artifact_id':str(UUID(int=1)),'sha256':'a'*64},
            'source':{'asset_version_id':str(UUID(int=2)),
                'source':{'artifact_id':str(UUID(int=3)),'sha256':'b'*64},'canvas_hash':'c'*64}}))


def test_precompose_cannot_accept_client_style_or_rebased_source_fields():
    body={'schema_version':1,'build_run_id':str(UUID(int=1)),
        'original_recipe':{'artifact_id':str(UUID(int=2)),'sha256':'a'*64},
        'source':{'asset_version_id':str(UUID(int=3)),
            'source':{'artifact_id':str(UUID(int=4)),'sha256':'b'*64},'canvas_hash':'c'*64}}
    assert EditorLayersPrecomposeParams.model_validate_json(json.dumps(body)).source.source.sha256=='b'*64
    with pytest.raises(ValidationError):EditorLayersPrecomposeParams.model_validate_json(json.dumps({**body,'style':{}}))


def test_precompose_source_parent_is_an_explicit_immutable_pin():
    body={'schema_version':1,'build_run_id':str(UUID(int=1)),
        'original_recipe':{'artifact_id':str(UUID(int=2)),'sha256':'a'*64},
        'source':{'asset_version_id':str(UUID(int=3)),
            'source':{'artifact_id':str(UUID(int=4)),'sha256':'b'*64},'canvas_hash':'c'*64},
        'source_result':{'artifact_id':str(UUID(int=5)),'sha256':'d'*64}}
    params=EditorLayersPrecomposeParams.model_validate_json(json.dumps(body))
    assert params.source_result.artifact_id == UUID(int=5)
    body['source_result']['sha256']='e'*64
    assert params.source_result.sha256=='d'*64
    with pytest.raises(ValidationError):
        EditorLayersPrecomposeParams.model_validate_json(json.dumps({**body,'source_result':{'artifact_id':str(UUID(int=0)),'sha256':'d'*64}}))
