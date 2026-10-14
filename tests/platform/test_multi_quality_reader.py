"""Authored scoped reader tests; no live DB or encoded quality evidence."""
from contextlib import contextmanager
from hashlib import sha256
from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4
import pytest
from psycopg.pq import TransactionStatus
from photo_worker.v2.adoption import ArtifactRef, AdoptionInvalid
from photo_worker.v2.multi_quality_reader import PostgresMultiQualityReader


class Cursor:
    def __init__(self, rows): self.rows = rows
    def fetchone(self): return self.rows[0] if self.rows else None
    def fetchall(self): return self.rows


class Connection:
    autocommit = True
    def __init__(self):
        self.info = SimpleNamespace(transaction_status=TransactionStatus.IDLE)
        self.task, self.result, self.params, self.manifest, self.derivative = [uuid4() for _ in range(5)]
        self.version, self.stale, self.role = 'immutable', False, 'photo_scheduler'
    @contextmanager
    def transaction(self):
        assert self.info.transaction_status == TransactionStatus.IDLE
        self.info.transaction_status = TransactionStatus.INTRANS
        try: yield
        finally: self.info.transaction_status = TransactionStatus.IDLE
    def execute(self, sql, args=None):
        assert self.info.transaction_status == TransactionStatus.INTRANS
        if sql == 'SELECT current_user': return Cursor([(self.role,)])
        if 'set_config' in sql or 'statement_timeout' in sql: return Cursor([])
        if 'v29_multi_quality_result' in sql:
            if self.stale: return Cursor([])
            return Cursor([(self.task, self.result, 2, 3, self.manifest, 'a'*64)])
        assert 'v29_multi_quality_evidence' in sql
        return Cursor([('photos', 'params', self.version, sha256(b'{}').hexdigest(),
                        2, 'application/json', 'task_params', 'input')])


class Response(BytesIO):
    def __init__(self, owner): super().__init__(b'{}'); self.owner = owner
    def close(self):
        super().close()
        if self.owner.replace: self.owner.connection.version = 'changed'
    def release_conn(self): self.owner.releases += 1


class Objects:
    def __init__(self, connection):
        self.connection, self.replace, self.opens, self.releases = connection, False, 0, 0
    def get_object(self, bucket, key, *, version_id):
        assert self.connection.info.transaction_status == TransactionStatus.IDLE
        assert (bucket, key, version_id) == ('photos', 'params', 'immutable')
        self.opens += 1
        return Response(self)


def fixture():
    connection = Connection(); objects = Objects(connection)
    reader = PostgresMultiQualityReader(connection, objects, tenant_id=uuid4(),
        run_id=uuid4(), task_id=connection.task, result_id=connection.result,
        attempt=2, fence=3, allowed_bucket='photos')
    return connection, objects, reader, ArtifactRef(connection.params, sha256(b'{}').hexdigest())


def test_original_json_io_outside_transaction_and_current_authority():
    connection, objects, reader, ref = fixture()
    assert reader.read_json(ref, 100) == b'{}'
    assert objects.opens == objects.releases == 1
    connection.stale = True
    with pytest.raises(AdoptionInvalid, match='RESULT_STALE'): reader.evidence(ref)
    assert objects.opens == 1


def test_replaced_locator_after_object_read_is_refused():
    connection, objects, reader, ref = fixture(); objects.replace = True
    with pytest.raises(AdoptionInvalid, match='LOCATOR_CHANGED'): reader.read_json(ref, 100)
    assert objects.opens == objects.releases == 1


def test_outer_transaction_and_wrong_role_refuse_before_io():
    connection, objects, reader, ref = fixture()
    connection.info.transaction_status = TransactionStatus.INTRANS
    with pytest.raises(AdoptionInvalid, match='TRANSACTION_ACTIVE'): reader.read_json(ref, 100)
    connection.info.transaction_status = TransactionStatus.IDLE; connection.role = 'photo_api'
    with pytest.raises(AdoptionInvalid, match='ROLE_INVALID'): reader.read_json(ref, 100)
    assert objects.opens == 0
