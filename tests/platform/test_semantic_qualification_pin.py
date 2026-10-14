"""Worker identity and runtime binding, not SQL/model qualification evidence."""
from copy import deepcopy
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_hash
from photo_worker.v2.adoption import AdoptionInvalid,ArtifactRef
from photo_worker.v2.semantic_qualification_pin import SemanticQualificationPin,semantic_runtime_authority


def value():
    return dict(policy_id=str(uuid4()),qualification_id=str(uuid4()),policy_sha='a'*64,
        report=dict(artifact_id=str(uuid4()),sha256='b'*64),calibration=dict(artifact_id=str(uuid4()),sha256='c'*64),
        model_lock_sha='d'*64,runtime_lock_sha='e'*64,device_profile_sha='f'*64,dataset_sha='1'*64,
        issuer_id=str(uuid4()),backend='cuda')


def authority(loader,raw,check_active=lambda:None):
    return semantic_runtime_authority(loader,frozen_pin=SemanticQualificationPin.from_value(raw),model_lock_sha='d'*64,runtime_lock_sha='e'*64,
        device_profile_sha='f'*64,dataset_sha='1'*64,check_active=check_active)


def test_pin_owns_refs_and_exact_calibration_identity():
    raw=value();original=deepcopy(raw);pin=SemanticQualificationPin.from_value(raw)
    assert pin.fingerprint()==canonical_hash(original)
    raw['calibration']['sha256']='0'*64
    assert pin.fingerprint()==canonical_hash(original)
    pin.bind('d'*64,pin.calibration)
    with pytest.raises(AdoptionInvalid):pin.bind('d'*64,ArtifactRef(uuid4(),pin.calibration.sha256))
    with pytest.raises(AdoptionInvalid):pin.bind('0'*64,pin.calibration)


@pytest.mark.parametrize('field',list(SemanticQualificationPin.__dataclass_fields__))
def test_every_frozen_identity_axis_affects_fingerprint(field):
    raw=value();old=SemanticQualificationPin.from_value(raw).fingerprint()
    if field in ('report','calibration'):raw[field]['artifact_id']=str(uuid4())
    elif field.endswith('_id'):raw[field]=str(uuid4())
    elif field=='backend':
        raw[field]='cpu'
        with pytest.raises(AdoptionInvalid):SemanticQualificationPin.from_value(raw)
        return
    else:raw[field]='0'*64
    assert SemanticQualificationPin.from_value(raw).fingerprint()!=old


@pytest.mark.parametrize('kind',['extra','missing','numeric_sha','zero_uuid','uuid_object','upper_sha','same_artifact','null_ref','extra_ref'])
def test_malformed_pins_rejected(kind):
    raw=value()
    if kind=='extra':raw['other']=True
    elif kind=='missing':del raw['policy_sha']
    elif kind=='numeric_sha':raw['dataset_sha']=111
    elif kind=='zero_uuid':raw['policy_id']='00000000-0000-0000-0000-000000000000'
    elif kind=='uuid_object':raw['issuer_id']=uuid4()
    elif kind=='upper_sha':raw['model_lock_sha']='A'*64
    elif kind=='same_artifact':raw['calibration']['artifact_id']=raw['report']['artifact_id']
    elif kind=='null_ref':raw['report']=None
    else:raw['report']['other']=True
    with pytest.raises(AdoptionInvalid):SemanticQualificationPin.from_value(raw)


@pytest.mark.parametrize('field',['model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha'])
def test_runtime_rechecks_changed_measurement(field):
    raw=value();calls=[]
    verify=authority(lambda:(calls.append(1),raw)[1],raw)
    verify();raw[field]='0'*64
    with pytest.raises(AdoptionInvalid,match='QUALIFICATION_CHANGED'):verify()
    assert calls==[1,1]


def test_runtime_revocation_and_cancellation_are_not_cached():
    raw=value();verify=authority(lambda:raw,raw)
    verify();raw.clear()
    with pytest.raises(AdoptionInvalid):verify()
    calls=[]
    def cancelled():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):authority(lambda:calls.append(1),value(),cancelled)()
    assert calls==[]


def test_runtime_denies_policy_or_report_substitution_even_with_identical_measurements():
    raw=value();verify=authority(lambda:raw,raw)
    verify()
    raw['policy_sha']='0'*64
    with pytest.raises(AdoptionInvalid,match='QUALIFICATION_CHANGED'):verify()
    raw=value();verify=authority(lambda:raw,raw)
    verify();raw['report']['artifact_id']=str(uuid4())
    with pytest.raises(AdoptionInvalid,match='QUALIFICATION_CHANGED'):verify()
