from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import psycopg
import pytest
from photo_worker.runtime.restore_resources import recover_restored_resources


@pytest.mark.parametrize('mode',['valid','active','missing_task','fence_changed','release_failed','accounting','worker_receipt','capacity_receipt'])
def test_resource_recovery_uses_canonical_release_and_refuses_accounting_repair(mode):
    tenant,task=uuid4(),uuid4();released=False;events=[]
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,params=None):
            nonlocal released
            events.append(sql);row=None;count=0
            if sql.startswith('SELECT current_database'):row=('isolated',True)
            elif 'FROM photo.tasks WHERE state' in sql:row=(mode=='active',)
            elif sql.startswith('SELECT tenant_id'):row=None if released else (tenant,task,7)
            elif 'FROM photo.lock_task' in sql:row=(None if mode=='missing_task' else task,)
            elif sql.startswith('SELECT fencing_token'):row=(8 if mode=='fence_changed' else 7,)
            elif sql.startswith('SELECT photo.release_task_resources'):
                assert params==(tenant,task,7);released=True
            elif 'FROM photo.resource_reservations' in sql:row=(mode=='release_failed',)
            elif sql.startswith('UPDATE photo.resource_slots'):count=2
            elif sql.startswith('UPDATE photo.worker_registrations'):count=True if mode=='worker_receipt' else 3
            elif sql.startswith('UPDATE photo.resource_nodes'):count=-1 if mode=='capacity_receipt' else 1
            elif 'FROM photo.tenant_usage' in sql:row=(mode=='accounting',)
            return SimpleNamespace(fetchone=lambda:row,rowcount=count)
    def run():return recover_restored_resources(Connection(),expected_database='isolated',
        assert_isolated_and_paused=lambda:None,check_active=lambda:None)
    if mode=='valid':
        result=run();assert result.released_reservations==1 and result.cleared_slots==2
        assert any("UPDATE photo.worker_registrations SET last_seen='-infinity'" in event and 'reported_capacity_policy_sha=NULL' in event for event in events)
        assert any('UPDATE photo.resource_nodes SET capacity_report_worker_id=NULL,capacity_report_policy_sha=NULL,reported_at=NULL,ram_available_bytes=0,vram_available_bytes=0,scratch_available_bytes=0' == event for event in events)
        assert not any('SET capacity_policy_sha=' in event or 'SET enabled=' in event or 'UPDATE photo.capacity_policy_timings' in event for event in events)
        assert events.index('SELECT id FROM photo.lock_task(%s)')<events.index('SELECT photo.release_task_resources(%s,%s,%s)')
    else:
        with pytest.raises(ValueError):run()
        if mode in ('active','missing_task','fence_changed'):assert not released
    assert not any(sql.startswith('UPDATE photo.tenant_usage') for sql in events)
