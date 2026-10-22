from dataclasses import asdict, replace
from hashlib import sha256
import pytest
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.v2.adoption import AdoptionInvalid, ArtifactRef
from photo_worker.v2.removal_qualification_pin import RemovalQualificationPin
from photo_worker.v2.removal_qualification_report import (
    REMOVAL_REQUIRED_CHECKS, verify_removal_qualification_report,
)
from test_semantic_qualification_report import fixture


def removal_fixture(backend='cuda', peak=10_000_000_000):
    policy, report, _, pin = fixture()
    policy.update(operation='semantic.preview_remove_object', backend=backend,
        required_checks=sorted(REMOVAL_REQUIRED_CHECKS))
    report.update(operation='semantic.preview_remove_object')
    report['input_fixture_shas'] = list(policy['test_fixture_shas'])
    report['checks'] = [dict(id=key, passed=True, evidence_sha=policy['evidence_shas'][0])
        for key in sorted(REMOVAL_REQUIRED_CHECKS)]
    report['metrics']['peak_vram_bytes'] = peak
    raw = canonical_bytes(report); fields = asdict(pin)
    fields.update(backend=backend, policy_sha=canonical_hash(policy), calibration=pin.calibration,
        report=ArtifactRef(pin.report.artifact_id, sha256(raw).hexdigest()))
    return policy, report, raw, RemovalQualificationPin(**fields)


@pytest.mark.parametrize('backend,peak', [('cpu',0),('cuda',10_000_000_000),('mps',12_000_000_000)])
def test_backend_memory_policy(backend, peak):
    policy, report, raw, pin = removal_fixture(backend, peak)
    assert verify_removal_qualification_report(raw,pin,policy,check_active=lambda:None)==report


@pytest.mark.parametrize('backend,peak', [('cpu',1),('cuda',10_000_000_001),('mps',1.5)])
def test_invalid_memory_report_denied(backend, peak):
    policy, _, raw, pin = removal_fixture(backend, peak)
    with pytest.raises(AdoptionInvalid):
        verify_removal_qualification_report(raw,pin,policy,check_active=lambda:None)


def test_repinned_policy_cannot_remove_mandatory_check():
    policy, _, raw, pin = removal_fixture()
    policy['required_checks'].remove('removal.real_photo_review')
    pin = replace(pin, policy_sha=canonical_hash(policy))
    with pytest.raises(AdoptionInvalid):
        verify_removal_qualification_report(raw,pin,policy,check_active=lambda:None)


@pytest.mark.parametrize('initially_complete', [True, False])
@pytest.mark.parametrize('mutation_checkpoint', [1, 2])
def test_mandatory_floor_owns_policy_before_callbacks(initially_complete, mutation_checkpoint):
    policy, report, raw, pin = removal_fixture()
    if not initially_complete:
        policy['required_checks'].remove('removal.real_photo_review')
        pin = replace(pin, policy_sha=canonical_hash(policy))
    calls = 0
    def active():
        nonlocal calls
        calls += 1
        if calls == mutation_checkpoint:
            policy['required_checks'] = [] if initially_complete else sorted(REMOVAL_REQUIRED_CHECKS)
            policy['test_fixture_shas'] = [] if initially_complete else list(report['input_fixture_shas'])
    if initially_complete:
        assert verify_removal_qualification_report(raw, pin, policy, check_active=active) == report
    else:
        with pytest.raises(AdoptionInvalid, match='REPORT_INVALID'):
            verify_removal_qualification_report(raw, pin, policy, check_active=active)
    assert calls >= mutation_checkpoint
