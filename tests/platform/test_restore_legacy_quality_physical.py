from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
import psycopg
import pytest
from photo_worker.runtime import restore_legacy_quality_physical as m


@pytest.mark.parametrize('mode',['valid','authority','missing','duplicate','foreign_key','activity_false','post_read_false'])
def test_quality_catalogue_requires_unique_exact_target_canonical_producer(mode):
    tenant=uuid4();result=NS(registration=NS(database='restore',run_id=uuid4()),artifact_id=uuid4(),object=NS(sha256='b'*64))
    value=('photos',('other/' if mode=='foreign_key' else f'tenants/{tenant}/')+'quality','v1','a'*64,64)
    queries=[];checks=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,args=None):
            queries.append(sql)
            if sql.startswith('SELECT current_database'):return NS(fetchone=lambda:('restore',mode!='authority'))
            if sql.startswith('SELECT a.bucket'):
                assert args==(tenant,result.registration.run_id,result.artifact_id,result.object.sha256)
                return NS(fetchall=lambda:[] if mode=='missing' else [value,value] if mode=='duplicate' else [value])
            return NS()
    def active():
        checks.append(True)
        return False if mode=='activity_false' or mode=='post_read_false' and len(checks)>=2 else None
    def run():return m.read_legacy_quality_object(Connection(),tenant,result,check_active=active)
    if mode=='valid':assert run().version=='v1'
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode=='activity_false':assert queries==[]
    if mode=='post_read_false':assert any(q.startswith('SELECT a.bucket') for q in queries)
