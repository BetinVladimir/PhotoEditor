from copy import deepcopy
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.v2.render_recipe import decode_frozen_render_recipe


def fixture():
    ref=lambda:dict(artifact_id=str(uuid4()),sha256='a'*64)
    binding=dict(asset_version_id=str(uuid4()),source=ref(),canvas_hash='b'*64)
    delta=dict(ev=.1,log_r=0,log_g=0,log_b=0,saturation_delta=0,retouch_delta=0)
    recipe=dict(schema_version=3,source=binding,base_style=ref(),session_corrections=None,
                learning_version_id=None,overrides=[],layer_stack=ref(),protected_mask=ref(),
                renderer_version='v2.2',parameter_delta=delta)
    scalars=dict(schema_version=1,source=binding,base_style=recipe['base_style'],effective_delta=delta,
                 renderer_version='v2.2',icc_lock_sha='c'*64,model_lock_sha='d'*64)
    return dict(schema_version=1,recipe=recipe,recipe_sha256=canonical_hash(recipe),scalars=scalars,learning=None)


def test_preserves_full_authored_composition():
    value=fixture();result=decode_frozen_render_recipe(canonical_bytes(value))
    assert canonical_bytes(result.recipe)==canonical_bytes(value['recipe'])
    assert result.recipe.layer_stack is not None and result.recipe.protected_mask is not None


@pytest.mark.parametrize('change', ['digest','source','style','extra','null_protection','learning'])
def test_rejects_changed_bindings_and_fields(change):
    value=fixture()
    if change=='digest': value['recipe_sha256']='f'*64
    elif change=='source': value['scalars']['source']=dict(value['scalars']['source'],asset_version_id=str(uuid4()))
    elif change=='style': value['scalars']['base_style']=dict(value['scalars']['base_style'],sha256='e'*64)
    elif change=='extra': value['extra']=True
    elif change=='null_protection':
        value['recipe']['protected_mask']=None;value['recipe_sha256']=canonical_hash(value['recipe'])
    else:
        value['recipe']['learning_version_id']=str(uuid4());value['recipe_sha256']=canonical_hash(value['recipe'])
    with pytest.raises(ValueError):decode_frozen_render_recipe(canonical_bytes(value))


def test_rejects_noncanonical_and_duplicate_keys():
    raw=canonical_bytes(fixture())
    for bad in (raw+b' ',b'{"schema_version":1,"schema_version":1}'):
        with pytest.raises(ValueError):decode_frozen_render_recipe(bad)


def test_learning_provenance_is_pinned_to_authored_version():
    value=fixture();version=str(uuid4())
    value['recipe']['learning_version_id']=version
    value['recipe_sha256']=canonical_hash(value['recipe'])
    value['learning']=dict(version_id=version,model=dict(artifact_id=str(uuid4()),sha256='e'*64),
                           descriptor=dict(artifact_id=str(uuid4()),sha256='f'*64),analysis_id=str(uuid4()),ordinal=4999)
    assert str(decode_frozen_render_recipe(canonical_bytes(value)).learning.version_id)==version
    for patch in (dict(version_id=str(uuid4())),dict(ordinal=5000),dict(analysis_id='00000000-0000-0000-0000-000000000000')):
        altered=deepcopy(value);altered['learning'].update(patch)
        with pytest.raises(ValueError):decode_frozen_render_recipe(canonical_bytes(altered))
