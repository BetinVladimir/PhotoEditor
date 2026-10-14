"""Canonical report/policy binding; live SQL/object ownership is external."""
from copy import deepcopy
from dataclasses import replace
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.semantic_qualification_pin import SemanticQualificationPin
from photo_worker.v2.semantic_qualification_report import verify_semantic_qualification_report


def fixture():
    policy=dict(schema_version=1,policy_id=str(uuid4()),operation='semantic.transfer',operation_version='1',backend='cuda',model_lock_sha='a'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64,dataset_sha='d'*64,issuer_id=str(uuid4()),required_checks=['quality'],test_fixture_shas=['e'*64],calibration_shas=['6'*64],evidence_shas=['f'*64],license_cleared=True)
    report=dict(schema_version=1,operation='semantic.transfer',version='1',model_lock_sha='a'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64,dataset_sha='d'*64,issuer_id=policy['issuer_id'],input_fixture_shas=['e'*64],checks=[dict(id='quality',passed=True,evidence_sha='f'*64)],metrics=dict(p50_ms=10,p95_ms=20,peak_vram_bytes=0,peak_rss_bytes=0,scratch_bytes=0),status='passed',created_at='2026-09-01T00:00:00Z')
    raw=canonical_bytes(report)
    pin=SemanticQualificationPin.from_value(dict(policy_id=policy['policy_id'],qualification_id=str(uuid4()),policy_sha=canonical_hash(policy),report=dict(artifact_id=str(uuid4()),sha256=sha256(raw).hexdigest()),calibration=dict(artifact_id=str(uuid4()),sha256='6'*64),model_lock_sha='a'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64,dataset_sha='d'*64,issuer_id=policy['issuer_id'],backend='cuda'))
    return policy,report,raw,pin


def test_exact_report_and_policy_owned():
    policy,report,raw,pin=fixture();original=deepcopy(policy)
    assert verify_semantic_qualification_report(raw,pin,policy)==report
    assert policy==original


@pytest.mark.parametrize('kind',['sha','whitespace','duplicate_json','extra','missing','schema_bool','failed','issuer','model','runtime','device','dataset','fixture','overlap','duplicate_fixture','empty_fixture','missing_check','failed_check','bool_metric','negative_metric','reversed_latency','extra_metric','duplicate_check','evidence','timestamp','zero_timestamp'])
def test_repinned_malformed_reports_do_not_prove_qualification(kind):
    policy,report,raw,pin=fixture()
    if kind=='sha':raw=raw+b' '
    elif kind=='whitespace':raw=b' '+raw
    elif kind=='duplicate_json':raw=raw[:-1]+b',"status":"passed"}'
    elif kind=='extra':report['other']=1
    elif kind=='missing':del report['status']
    elif kind=='schema_bool':report['schema_version']=True
    elif kind=='failed':report['status']='failed'
    elif kind in ('issuer','model','runtime','device','dataset'):
        report[dict(issuer='issuer_id',model='model_lock_sha',runtime='runtime_lock_sha',device='device_profile_sha',dataset='dataset_sha')[kind]]=str(uuid4()) if kind=='issuer' else '0'*64
    elif kind=='fixture':report['input_fixture_shas']=['0'*64]
    elif kind=='overlap':report['input_fixture_shas']=['6'*64]
    elif kind=='duplicate_fixture':report['input_fixture_shas']*=2
    elif kind=='empty_fixture':report['input_fixture_shas']=[]
    elif kind=='missing_check':report['checks'][0]['id']='unrelated'
    elif kind=='failed_check':report['checks'][0]['passed']=False
    elif kind=='bool_metric':report['metrics']['p50_ms']=True
    elif kind=='negative_metric':report['metrics']['scratch_bytes']=-1
    elif kind=='reversed_latency':report['metrics']['p95_ms']=1
    elif kind=='extra_metric':report['metrics']['other']=0
    elif kind=='duplicate_check':report['checks']*=2
    elif kind=='evidence':report['checks'][0]['evidence_sha']='0'*64
    elif kind=='timestamp':report['created_at']='2026-09-01'
    elif kind=='zero_timestamp':report['created_at']='0001-01-01T00:00:00Z'
    if kind not in ('sha','whitespace','duplicate_json'):raw=canonical_bytes(report)
    if kind!='sha':pin=replace(pin,report=replace(pin.report,sha256=sha256(raw).hexdigest()))
    with pytest.raises(AdoptionInvalid):verify_semantic_qualification_report(raw,pin,policy)


@pytest.mark.parametrize('kind',['checks','license','extra','duplicate','calibration','policy_sha'])
def test_current_policy_cannot_silently_replace_frozen_policy(kind):
    policy,_,raw,pin=fixture()
    if kind=='checks':policy['required_checks']=['changed']
    elif kind=='license':policy['license_cleared']=False
    elif kind=='extra':policy['other']=1
    elif kind=='duplicate':policy['evidence_shas']*=2
    elif kind=='calibration':policy['calibration_shas']=['0'*64]
    else:pin=replace(pin,policy_sha='0'*64)
    with pytest.raises(AdoptionInvalid):verify_semantic_qualification_report(raw,pin,policy)


def test_cancellation_propagates_before_validation():
    policy,_,raw,pin=fixture()
    def cancelled():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):verify_semantic_qualification_report(raw,pin,policy,check_active=cancelled)


def test_policy_set_order_is_irrelevant_but_full_contents_are_pinned():
    policy,report,_,pin=fixture()
    policy['required_checks']=['quality','z']
    policy['test_fixture_shas']=sorted(['e'*64,'4'*64])
    policy['calibration_shas']=sorted(['6'*64,'3'*64])
    policy['evidence_shas']=sorted(['f'*64,'2'*64])
    report['checks'].append(dict(id='z',passed=True,evidence_sha='2'*64))
    raw=canonical_bytes(report)
    pin=replace(pin,policy_sha=canonical_hash(policy),report=replace(pin.report,sha256=sha256(raw).hexdigest()))
    for key in ('required_checks','test_fixture_shas','calibration_shas','evidence_shas'):policy[key].reverse()
    original=deepcopy(policy)
    assert verify_semantic_qualification_report(raw,pin,policy)==report
    assert policy==original


def test_report_limit_and_canonical_number_representation():
    policy,_,raw,pin=fixture()
    with pytest.raises(AdoptionInvalid):verify_semantic_qualification_report(b' '*(8*1024*1024+1),pin,policy)
    raw=raw.replace(b'"p50_ms":10',b'"p50_ms":10.0')
    pin=replace(pin,report=replace(pin.report,sha256=sha256(raw).hexdigest()))
    with pytest.raises(AdoptionInvalid):verify_semantic_qualification_report(raw,pin,policy)
