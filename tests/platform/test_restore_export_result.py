from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
import psycopg
import pytest
from photo_worker.runtime import restore_export_result as m


@pytest.mark.parametrize('mode',['valid','authority','missing','scope','filename','control_filename','registration','inactive'])
def test_encoded_export_catalogue_requires_ready_job_canonical_producer(monkeypatch,mode):
    tenant,job,run,version,artifact=uuid4(),uuid4(),uuid4(),uuid4(),uuid4()
    registration=NS(run_id=run)
    def registered(*a,**k):
        if mode=='registration':raise ValueError('invalid registration')
        return registration
    monkeypatch.setattr(m,'observe_fixture_export_registration',registered)
    calls=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):calls.append('sql');yield
        def execute(self,sql,args=None):
            if sql.startswith('SELECT current_database'):row=('restore',mode!='authority')
            elif sql.startswith('SELECT a.id'):
                assert 't.run_id=r.id' in sql and "(p.params-'image'-'icc_sha256')=j.preset->'config'" in sql
                assert "a.output_role='encoded_image'" in sql
                assert args==(tenant,job,run,version)
                assert "t.operation='export.encode'" in sql and 'a.produced_fence=z.fencing_token' in sql
                row=None if mode=='missing' else (artifact,'photos','other/object' if mode=='scope' else f'tenants/{tenant}/encoded',
                    'v1','a'*64,1024,'image/jpeg','../escape.jpg' if mode=='filename' else
                    'bad\nname.jpg' if mode=='control_filename' else 'output.jpg',1,'zip')
            else:row=None
            return NS(fetchone=lambda:row)
    def invoke():return m.observe_fixture_encoded_export(Connection(),NS(tenant_id=tenant),NS(),NS(download_job_id=job),NS(),version,
        expected_database='restore',check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':assert invoke().artifact_id==artifact
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    if mode=='inactive':assert not calls
