import json
from pathlib import Path
from uuid import uuid4
import pytest
from jsonschema import Draft202012Validator
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.delivery import DeliveryPolicy,FrozenDeliveryInput


def policy_value():
    return dict(required_presets=[str(uuid4())],optional_presets=[],require_review=False,
                metadata_policy='strip',provenance='off',allow_omitted=False)


def test_delivery_policy_legacy_canonical_and_nested_omission():
    value=policy_value()
    policy=DeliveryPolicy.model_validate_json(json.dumps(value))
    assert canonical_bytes(policy)==canonical_bytes(value)
    value['approved_fields']=[]
    assert canonical_bytes(DeliveryPolicy.model_validate_json(json.dumps(value)))==canonical_bytes(policy)
    frozen=FrozenDeliveryInput(schema_version=1,snapshot={'artifact_id':uuid4(),'sha256':'a'*64},
        snapshot_digest='b'*64,policy=policy,presets=[dict(preset_id=uuid4(),preset_sha='c'*64,
        config={'artifact_id':uuid4(),'sha256':'c'*64},required=True)])
    assert 'approved_fields' not in json.loads(canonical_bytes(frozen))['policy']


def test_delivery_policy_approval_sorted_owned_and_explicit():
    value=policy_value();value.update(metadata_policy='approved_exif',approved_fields=['gps','author'])
    policy=DeliveryPolicy.model_validate_json(json.dumps(value))
    assert policy.approved_fields==['author','gps']
    value['approved_fields'][0]='lens'
    assert policy.approved_fields==['author','gps']
    assert json.loads(canonical_bytes(policy))['approved_fields']==['author','gps']


@pytest.mark.parametrize('mode,fields',[
    ('strip',['gps']),('copyright',['author']),('approved_exif',['gps','gps']),
    ('approved_exif',['unknown']),('approved_exif',['GPS']),('approved_exif',None),
])
def test_delivery_policy_refuses_invalid_consent(mode,fields):
    value=policy_value();value.update(metadata_policy=mode,approved_fields=fields)
    with pytest.raises(ValueError):DeliveryPolicy.model_validate_json(json.dumps(value))


def test_delivery_signing_policy_choice_is_frozen_only_for_required_mode():
    value=policy_value()
    value.update(provenance='required',signing_policy_id=str(uuid4()),signing_policy_sha='a'*64)
    policy=DeliveryPolicy.model_validate_json(json.dumps(value))
    assert json.loads(canonical_bytes(policy))['signing_policy_sha']=='a'*64
    for changed in (
        {**value,'signing_policy_id':None},
        {**value,'signing_policy_sha':None},
        {**value,'signing_policy_sha':'bad'},
        {**value,'provenance':'off'},
        {**policy_value(),'signing_policy_id':None},
        {**policy_value(),'signing_policy_sha':None},
    ):
        with pytest.raises(ValueError):DeliveryPolicy.model_validate_json(json.dumps(changed))


def test_delivery_policy_uses_sixteen_preset_total_limit():
    value=policy_value()
    value['required_presets']=[str(uuid4()) for _ in range(16)]
    assert len(DeliveryPolicy.model_validate(value).required_presets)==16
    value['optional_presets']=[value['required_presets'].pop()]
    assert len(DeliveryPolicy.model_validate(value).optional_presets)==1
    value['optional_presets'].append(str(uuid4()))
    with pytest.raises(ValueError):DeliveryPolicy.model_validate(value)
    value['optional_presets']=[value['required_presets'][0]]
    with pytest.raises(ValueError):DeliveryPolicy.model_validate(value)


def test_public_delivery_policy_schema_enforces_the_same_total_limit():
    root=Path(__file__).resolve().parents[2]
    schema=json.loads((root/'docs/implementation-v2/contracts/schemas.json').read_text())['$defs']['DeliveryPolicy']
    validator=Draft202012Validator(schema)
    value=policy_value()
    value['required_presets']=[str(uuid4()) for _ in range(16)]
    assert validator.is_valid(value)
    value['optional_presets']=[value['required_presets'].pop()]
    assert validator.is_valid(value)
    value['optional_presets'].append(str(uuid4()))
    assert not validator.is_valid(value)
    value=policy_value()
    value['required_presets'].append(value['required_presets'][0])
    assert not validator.is_valid(value)
