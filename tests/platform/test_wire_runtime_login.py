"""Worker wire DSN separation without a database or printed credentials."""
from contextlib import contextmanager
import importlib.util
from pathlib import Path
import pytest
from psycopg.conninfo import conninfo_to_dict

spec=importlib.util.spec_from_file_location('wire_runtime_login',Path(__file__).resolve().parents[1]/'integration/wire_runtime_login.py')
module=importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


@pytest.mark.parametrize('fail',[False,True])
def test_scoped_membership_login_and_failure_cleanup(monkeypatch,fail):
    calls=[]
    admin_dsn='host=127.0.0.1 dbname=private user=admin password=admin-secret options=-crole=privileged'
    class Admin:
        def execute(self,query):calls.append(query)
    @contextmanager
    def connect(dsn,**kwargs):
        assert dsn==admin_dsn and kwargs=={'autocommit':True}
        yield Admin()
    monkeypatch.setattr(module.psycopg,'connect',connect)
    def invoke():
        with module.worker_login(admin_dsn) as dsn:
            parsed=conninfo_to_dict(dsn)
            assert parsed['user'].startswith('wire_worker_')
            assert parsed['password']!='admin-secret'
            assert parsed['dbname']=='private' and parsed['host']=='127.0.0.1'
            assert 'options' not in parsed and len(calls)==2
            if fail:raise RuntimeError('probe failed')
    if fail:
        with pytest.raises(RuntimeError,match='probe failed'):invoke()
    else:invoke()
    assert len(calls)==3
    create,grant,drop=[query.as_string() for query in calls]
    assert 'NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOREPLICATION' in create
    assert grant.startswith('GRANT photo_worker TO ') and drop.startswith('DROP ROLE ')
    assert grant.split(' TO ')[1]==drop.split('DROP ROLE ')[1]


@pytest.mark.parametrize('fail',[False,True])
def test_exact_registration_is_rebound_and_restored_before_drop(monkeypatch,fail):
    from uuid import uuid4
    tenant,worker,task=uuid4(),uuid4(),uuid4()
    calls=[]
    class Row:
        def fetchone(self):return tenant,worker,'original_admin'
    class Admin:
        def execute(self,query,args=None):
            calls.append((query,args))
            if isinstance(query,str) and query.startswith('SELECT'):return Row()
    @contextmanager
    def connect(*args,**kwargs):yield Admin()
    monkeypatch.setattr(module.psycopg,'connect',connect)
    def invoke():
        with module.worker_login('host=127.0.0.1 dbname=private user=admin',task) as dsn:
            name=conninfo_to_dict(dsn)['user']
            assert calls[2][1]==(task,)
            assert calls[3][1]==(name,tenant,worker)
            if fail:raise RuntimeError('probe failed')
    if fail:
        with pytest.raises(RuntimeError,match='probe failed'):invoke()
    else:invoke()
    assert len(calls)==6
    assert calls[4][1]==('original_admin',tenant,worker,calls[3][1][0])
    assert calls[5][0].as_string().startswith('DROP ROLE ')
