from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
import psycopg
import pytest
from photo_worker.runtime.restore_inventory import RestoreObject
from photo_worker.runtime import restore_export_volume as m


@pytest.mark.parametrize('mode',['zip','single','authority','missing','scope','manifest_alias','inactive'])
def test_ready_volume_single_identity_or_canonical_archive_manifest(mode):
    tenant,artifact,manifest=uuid4(),uuid4(),uuid4();prefix=f'tenants/{tenant}/'
    obj=RestoreObject('photos',prefix+'encoded','v1','a'*64,100)
    encoded=NS(registration=NS(download_job_id=uuid4(),run_id=uuid4()),volume_no=1,artifact_id=artifact,
        object=obj,delivery_kind='single' if mode=='single' else 'zip')
    row=(artifact,*[obj.bucket,obj.key,obj.version,obj.sha256,obj.size],None,None,None,None,None,None) if mode=='single' else (
        artifact,'photos','other/archive' if mode=='scope' else prefix+'archive','v2','b'*64,200,
        artifact if mode=='manifest_alias' else manifest,'photos',prefix+'manifest','v3','c'*64,512)
    calls=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):calls.append('sql');yield
        def execute(self,sql,args=None):
            if sql.startswith('SELECT current_database'):value=('restore',mode!='authority')
            elif sql.startswith('SELECT a.id'):
                assert "t.operation='archive.build'" in sql and 'm.produced_fence=z.fencing_token' in sql
                value=None if mode=='missing' else row
            else:value=None
            return NS(fetchone=lambda:value)
    def invoke():return m.read_fixture_export_volume(Connection(),tenant,encoded,expected_database='restore',
        check_active=lambda:False if mode=='inactive' else None)
    if mode in ('zip','single'):assert invoke().delivery_kind==encoded.delivery_kind
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    if mode=='inactive':assert not calls
