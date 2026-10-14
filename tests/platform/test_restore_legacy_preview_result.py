from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
import psycopg
import pytest
from photo_worker.runtime import restore_legacy_preview_result as m


@pytest.mark.parametrize('mode',['valid','authority','missing','duplicate_role','duplicate_id','scope','registration','fixture_false','activity_false'])
def test_preview_completed_catalogue_requires_two_distinct_scoped_outputs(monkeypatch,mode):
    tenant,run,task,derivative,manifest=uuid4(),uuid4(),uuid4(),uuid4(),uuid4()
    target=NS(tenant_id=tenant);ticket=NS(run_id=run);registration=NS(task_id=task)
    def registered(*a,**k):
        if mode=='registration':raise ValueError('bad registration')
        return registration
    monkeypatch.setattr(m,'observe_fixture_legacy_preview_registration',registered)
    prefix='tenants/'+str(uuid4() if mode=='scope' else tenant)+'/'
    rows=[(derivative,'derivative','artifacts',prefix+'preview','v1','a'*64,1024),
          (derivative if mode=='duplicate_id' else manifest,
           'derivative' if mode=='duplicate_role' else 'derivative_manifest',
           'artifacts',prefix+'manifest','v2','b'*64,512)]
    if mode=='missing':rows=rows[:1]
    queries=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,args=None):
            queries.append(sql)
            if sql.startswith('SELECT current_database'):return NS(fetchone=lambda:('restore',mode!='authority'))
            if sql.startswith('SELECT a.id'):
                assert args==(tenant,run,task)
                assert "r.status='completed'" in sql and 'a.produced_fence=z.fencing_token' in sql
                return NS(fetchall=lambda:rows)
            return NS()
    def invoke():return m.observe_fixture_legacy_preview_result(Connection(),target,NS(),ticket,
        expected_database='restore',
        assert_fixture_scope=lambda *a:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':
        result=invoke();assert result.derivative.artifact_id==derivative and result.manifest.artifact_id==manifest
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    if mode in ('fixture_false','activity_false'):assert queries==[]
