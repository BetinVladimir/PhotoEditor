from uuid import uuid4
from copy import deepcopy
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.render_layers import decode_render_layer_stack


def fixture():
    ref=lambda:dict(artifact_id=str(uuid4()),sha256='a'*64)
    binding=dict(asset_version_id=str(uuid4()),source=ref(),canvas_hash='b'*64)
    layer=dict(id=str(uuid4()),enabled=True,opacity=1,blend='normal',operation='color.adjust',operation_version='1',params=ref(),mask=dict(op='ref',mask=ref(),canvas_hash=binding['canvas_hash']))
    return dict(schema_version=1,source=binding,layers=[layer])


def decode(value):
    ref=ArtifactRef.model_validate_json(canonical_bytes(dict(artifact_id=str(uuid4()),sha256=canonical_hash(value))))
    binding=VersionBinding.model_validate_json(canonical_bytes(value['source']))
    return decode_render_layer_stack(canonical_bytes(value),ref,binding)


def test_preserves_layer_stack_and_rejects_changed_digest():
    value=fixture();assert canonical_bytes(decode(value))==canonical_bytes(value)
    ref=ArtifactRef.model_validate_json(canonical_bytes(dict(artifact_id=str(uuid4()),sha256='f'*64)))
    with pytest.raises(ValueError):decode_render_layer_stack(canonical_bytes(value),ref,decode(value).source)


@pytest.mark.parametrize('change',['duplicate','geometry','canvas','params','extra','mask_budget'])
def test_layer_stack_rejects_invalid_composition(change):
    value=fixture()
    if change=='duplicate':value['layers']*=2
    elif change=='geometry':
        second=deepcopy(value['layers'][0]);second.update(id=str(uuid4()),operation='optical.lens');value['layers'].append(second)
    elif change=='canvas':value['layers'][0]['mask']['canvas_hash']='c'*64
    elif change=='params':value['layers'][0]['params']['artifact_id']='00000000-0000-0000-0000-000000000000'
    elif change=='extra':value['layers'][0]['extra']=True
    else:
        expression=value['layers'][0]['mask']
        for _ in range(7):expression=dict(op='union',left=deepcopy(expression),right=deepcopy(expression))
        value['layers'][0]['mask']=expression
        second=deepcopy(value['layers'][0]);second['id']=str(uuid4());value['layers'].append(second)
    with pytest.raises(ValueError):decode(value)
