from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
import psycopg
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.restore_inventory import RestoreObject
from photo_worker.runtime import restore_render_array_catalogue as m


@pytest.mark.parametrize('mode',['valid','foreign_database','missing_producer','foreign_key','late_version','fixture_false','activity_false'])
def test_array_physical_verification_requires_stable_canonical_sql_producer(monkeypatch,mode):
    tenant=uuid4();ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64);reads=[];physical=[];queries=[]
    root=RestoreObject('photos','tenants/'+str(tenant)+'/root','v1','b'*64,128)
    result=m.RestoreBatchResultEvidence('restore-batch-result/v1','isolated',uuid4(),uuid4(),uuid4(),1,7,root)
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,q,params=None):
            queries.append(q)
            row=None
            if q.startswith('SELECT current_database'):row=('other' if mode=='foreign_database' else 'isolated',True)
            if q.startswith('SELECT a.bucket'):
                assert params==(tenant,ref.artifact_id,ref.sha256,'mask_pixels',result.task_id,result.result_id,1,7)
                reads.append(True)
                row=None if mode=='missing_producer' else ('photos','tenants/'+str(uuid4() if mode=='foreign_key' else tenant)+'/array',
                    'v2' if mode=='late_version' and len(reads)>1 else 'v1',ref.sha256,144)
            return NS(fetchone=lambda:row)
    monkeypatch.setattr(m,'verify_restore_render_array',lambda *a,**k:physical.append(True) or 'verified')
    def run():return m.verify_fixture_produced_array(Connection(),object(),('photos',),tenant,result,ref,(2,2),'mask_pixels',
        assert_fixture_scope=lambda *args:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert run()=='verified' and len(reads)==2
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('foreign_database','missing_producer','foreign_key'):assert not physical
    if mode in ('fixture_false','activity_false'):assert queries==[] and physical==[]
