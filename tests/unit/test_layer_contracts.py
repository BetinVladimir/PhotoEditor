from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.operations.layers.contracts import LayerComposeParams
from pathlib import Path
import json
from jsonschema import Draft202012Validator,FormatChecker


def schema_validator():
    schema=json.loads((Path(__file__).resolve().parents[2]/'docs/implementation-v2/contracts/schemas.json').read_text())
    return Draft202012Validator({**schema,'$ref':'#/$defs/LayerComposeParams'},format_checker=FormatChecker())


def data():
    ref=lambda:ArtifactRef(artifact_id=uuid4(),sha256='a'*64).model_dump(mode='json')
    return {'schema_version':1,'base':{'asset_version_id':str(uuid4()),'source':ref(),'canvas_hash':'b'*64},
        'source_result':None,'proposal_result':ref(),'mask':{'op':'ref','mask':ref(),'canvas_hash':'b'*64},
        'protected_mask':None,'opacity':.5,'blend':'normal'}


def test_frozen_layer_contract_wire_roundtrip():
    body=data();params=LayerComposeParams.model_validate_json(canonical_bytes(body))
    assert params.source_result is None and params.opacity==.5
    assert LayerComposeParams.model_validate_json(canonical_bytes(params))==params

@pytest.mark.parametrize('change',['canvas','blend','opacity','missing_lineage','null_mask','unknown'])
def test_layer_contract_rejects_unsafe_or_ambiguous_params(change):
    body=data()
    if change=='canvas':body['mask']['canvas_hash']='c'*64
    elif change=='blend':body['blend']='overlay'
    elif change=='opacity':body['opacity']=True
    elif change=='missing_lineage':del body['source_result']
    elif change=='null_mask':body['mask']=None
    else:body['operation']='portrait.makeup'
    with pytest.raises(ValueError):LayerComposeParams.model_validate_json(canonical_bytes(body))


@pytest.mark.parametrize('blend',['normal','multiply','screen'])
@pytest.mark.parametrize('parents',[False,True])
def test_layer_machine_schema_matches_nullable_worker_wire(blend,parents):
    body=data();body['blend']=blend
    if parents:
        body['source_result']=body['proposal_result']
        body['protected_mask']=body['mask']['mask']
    leaf=body['mask']
    body['mask']={'op':'subtract','left':{'op':'union','left':leaf,'right':leaf},'right':{'op':'invert','operand':leaf}}
    schema_validator().validate(body)
    params=LayerComposeParams.model_validate_json(canonical_bytes(body))
    assert canonical_bytes(params)==canonical_bytes(body)


@pytest.mark.parametrize('change',['missing_source','missing_protection','unknown','opacity_bool','opacity_range','invalid_blend','extra_mask_field','missing_operand','invalid_ref'])
def test_layer_machine_schema_and_worker_reject_malformed_wire(change):
    body=data()
    if change=='missing_source':del body['source_result']
    elif change=='missing_protection':del body['protected_mask']
    elif change=='unknown':body['extra']=1
    elif change=='opacity_bool':body['opacity']=True
    elif change=='opacity_range':body['opacity']=1.01
    elif change=='invalid_blend':body['blend']='overlay'
    elif change=='extra_mask_field':body['mask']['strength']=.5
    elif change=='missing_operand':body['mask']={'op':'invert'}
    else:body['proposal_result']['sha256']='invalid'
    assert not schema_validator().is_valid(body)
    with pytest.raises(ValueError):LayerComposeParams.model_validate_json(canonical_bytes(body))
