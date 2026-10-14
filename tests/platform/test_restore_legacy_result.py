from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
import psycopg
import pytest
from photo_worker.runtime import restore_legacy_result as m


@pytest.mark.parametrize('mode',['valid','authority','not_ready','foreign_key','zero_version','registration_failure','scope_false','activity_false'])
def test_adopted_legacy_result_requires_completed_lineage_and_registered_producer(monkeypatch,mode):
    target=NS(tenant_id=uuid4(),item_id=uuid4());binding=NS(asset_version_id=uuid4())
    ticket=NS(run_id=uuid4(),output_collection_id=uuid4())
    def registered(*a,**k):
        if mode=='registration_failure':raise ValueError('receipt invalid')
        return 'registration'
    monkeypatch.setattr(m,'observe_fixture_legacy_apply_registration',registered)
    version=uuid4() if mode!='zero_version' else m.UUID(int=0)
    physical=('photos',('other/' if mode=='foreign_key' else f'tenants/{target.tenant_id}/')+'output','v1','a'*64,64)
    queries=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,args=None):
            queries.append(sql)
            if sql.startswith('SELECT current_database'):row=('restore',mode!='authority')
            elif sql.startswith('SELECT i.id'):
                assert args==(target.tenant_id,ticket.run_id,ticket.output_collection_id,target.item_id,binding.asset_version_id)
                row=None if mode=='not_ready' else (uuid4(),version,uuid4(),*physical)
            else:row=None
            return NS(fetchone=lambda:row)
    def run():return m.observe_fixture_legacy_apply_result(Connection(),target,binding,ticket,
        expected_database='restore',
        assert_fixture_scope=lambda *a:False if mode=='scope_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert run().asset_version_id==version
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('scope_false','activity_false'):assert queries==[]
