"""Exact worker SQL transport; DB query is isolated, migrations not executed."""
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.runtime.database import TaskDatabase


def fixture(row):
    params=SimpleNamespace(artifact_id=uuid4(),sha256='a'*64)
    command=SimpleNamespace(tenant_id=uuid4(),run_id=uuid4(),task_id=uuid4(),attempt=3,params=params)
    lease=SimpleNamespace(command=command,fence=9,worker_id=uuid4(),input_digest='b'*64)
    db=object.__new__(TaskDatabase);calls=[]
    def query(sql,args):calls.append((sql,args));return row
    db._query=query
    return db,lease,calls


@pytest.mark.parametrize('row',[None,(False,),(1,),(True,)])
def test_retouch_worker_admission_accepts_only_sql_true(row):
    db,lease,calls=fixture(row)
    assert db.admit_semantic_retouch_preview(lease,b'{"kind":"retouch_region"}') is (row is not None and row[0] is True)
    sql,args=calls[0]
    assert 'photo.admit_semantic_retouch_preview' in sql and sql.count('%s')==len(args)==10
    assert args[0:6]==(lease.command.tenant_id,lease.command.run_id,lease.command.task_id,3,9,lease.worker_id)
    assert args[-2:]==('{"kind":"retouch_region"}',lease.input_digest)


def test_retouch_report_transport_binds_context_and_exact_unicode_bytes():
    record={'pin':'owned'};db,lease,calls=fixture((record,))
    context=SimpleNamespace(artifact_id=uuid4(),sha256='c'*64)
    params='{"kind":"retouch_region","note":"тест"}'.encode()
    body='{"algorithm":"beautyfilter_v1"}'.encode()
    assert db.retouch_preview_report_evidence(lease,params,context,body)==record
    sql,args=calls[0]
    assert 'photo.v27_retouch_preview_report_evidence' in sql and sql.count('%s')==len(args)==13
    assert args[8]==params.decode() and args[-3:]==(context.artifact_id,context.sha256,body.decode())


@pytest.mark.parametrize('row',[None,(False,),(1,),(True,)])
def test_retouch_landmark_origin_transports_exact_lease_and_ref(row):
    db,lease,calls=fixture(row)
    landmark=SimpleNamespace(artifact_id=uuid4(),sha256='c'*64)
    assert db.retouch_preview_landmark_origin(lease,b'{}',landmark) is (row is not None and row[0] is True)
    sql,args=calls[0]
    assert 'photo.semantic_retouch_preview_worker_landmark_origin' in sql
    assert sql.count('%s')==len(args)==12
    assert args[:6]==(lease.command.tenant_id,lease.command.run_id,lease.command.task_id,3,9,lease.worker_id)
    assert args[-4:]==('{}',lease.input_digest,landmark.artifact_id,landmark.sha256)


@pytest.mark.parametrize('row',[None,(False,),(1,),(True,)])
def test_retouch_protection_origin_transports_exact_roots(row):
    db,lease,calls=fixture(row)
    root=SimpleNamespace(artifact_id=uuid4(),sha256='c'*64)
    binary=SimpleNamespace(artifact_id=uuid4(),sha256='d'*64)
    assert db.retouch_preview_protection_origin(lease,b'{}',root,binary) is (row is not None and row[0] is True)
    sql,args=calls[0]
    assert 'photo.semantic_retouch_preview_protection_origin' in sql and sql.count('%s')==len(args)==14
    assert args[-4:]==(root.artifact_id,root.sha256,binary.artifact_id,binary.sha256)
    assert args[:6]==(lease.command.tenant_id,lease.command.run_id,lease.command.task_id,3,9,lease.worker_id)
