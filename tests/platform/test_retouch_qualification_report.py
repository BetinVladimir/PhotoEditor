"""Synthetic report-verifier scenarios; no qualified photographic results generated."""
from dataclasses import asdict,replace
from hashlib import sha256
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.v2.adoption import AdoptionInvalid,ArtifactRef
from photo_worker.v2.retouch_qualification_pin import RetouchQualificationPin
from photo_worker.v2.retouch_qualification_report import RETOUCH_REQUIRED_CHECKS,verify_retouch_qualification_report
from test_semantic_qualification_report import fixture


def retouch_fixture():
    policy,report,_,pin=fixture()
    policy.update(operation='semantic.preview_retouch_region',backend='cpu',required_checks=sorted(RETOUCH_REQUIRED_CHECKS))
    report.update(operation='semantic.preview_retouch_region',input_fixture_shas=list(policy['test_fixture_shas']))
    report['checks']=[dict(id=key,passed=True,evidence_sha=policy['evidence_shas'][0]) for key in sorted(RETOUCH_REQUIRED_CHECKS)]
    report['metrics']['peak_vram_bytes']=0
    raw=canonical_bytes(report);fields=asdict(pin)
    fields.update(backend='cpu',policy_sha=canonical_hash(policy),calibration=pin.calibration,
        report=ArtifactRef(pin.report.artifact_id,sha256(raw).hexdigest()))
    return policy,report,raw,RetouchQualificationPin(**fields)


def test_retouch_exact_report_keeps_cpu_memory_and_own_operation():
    policy,report,raw,pin=retouch_fixture()
    assert verify_retouch_qualification_report(raw,pin,policy,check_active=lambda:None)==report
    value=asdict(pin)
    for key in ('policy_id','qualification_id','issuer_id'):value[key]=str(value[key])
    for key in ('report','calibration'):value[key]['artifact_id']=str(value[key]['artifact_id'])
    assert RetouchQualificationPin.from_value(value)==pin


@pytest.mark.parametrize('check',sorted(RETOUCH_REQUIRED_CHECKS))
def test_retouch_policy_cannot_drop_mandatory_check(check):
    policy,_,raw,pin=retouch_fixture();policy['required_checks'].remove(check)
    pin=replace(pin,policy_sha=canonical_hash(policy))
    with pytest.raises(AdoptionInvalid):verify_retouch_qualification_report(raw,pin,policy,check_active=lambda:None)


@pytest.mark.parametrize('peak',[True,.5,1,10000000000])
def test_retouch_cpu_report_refuses_invalid_vram_metric(peak):
    policy,report,_,pin=retouch_fixture();report['metrics']['peak_vram_bytes']=peak
    raw=canonical_bytes(report);pin=replace(pin,report=ArtifactRef(pin.report.artifact_id,sha256(raw).hexdigest()))
    with pytest.raises(AdoptionInvalid):verify_retouch_qualification_report(raw,pin,policy,check_active=lambda:None)


@pytest.mark.parametrize('backend',['cuda','mps'])
def test_retouch_cpu_algorithm_refuses_other_backend_pin(backend):
    _,_,_,pin=retouch_fixture()
    with pytest.raises(AdoptionInvalid):replace(pin,backend=backend)


@pytest.mark.parametrize('failure',['operation','fixture'])
def test_retouch_report_refuses_foreign_operation_or_missing_photo(failure):
    policy,report,_,pin=retouch_fixture()
    if failure=='operation':report['operation']='semantic.preview_background_cleanup'
    else:report['input_fixture_shas']=report['input_fixture_shas'][1:]
    raw=canonical_bytes(report);pin=replace(pin,report=ArtifactRef(pin.report.artifact_id,sha256(raw).hexdigest()))
    with pytest.raises(AdoptionInvalid):verify_retouch_qualification_report(raw,pin,policy,check_active=lambda:None)


@pytest.mark.parametrize('initially_complete', [True, False])
def test_retouch_mandatory_floor_uses_owned_policy_despite_activity_callback_mutation(initially_complete):
    policy, report, raw, pin = retouch_fixture()
    if not initially_complete:
        policy['required_checks'].remove('retouch.real_photo_review')
        pin = replace(pin, policy_sha=canonical_hash(policy))
    calls = 0
    def active():
        nonlocal calls
        calls += 1
        # First call precedes common verification's policy copy; the second
        # occurs after that copy, while report checks are being traversed.
        if calls == 2:
            policy['required_checks'] = ([] if initially_complete else sorted(RETOUCH_REQUIRED_CHECKS))
            policy['test_fixture_shas'] = [] if initially_complete else list(report['input_fixture_shas'])
    if initially_complete:
        assert verify_retouch_qualification_report(raw, pin, policy, check_active=active) == report
    else:
        with pytest.raises(AdoptionInvalid, match='REPORT_INVALID'):
            verify_retouch_qualification_report(raw, pin, policy, check_active=active)
    assert calls >= 2
