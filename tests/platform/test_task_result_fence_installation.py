"""Startup ABI probe rejects missing, malformed and ambiguous result bases."""
import pytest
from photo_worker.runtime.database import TaskDatabase

@pytest.mark.parametrize('row,ready',[(None,False),((False,),False),((None,),False),
    ((1,),False),((True,False),False),((True,),True)])
def test_task_result_fence_probe_requires_exact_boolean(monkeypatch,row,ready):
    database=TaskDatabase('unused')
    calls=[]
    def query(sql,args):
        calls.append((sql,args))
        return row
    monkeypatch.setattr(database,'_query',query)
    assert database.task_result_fence_ready() is ready
    assert len(calls)==1
    assert '-- 02535:' in calls[0][0]
    assert '-- 02537:' in calls[0][0]
    assert 'photo.execution_source_live(uuid)' in calls[0][0]
    assert 'has_function_privilege' in calls[0][0]
    assert 'UPDATE photo.tasks SET lease_until' in calls[0][0]

    assert '-- 02543: final failure authority' in calls[0][0]
    assert 'photo.commit_task_failure(uuid,integer,bigint,uuid,jsonb,jsonb)' in calls[0][0]
    assert 'OR slot.lease_until IS NULL' in calls[0][0]
