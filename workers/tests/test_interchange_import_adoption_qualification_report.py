"""Imported report bytes must match the process-pinned Go qualification ABI."""

from copy import deepcopy
from hashlib import sha256
from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_import_adoption_qualification_report import (
    verify_import_adoption_qualification_report)


def fixture():
    fixture_sha, evidence_sha = 'a' * 64, 'b' * 64
    expected = {
        'Operation': 'image.denoise', 'Version': '1', 'Backend': 'mps',
        'ModelLockSHA': 'c' * 64, 'RuntimeLockSHA': 'd' * 64,
        'DeviceProfileSHA': 'e' * 64, 'DatasetSHA': 'f' * 64,
        'IssuerID': str(uuid4()), 'RequiredChecks': ['quality'],
        'CalibrationSHAs': {}, 'TestFixtureSHAs': {fixture_sha: {}},
        'EvidenceSHAs': {evidence_sha: {}}, 'LicenseCleared': True,
    }
    report = {
        'schema_version': 1, 'operation': expected['Operation'], 'version': '1',
        'model_lock_sha': expected['ModelLockSHA'],
        'runtime_lock_sha': expected['RuntimeLockSHA'],
        'device_profile_sha': expected['DeviceProfileSHA'],
        'dataset_sha': expected['DatasetSHA'],
        'input_fixture_shas': [fixture_sha],
        'checks': [{'id': 'quality', 'passed': True, 'evidence_sha': evidence_sha}],
        'metrics': {'p50_ms': 1, 'p95_ms': 2, 'peak_vram_bytes': 1024,
                    'peak_rss_bytes': 2048, 'scratch_bytes': 4096},
        'status': 'passed', 'issuer_id': expected['IssuerID'],
        'created_at': '2026-10-14T12:00:00Z',
    }
    return expected, report


def verify(expected, report):
    raw = canonical_bytes(report)
    return verify_import_adoption_qualification_report(
        raw, sha256(raw).hexdigest(), expected)


def test_import_adoption_report_verifies_exact_pinned_evidence():
    expected, report = fixture()
    assert verify(expected, report) == report


@pytest.mark.parametrize('change', ('model', 'check', 'evidence', 'metric', 'issuer', 'fields'))
def test_import_adoption_report_rejects_drift(change):
    expected, report = fixture()
    report = deepcopy(report)
    if change == 'model':
        report['model_lock_sha'] = '0' * 64
    elif change == 'check':
        report['checks'][0]['passed'] = False
    elif change == 'evidence':
        report['checks'][0]['evidence_sha'] = '0' * 64
    elif change == 'metric':
        report['metrics']['p95_ms'] = 0
    elif change == 'issuer':
        report['issuer_id'] = str(uuid4())
    else:
        report['extra'] = True
    with pytest.raises(AdoptionInvalid, match='REPORT_INVALID'):
        verify(expected, report)


def test_import_adoption_report_rejects_noncanonical_bytes_and_untrusted_policy():
    expected, report = fixture()
    raw = canonical_bytes(report) + b' '
    with pytest.raises(AdoptionInvalid, match='REPORT_INVALID'):
        verify_import_adoption_qualification_report(raw, sha256(raw).hexdigest(), expected)
    expected['LicenseCleared'] = False
    with pytest.raises(AdoptionInvalid, match='REPORT_INVALID'):
        verify(expected, report)


@pytest.mark.parametrize('operation', ['defects.auto_repair', 'sensor.dust_auto_repair'])
@pytest.mark.parametrize('mode', ['valid', 'mps', 'unmeasured', 'missing_check'])
def test_native_auto_reports_require_cpu_and_unchanged_measured_evidence(operation, mode):
    expected, report = fixture()
    expected['Operation']=report['operation']=operation
    expected['Backend']='mps' if mode=='mps' else 'cpu'
    if mode=='unmeasured': report['status']='pending'
    if mode=='missing_check': report['checks']=[]
    if mode=='valid': assert verify(expected, report)==report
    else:
        with pytest.raises(AdoptionInvalid): verify(expected, report)
