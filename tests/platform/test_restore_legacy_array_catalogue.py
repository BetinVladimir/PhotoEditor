from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
import psycopg
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime import restore_legacy_array_catalogue as m


@pytest.mark.parametrize('mode',['valid','authority','ownership','foreign_key','changed','physical_failure','activity_false','post_read_false'])
def test_legacy_array_repeats_producer_or_declared_alpha_authority(monkeypatch,mode):
    tenant=uuid4();ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    result=NS(registration=NS(database='restore',run_id=uuid4()),artifact_id=uuid4(),object=NS(sha256='b'*64))
    reads=[];physical=[];checks=[];queries=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,args=None):
            queries.append(sql)
            if sql.startswith('SELECT current_database'):row=('restore',mode!='authority')
            elif sql.startswith('SELECT a.bucket'):
                assert "a.output_role='alpha'" in sql and args[-3:]==('working_alpha','working_alpha','working_alpha')
                reads.append(True)
                row=None if mode=='ownership' else ('photos',('other/' if mode=='foreign_key' else f'tenants/{tenant}/')+'array',
                    'v2' if mode=='changed' and len(reads)>1 else 'v1',ref.sha256,64)
            else:row=None
            return NS(fetchone=lambda:row)
    def verify(*a,**k):
        physical.append(True)
        if mode=='physical_failure':raise ValueError('bad bytes')
    monkeypatch.setattr(m,'verify_legacy_array_bytes',verify)
    def active():
        checks.append(True)
        return False if mode=='activity_false' or mode=='post_read_false' and len(checks)>=2 else None
    def run():return m.verify_fixture_legacy_array(Connection(),object(),('photos',),tenant,result,ref,(2,2),'uint16',
        'working_alpha',check_active=active)
    if mode=='valid':assert run().version=='v1' and len(reads)==2 and physical==[True]
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode=='activity_false':assert queries==[] and physical==[]
    if mode=='post_read_false':assert len(reads)==1 and physical==[]
