"""Import qualification report locators stay bound to live scheduler scope."""

from hashlib import sha256
from io import BytesIO
from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_import_adoption_qualification_catalog import (
    SQLImportAdoptionQualificationCatalog)


def fixture(report_bytes=None):
    scope = (uuid4(), uuid4(), 1, 7, uuid4())
    report = uuid4()
    digest = sha256(report_bytes).hexdigest() if report_bytes is not None else 'a' * 64
    expected = {'Operation': 'image.denoise', 'Version': '1',
                'LicenseCleared': True}
    row = [report, digest, 'photos', 'qualification/report.json', 'version-1',
           len(report_bytes) if report_bytes is not None else 1024]
    class Connection:
        autocommit = True
        def execute(self, query, args):
            assert 'v30_import_adoption_qualification_object' in query
            assert args[:5] == scope
            assert args[5] == 'image.denoise'
            assert args[-2:] == (report, digest)
            return self
        def fetchall(self):
            return [tuple(row)]
    return SQLImportAdoptionQualificationCatalog(Connection(), 'photos', lambda: None), scope, expected, report, digest, row


def test_import_qualification_catalog_rechecks_object_version():
    catalog, scope, expected, report, digest, row = fixture()
    pin = catalog.lookup(scope, 'image.denoise', expected, report, digest)
    catalog.recheck(scope, 'image.denoise', expected, report, digest, pin)
    row[4] = 'version-2'
    with pytest.raises(AdoptionInvalid, match='CHANGED'):
        catalog.recheck(scope, 'image.denoise', expected, report, digest, pin)


def test_import_qualification_catalog_rejects_foreign_bucket_and_policy():
    catalog, scope, expected, report, digest, row = fixture()
    row[2] = 'foreign'
    with pytest.raises(AdoptionInvalid, match='OBJECT'):
        catalog.lookup(scope, 'image.denoise', expected, report, digest)
    with pytest.raises(AdoptionInvalid, match='POLICY'):
        catalog.lookup(scope, 'image.denoise', {**expected, 'LicenseCleared': False},
                       report, digest)
    with pytest.raises(AdoptionInvalid, match='SCOPE'):
        catalog.lookup(scope, 'unknown.operation', expected, report, digest)


def test_import_qualification_catalog_reads_exact_version_and_rechecks():
    raw = b'{"schema_version":1}'
    catalog, scope, expected, report, digest, row = fixture(raw)
    calls = []
    class Objects:
        def get_object(self, bucket, key, *, version_id):
            calls.append((bucket, key, version_id))
            return BytesIO(raw)
    assert catalog.read_report(Objects(), scope, 'image.denoise', expected,
                               report, digest) == raw
    assert calls == [('photos', 'qualification/report.json', 'version-1')]
    class RotatingObjects(Objects):
        def get_object(self, bucket, key, *, version_id):
            row[4] = 'version-2'
            return super().get_object(bucket, key, version_id=version_id)
    with pytest.raises(AdoptionInvalid, match='CHANGED'):
        catalog.read_report(RotatingObjects(), scope, 'image.denoise', expected,
                            report, digest)


def test_import_qualification_catalog_rejects_physical_hash_mismatch():
    raw = b'{"schema_version":1}'
    catalog, scope, expected, report, digest, _ = fixture(raw)
    class Objects:
        def get_object(self, *_args, **_kwargs):
            return BytesIO(b'{"schema_version":2}')
    with pytest.raises(AdoptionInvalid, match='HASH'):
        catalog.read_report(Objects(), scope, 'image.denoise', expected,
                            report, digest)


def test_import_qualification_catalog_verifies_report_semantics_under_live_pin():
    from test_interchange_import_adoption_qualification_report import fixture as report_fixture
    expected, document = report_fixture()
    raw = canonical_bytes(document)
    catalog, scope, _, report, digest, _ = fixture(raw)
    class Objects:
        def get_object(self, *_args, **_kwargs):
            return BytesIO(raw)
    assert catalog.verified_report(Objects(), scope, 'image.denoise', expected,
                                   report, digest) == document
