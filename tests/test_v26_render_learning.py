from contextlib import contextmanager
from io import BytesIO
from uuid import uuid4
from copy import deepcopy
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.v2.render_recipe import decode_frozen_render_recipe
from photo_worker.v2.render_learning import load_render_learning_prediction
from test_v26_frozen_render_recipe import fixture


def setup():
    value=fixture();version=str(uuid4())
    model=dict(schema_version=1,profile_id=str(uuid4()),training_sha='a'*64,feature_order='scene12_v1',means=[0]*12,scales=[1]*12,coefficients=[[0]*6 for _ in range(12)],intercept=[2,.01,0,0,.02,0],ridge_lambda=.1)
    frame=dict(schema_version=1,binding=value['recipe']['source'],ordinal=3,captured_at=None,camera_hash=None,scene_embedding=[0]*384,luma_quantiles=[.1,.2,.3,.4,.5],neutral_rgb=None,sharpness=1,clipping_fraction=0,person_fraction=0,faces=dict(artifact_id=str(uuid4()),sha256='b'*64))
    shard=dict(schema_version=1,snapshot_digest='c'*64,frames=[frame])
    model_ref=dict(artifact_id=str(uuid4()),sha256=canonical_hash(model));descriptor_ref=dict(artifact_id=str(uuid4()),sha256=canonical_hash(shard))
    value['recipe']['learning_version_id']=version;value['recipe_sha256']=canonical_hash(value['recipe'])
    value['learning']=dict(version_id=version,model=model_ref,descriptor=descriptor_ref,analysis_id=str(uuid4()),ordinal=3)
    bodies={model_ref['artifact_id']:canonical_bytes(model),descriptor_ref['artifact_id']:canonical_bytes(shard)}
    class Artifacts:
        def __init__(self):self.reads=0
        @contextmanager
        def stream(self,ref):
            self.reads+=1
            with BytesIO(bodies[str(ref.artifact_id)]) as stream:yield stream
    return value,shard,bodies,Artifacts()


def test_pinned_model_prediction_uses_bounded_descriptor_and_clamps_only_model_output():
    value,_,_,artifacts=setup();frozen=decode_frozen_render_recipe(canonical_bytes(value))
    delta=load_render_learning_prediction(artifacts,frozen)
    assert delta.ev==1 and delta.log_r==.01 and delta.saturation_delta==.02
    assert artifacts.reads==2


@pytest.mark.parametrize('change',['binding','duplicate','ordinal','corrupt'])
def test_changed_descriptor_or_bytes_fail(change):
    value,shard,bodies,artifacts=setup()
    if change=='binding':shard['frames'][0]['binding']=dict(shard['frames'][0]['binding'],asset_version_id=str(uuid4()))
    elif change=='duplicate':shard['frames'].append(deepcopy(shard['frames'][0]))
    elif change=='ordinal':value['learning']['ordinal']=4
    else:bodies[value['learning']['descriptor']['artifact_id']]+=b' '
    if change in ('binding','duplicate'):
        value['learning']['descriptor']['sha256']=canonical_hash(shard);bodies[value['learning']['descriptor']['artifact_id']]=canonical_bytes(shard)
    with pytest.raises(ValueError):load_render_learning_prediction(artifacts,decode_frozen_render_recipe(canonical_bytes(value)))


def test_absent_learning_does_not_open_artifacts():
    _,_,_,artifacts=setup()
    assert load_render_learning_prediction(artifacts,decode_frozen_render_recipe(canonical_bytes(fixture()))) is None
    assert artifacts.reads==0
