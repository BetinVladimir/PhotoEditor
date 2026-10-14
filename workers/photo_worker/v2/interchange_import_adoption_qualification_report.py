"""Python verifier for the V30 imported v2qualification report contract.

The caller must obtain Expected from an operator-controlled deployment and
repeat the current scheduler SQL/object pin around this pure byte check.
"""

from datetime import datetime, timezone
from hashlib import sha256
from math import isfinite
import re
from uuid import UUID

from photo_worker.contracts.base import canonical_bytes, parse_json
from .adoption import AdoptionInvalid


EXPECTED_KEYS = frozenset(('Operation', 'Version', 'Backend', 'ModelLockSHA',
    'RuntimeLockSHA', 'DeviceProfileSHA', 'DatasetSHA', 'IssuerID',
    'RequiredChecks', 'CalibrationSHAs', 'TestFixtureSHAs', 'EvidenceSHAs',
    'LicenseCleared'))
REPORT_KEYS = frozenset(('schema_version', 'operation', 'version',
    'model_lock_sha', 'runtime_lock_sha', 'device_profile_sha', 'dataset_sha',
    'input_fixture_shas', 'checks', 'metrics', 'status', 'issuer_id', 'created_at'))
METRIC_KEYS = frozenset(('p50_ms', 'p95_ms', 'peak_vram_bytes',
                         'peak_rss_bytes', 'scratch_bytes'))
TIMESTAMP = re.compile(r'^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$')


def _invalid():
    raise AdoptionInvalid('INTERCHANGE_IMPORT_QUALIFICATION_REPORT_INVALID')


def _digest(value):
    return isinstance(value, str) and re.fullmatch('[0-9a-f]{64}', value) is not None


def _list(value, maximum, *, hashes=False, minimum=1):
    if not isinstance(value, list) or not minimum <= len(value) <= maximum:
        _invalid()
    if hashes:
        if any(not _digest(item) for item in value):
            _invalid()
    elif any(not isinstance(item, str) or not 1 <= len(item.encode('utf-8')) <= 100
             for item in value):
        _invalid()
    if len(set(value)) != len(value):
        _invalid()
    return set(value)


def validate_import_adoption_expected(expected):
    """Validate process-selected Go Expected and return owned lookup sets."""
    if not isinstance(expected, dict) or set(expected) != EXPECTED_KEYS:
        _invalid()
    if (expected['Operation'] not in ('image.denoise', 'image.upscale',
            'color.stats', 'mask.person') or expected['Version'] != '1'
            or expected['Backend'] not in ('cpu', 'cuda', 'mps')
            or expected['LicenseCleared'] is not True
            or any(not _digest(expected[key]) for key in ('ModelLockSHA',
                'RuntimeLockSHA', 'DeviceProfileSHA', 'DatasetSHA'))):
        _invalid()
    try:
        issuer = UUID(expected['IssuerID'])
    except (TypeError, ValueError, AttributeError) as error:
        raise AdoptionInvalid('INTERCHANGE_IMPORT_QUALIFICATION_REPORT_INVALID') from error
    if issuer.int == 0 or str(issuer) != expected['IssuerID']:
        _invalid()
    required = _list(expected['RequiredChecks'], 1000)
    trusted = {}
    for key, minimum in (('CalibrationSHAs', 0), ('TestFixtureSHAs', 1),
                         ('EvidenceSHAs', 1)):
        values = expected[key]
        if (not isinstance(values, dict) or not minimum <= len(values) <= 10000
                or any(not _digest(item) or value != {}
                       for item, value in values.items())):
            _invalid()
        trusted[key] = set(values)
    if trusted['CalibrationSHAs'] & trusted['TestFixtureSHAs']:
        _invalid()
    return required, trusted


def verify_import_adoption_qualification_report(raw, expected_sha, expected):
    """Match Go v2qualification.Verify without granting SQL or issuer authority."""
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 8 << 20
            or not _digest(expected_sha) or sha256(raw).hexdigest() != expected_sha):
        _invalid()
    try:
        required, trusted = validate_import_adoption_expected(expected)
        report = parse_json(raw, max_bytes=8 << 20)
        if (not isinstance(report, dict) or set(report) != REPORT_KEYS
                or canonical_bytes(report) != raw
                or type(report['schema_version']) is not int or report['schema_version'] != 1
                or report['operation'] != expected['Operation']
                or report['version'] != expected['Version']
                or report['status'] != 'passed'
                or report['issuer_id'] != expected['IssuerID']):
            _invalid()
        for field, key in (('model_lock_sha', 'ModelLockSHA'),
                           ('runtime_lock_sha', 'RuntimeLockSHA'),
                           ('device_profile_sha', 'DeviceProfileSHA'),
                           ('dataset_sha', 'DatasetSHA')):
            if report[field] != expected[key]:
                _invalid()
        stamp = report['created_at']
        if not isinstance(stamp, str) or TIMESTAMP.fullmatch(stamp) is None:
            _invalid()
        created = datetime.fromisoformat(stamp.replace('Z', '+00:00'))
        if created.tzinfo is None or created.astimezone(timezone.utc) == datetime.min.replace(tzinfo=timezone.utc):
            _invalid()
        fixtures = _list(report['input_fixture_shas'], 10000, hashes=True)
        if (not fixtures <= trusted['TestFixtureSHAs']
                or fixtures & trusted['CalibrationSHAs']):
            _invalid()
        metrics = report['metrics']
        if not isinstance(metrics, dict) or set(metrics) != METRIC_KEYS:
            _invalid()
        if any(type(value) not in (int, float) or not isfinite(value)
               or not 0 <= value <= 2**53-1 for value in metrics.values()):
            _invalid()
        if metrics['p95_ms'] < metrics['p50_ms']:
            _invalid()
        checks = report['checks']
        if not isinstance(checks, list) or not 1 <= len(checks) <= 1000:
            _invalid()
        seen = set()
        for check in checks:
            if (not isinstance(check, dict) or set(check) != {'id', 'passed', 'evidence_sha'}
                    or not isinstance(check['id'], str)
                    or not 1 <= len(check['id'].encode('utf-8')) <= 100
                    or check['id'] in seen or check['passed'] is not True
                    or check['evidence_sha'] not in trusted['EvidenceSHAs']):
                _invalid()
            seen.add(check['id'])
        if not required <= seen:
            _invalid()
        return report
    except (TypeError, ValueError, KeyError, OverflowError, UnicodeError) as error:
        raise AdoptionInvalid('INTERCHANGE_IMPORT_QUALIFICATION_REPORT_INVALID') from error
