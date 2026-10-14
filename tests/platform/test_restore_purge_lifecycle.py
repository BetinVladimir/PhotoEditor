from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import ssl
import psycopg
import pytest
from photo_worker.runtime import restore_purge_lifecycle as module


@pytest.mark.parametrize('mode',['valid','revision_conflict','redirect','network_error','bad_job','bad_status','scope_error','activity_false','scope_false'])
def test_fixture_delete_uses_canonical_api_without_retry(monkeypatch,mode):
    tenant,job=uuid4(),uuid4();calls=[];closed=[]
    target=module.RestoreTenantDeletionTarget('https://restore.test',tenant,7,'Bearer secret')
    class Connection:
        def __init__(self,*args,**kwargs):pass
        def request(self,method,path,headers):
            calls.append(method);assert method=='DELETE' and path=='/api/v1/tenants/'+str(tenant)
            assert headers['If-Match']=='"7"' and headers['X-Photo-Client']=='native'
            if mode=='network_error':raise OSError('secret endpoint')
        def getresponse(self):
            raw=(' {"job_id":"'+('not-uuid' if mode=='bad_job' else str(job))+'","status":"'+('completed' if mode=='bad_status' else 'queued')+'"}').encode()
            return SimpleNamespace(status=409 if mode=='revision_conflict' else (302 if mode=='redirect' else 202),
                read=lambda _:raw,getheader=lambda name,default=None:'application/json' if name=='Content-Type' else default)
        def close(self):closed.append(True)
    monkeypatch.setattr(module.http.client,'HTTPSConnection',Connection)
    def scope(actual):
        assert actual is target
        if mode=='scope_error':raise RuntimeError('wrong fixture')
        if mode=='scope_false':return False
    def run():return module.request_fixture_tenant_deletion(target,tls_context=ssl.create_default_context(),assert_fixture_scope=scope,check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert run()==job
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    assert len(calls)<=1 and len(closed)==len(calls)
    if mode in ('activity_false','scope_false'):assert calls==[]
    assert 'Bearer secret' not in repr(target)


@pytest.mark.parametrize('mode',['valid','foreign_database','missing_job','failed_job','bad_tombstone','live_claim','timeout'])
def test_fixture_completion_wait_is_bounded_and_read_only(monkeypatch,mode):
    tenant,job=uuid4(),uuid4();polls=[];clock=[0.0];queries=[]
    monkeypatch.setattr(module,'monotonic',lambda:clock[0])
    monkeypatch.setattr(module,'sleep',lambda seconds:clock.__setitem__(0,clock[0]+seconds))
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,statement,params=None):
            queries.append(statement);row=None
            if statement.startswith('SELECT current_database'):row=('foreign' if mode=='foreign_database' else 'isolated',True)
            elif statement.startswith('SELECT j.status'):
                assert params==(job,tenant,tenant);polls.append(True)
                row=('running','deleting',None,None) if mode=='timeout' or len(polls)==1 else ('completed','deleted',None,None)
                if mode=='missing_job':row=None
                if mode=='failed_job':row=('failed','deleting',None,None)
                if mode=='bad_tombstone':row=('completed','active',None,None)
                if mode=='live_claim':row=('completed','deleted',uuid4(),None)
            return SimpleNamespace(fetchone=lambda:row)
    def run():return module.wait_for_fixture_purge(Connection(),tenant,job,expected_database='isolated',maximum_seconds=1,
        assert_fixture_scope=lambda scope,deletion:None,check_active=lambda:None)
    if mode=='valid':assert run().polls==2
    else:
        with pytest.raises(ValueError):run()
    assert not any(q.startswith(('DELETE','UPDATE','INSERT')) for q in queries)


@pytest.mark.parametrize('mode',['valid','foreign_database','unprivileged','stale_revision','deleting','missing'])
def test_fixture_start_requires_active_exact_revision_before_request(mode):
    tenant=uuid4();target=module.RestoreTenantDeletionTarget('https://restore.test',tenant,7,'Bearer private');queries=[]
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,statement,params=None):
            queries.append(statement);row=None
            if statement.startswith('SELECT current_database'):row=('foreign' if mode=='foreign_database' else 'isolated',mode!='unprivileged')
            if statement.startswith('SELECT status,revision'):
                assert params==(tenant,)
                row=None if mode=='missing' else ('deleting' if mode=='deleting' else 'active',8 if mode=='stale_revision' else 7)
            return SimpleNamespace(fetchone=lambda:row)
    def run():return module.verify_fixture_purge_start(Connection(),target,expected_database='isolated',assert_fixture_scope=lambda _:None,check_active=lambda:None)
    if mode=='valid':assert run() is None
    else:
        with pytest.raises(ValueError):run()
    assert not any(q.startswith(('UPDATE','DELETE','INSERT')) for q in queries)


@pytest.mark.parametrize('denial',['activity','fixture'])
def test_fixture_purge_sql_start_and_wait_refuse_false_before_sql(denial):
    tenant,job=uuid4(),uuid4()
    target=module.RestoreTenantDeletionTarget('https://restore.test',tenant,7,'Bearer private')
    class NoSQL:
        def __getattribute__(self,name):raise AssertionError('SQL reached')
    kwargs=dict(assert_fixture_scope=lambda *_:False if denial=='fixture' else None,
                check_active=lambda:False if denial=='activity' else None)
    with pytest.raises(RuntimeError):
        module.verify_fixture_purge_start(NoSQL(),target,expected_database='isolated',**kwargs)
    with pytest.raises(RuntimeError):
        module.wait_for_fixture_purge(NoSQL(),tenant,job,expected_database='isolated',maximum_seconds=1,**kwargs)
