from dataclasses import asdict,replace
from hashlib import sha256
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.v2.adoption import AdoptionInvalid,ArtifactRef
from photo_worker.v2.exposure_qualification_pin import ExposureQualificationPin
from photo_worker.v2.semantic_qualification_report import verify_exposure_qualification_report,verify_semantic_qualification_report
from test_semantic_qualification_report import fixture


def exposure_fixture():
    policy,report,_,pin=fixture()
    policy.update(operation='semantic.local_exposure',backend='cpu');report['operation']='semantic.local_exposure'
    raw=canonical_bytes(report);fields=asdict(pin)
    fields.update(backend='cpu',policy_sha=canonical_hash(policy),
        report=ArtifactRef(pin.report.artifact_id,sha256(raw).hexdigest()),calibration=pin.calibration)
    return policy,report,raw,ExposureQualificationPin(**fields)


def test_exact_cpu_report_does_not_qualify_transfer():
    policy,report,raw,pin=exposure_fixture()
    assert verify_exposure_qualification_report(raw,pin,policy,check_active=lambda:None)==report
    with pytest.raises(AdoptionInvalid):verify_semantic_qualification_report(raw,pin,policy)
    policy,_,raw,pin=fixture()
    with pytest.raises(AdoptionInvalid):verify_exposure_qualification_report(raw,pin,policy,check_active=lambda:None)


@pytest.mark.parametrize('field,value',[('operation','semantic.transfer'),('status','failed'),('runtime_lock_sha','0'*64),('schema_version',True)])
def test_repinned_bad_cpu_report_denied(field,value):
    policy,report,_,pin=exposure_fixture();report[field]=value;raw=canonical_bytes(report)
    pin=replace(pin,report=ArtifactRef(pin.report.artifact_id,sha256(raw).hexdigest()))
    with pytest.raises(AdoptionInvalid):verify_exposure_qualification_report(raw,pin,policy,check_active=lambda:None)
