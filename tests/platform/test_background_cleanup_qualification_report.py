from dataclasses import asdict, replace
from hashlib import sha256
import pytest
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.v2.adoption import AdoptionInvalid, ArtifactRef
from photo_worker.v2.background_cleanup_qualification_pin import BackgroundCleanupQualificationPin
from photo_worker.v2.background_cleanup_qualification_report import (
    BACKGROUND_CLEANUP_REQUIRED_CHECKS, verify_background_cleanup_qualification_report,
)
from test_semantic_qualification_report import fixture


def background_cleanup_fixture(backend='cuda', peak=10_000_000_000):
    policy, report, _, pin = fixture()
    policy.update(operation='semantic.preview_background_cleanup', backend=backend,
        required_checks=sorted(BACKGROUND_CLEANUP_REQUIRED_CHECKS))
    report.update(operation='semantic.preview_background_cleanup')
    report['input_fixture_shas'] = list(policy['test_fixture_shas'])
    report['checks'] = [dict(id=key, passed=True, evidence_sha=policy['evidence_shas'][0])
        for key in sorted(BACKGROUND_CLEANUP_REQUIRED_CHECKS)]
    report['metrics']['peak_vram_bytes'] = peak
    raw = canonical_bytes(report); fields = asdict(pin)
    fields.update(backend=backend, policy_sha=canonical_hash(policy), calibration=pin.calibration,
        report=ArtifactRef(pin.report.artifact_id, sha256(raw).hexdigest()))
    return policy, report, raw, BackgroundCleanupQualificationPin(**fields)


@pytest.mark.parametrize('backend,peak', [('cpu',0),('cuda',10_000_000_000),('mps',12_000_000_000)])
def test_backend_memory_policy(backend, peak):
    policy, report, raw, pin = background_cleanup_fixture(backend, peak)
    assert verify_background_cleanup_qualification_report(raw,pin,policy,check_active=lambda:None)==report


@pytest.mark.parametrize('backend,peak', [('cpu',1),('cuda',10_000_000_001),('mps',1.5)])
def test_invalid_memory_report_denied(backend, peak):
    policy, _, raw, pin = background_cleanup_fixture(backend, peak)
    with pytest.raises(AdoptionInvalid):
        verify_background_cleanup_qualification_report(raw,pin,policy,check_active=lambda:None)


@pytest.mark.parametrize('check', sorted(BACKGROUND_CLEANUP_REQUIRED_CHECKS))
def test_repinned_policy_cannot_remove_mandatory_check(check):
    policy, _, raw, pin = background_cleanup_fixture()
    policy['required_checks'].remove(check)
    pin = replace(pin, policy_sha=canonical_hash(policy))
    with pytest.raises(AdoptionInvalid):
        verify_background_cleanup_qualification_report(raw,pin,policy,check_active=lambda:None)


def test_removal_pin_cannot_qualify_background_cleanup():
    from photo_worker.v2.removal_qualification_pin import RemovalQualificationPin
    policy, _, raw, pin = background_cleanup_fixture()
    values=asdict(pin)
    values.update(report=pin.report,calibration=pin.calibration)
    foreign=RemovalQualificationPin(**values)
    with pytest.raises(AdoptionInvalid,match='PIN_INVALID'):
        verify_background_cleanup_qualification_report(raw,foreign,policy,check_active=lambda:None)


def test_repinned_removal_report_cannot_qualify_background_cleanup():
    policy, report, _, pin = background_cleanup_fixture()
    report['operation']='semantic.preview_remove_object'
    raw=canonical_bytes(report)
    pin=replace(pin,report=ArtifactRef(pin.report.artifact_id,sha256(raw).hexdigest()))
    with pytest.raises(AdoptionInvalid):
        verify_background_cleanup_qualification_report(raw,pin,policy,check_active=lambda:None)


@pytest.mark.parametrize('mutation', ['missing','extra'])
def test_repinned_report_requires_exact_photo_fixture_set(mutation):
    policy, report, _, pin = background_cleanup_fixture()
    if mutation == 'missing':
        report['input_fixture_shas'] = report['input_fixture_shas'][1:]
    else:
        foreign = sha256(b'foreign-photo-fixture').hexdigest()
        assert foreign not in report['input_fixture_shas']
        report['input_fixture_shas'].append(foreign)
    raw = canonical_bytes(report)
    pin = replace(pin, report=ArtifactRef(pin.report.artifact_id,sha256(raw).hexdigest()))
    with pytest.raises(AdoptionInvalid):
        verify_background_cleanup_qualification_report(raw,pin,policy,check_active=lambda:None)


@pytest.mark.parametrize('revoke_at', [1,2])
def test_background_cleanup_report_propagates_live_authority_revocation(revoke_at):
    policy, _, raw, pin = background_cleanup_fixture()
    calls = 0
    failure = AdoptionInvalid('FIXTURE_AUTHORITY_REVOKED')
    def check_active():
        nonlocal calls
        calls += 1
        if calls == revoke_at:
            raise failure
    with pytest.raises(AdoptionInvalid) as caught:
        verify_background_cleanup_qualification_report(raw,pin,policy,check_active=check_active)
    assert caught.value is failure
    assert calls == revoke_at


@pytest.mark.parametrize('initially_complete', [True, False])
@pytest.mark.parametrize('mutation_checkpoint', [1, 2])
def test_mandatory_floor_owns_policy_before_callbacks(initially_complete, mutation_checkpoint):
    policy, report, raw, pin = background_cleanup_fixture()
    if not initially_complete:
        policy['required_checks'].remove('background_cleanup.real_photo_review')
        pin = replace(pin, policy_sha=canonical_hash(policy))
    calls = 0
    def active():
        nonlocal calls
        calls += 1
        if calls == mutation_checkpoint:
            policy['required_checks'] = [] if initially_complete else sorted(BACKGROUND_CLEANUP_REQUIRED_CHECKS)
            policy['test_fixture_shas'] = [] if initially_complete else list(report['input_fixture_shas'])
    if initially_complete:
        assert verify_background_cleanup_qualification_report(raw, pin, policy, check_active=active) == report
    else:
        with pytest.raises(AdoptionInvalid, match='REPORT_INVALID'):
            verify_background_cleanup_qualification_report(raw, pin, policy, check_active=active)
    assert calls >= mutation_checkpoint
