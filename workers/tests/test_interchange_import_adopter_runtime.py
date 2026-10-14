"""An installed importer advertises readiness only while its loop is live."""

from threading import Event
from types import SimpleNamespace

import pytest

from photo_worker.v2.interchange_import_adopter_runtime import (
    ImportAdopterRuntimeLease, require_import_adopter_runtime_schema)
from photo_worker.v2.service import AdopterConfig


class Connection:
    autocommit = True
    def __init__(self):
        self.calls = []
    def execute(self, sql, params=()):
        self.calls.append((sql, params))
        if sql == 'SELECT current_user':
            return SimpleNamespace(fetchone=lambda: ('photo_scheduler',))
        return SimpleNamespace(fetchone=lambda: (True,))


def test_import_adopter_runtime_schema_requires_scheduler_and_closed_acl():
    connection = Connection()
    require_import_adopter_runtime_schema(connection)
    assert len(connection.calls) == 4
    assert 'photo.v30_import_runtime_ready(text)' in connection.calls[-1][0]
    assert 'p_bucket' in connection.calls[-1][0]
    connection.autocommit = False
    with pytest.raises(ValueError, match='SCHEMA'):
        require_import_adopter_runtime_schema(connection)


def test_import_adopter_runtime_announces_and_withdraws_on_stop():
    connection, stop = Connection(), Event()
    with ImportAdopterRuntimeLease(connection, stop, 'photo') as lease:
        assert lease.instance.int != 0
    assert 'announce_v30_import_adopter_runtime' in connection.calls[0][0]
    assert 'withdraw_v30_import_adopter_runtime' in connection.calls[-1][0]
    assert connection.calls[0][1] == (connection.calls[-1][1][0], 'photo')


def test_import_adopter_runtime_renew_failure_stops_adopter():
    connection, stop = Connection(), Event()
    lease = ImportAdopterRuntimeLease(connection, stop, 'photo')
    calls = 0
    def broken():
        nonlocal calls
        calls += 1
        raise RuntimeError('lost')
    lease._announce = broken
    lease._halt = SimpleNamespace(wait=lambda _seconds: False)
    lease._renew()
    assert calls == 1 and stop.is_set()
    assert str(lease._failure) == 'INTERCHANGE_IMPORT_ADOPTER_RUNTIME_LOST'


@pytest.mark.parametrize('bucket', ('', 'ab', 'x' * 64))
def test_import_adopter_runtime_rejects_invalid_bucket(bucket):
    with pytest.raises(ValueError, match='INSTALLATION'):
        ImportAdopterRuntimeLease(Connection(), Event(), bucket)
    environment = {'DATABASE_URL': 'postgres://example',
                   'S3_ENDPOINT': 'https://s3.example.test',
                   'S3_BUCKET': bucket, 'S3_ACCESS_KEY': 'key',
                   'S3_SECRET_KEY': 'secret'}
    with pytest.raises(ValueError, match='bucket'):
        AdopterConfig.from_env(environment)
