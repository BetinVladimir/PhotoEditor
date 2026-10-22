"""Capacity and model residency share commit/rollback on the worker connection."""
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.runtime.database import TaskDatabase


@pytest.mark.parametrize('responses,accepted,rollbacks', [
    ([(True,),(True,)],True,0), ([(True,),(False,)],False,1),
    ([(False,)],False,1), ([(True,),None],False,1)])
def test_atomic_heartbeat_transaction(monkeypatch,responses,accepted,rollbacks):
    calls=[]
    class Connection:
        def __enter__(self):return self
        def __exit__(self,*args):calls.append(('exit',args[0]))
        def rollback(self):calls.append(('rollback',))
        def execute(self,query,args):
            calls.append(('query',query,args))
            return SimpleNamespace(fetchone=lambda:responses.pop(0))
    database=TaskDatabase('fixture')
    monkeypatch.setattr(database,'_connect',lambda:Connection())
    assert database.heartbeat_policy_worker_with_residency(uuid4(),'gpu','cuda',{'features.scene'},1,2,3,'a'*64,()) is accepted
    assert sum(call[0]=='rollback' for call in calls)==rollbacks
    assert not responses
    assert calls[-1]==('exit',None)


def test_bad_residency_refused_before_connection(monkeypatch):
    database=TaskDatabase('fixture')
    monkeypatch.setattr(database,'_connect',lambda:pytest.fail('invalid residency reached DB'))
    with pytest.raises(ValueError,match='MODEL_RESIDENCY_INVALID'):
        database.heartbeat_policy_worker_with_residency(uuid4(),'gpu','cuda',set(),1,2,3,'a'*64,[])


@pytest.mark.parametrize('error_name',['SerializationFailure','DeadlockDetected'])
@pytest.mark.parametrize('phase',['capacity','residency','commit'])
def test_retry_restarts_whole_transaction(monkeypatch,error_name,phase):
    import psycopg
    from photo_worker.runtime import database as module
    attempts=[]; pauses=[]
    error_type=getattr(psycopg.errors,error_name)
    class Connection:
        def __init__(self):self.number=len(attempts);self.queries=[];attempts.append(self)
        def __enter__(self):return self
        def __exit__(self,*args):
            if self.number==0 and phase=='commit' and args[0] is None:
                raise error_type('retry commit')
        def execute(self,query,args):
            self.queries.append((query,args))
            failure_index=1 if phase=='capacity' else 2
            if self.number==0 and phase!='commit' and len(self.queries)==failure_index:
                raise error_type('retry statement')
            return SimpleNamespace(fetchone=lambda:(True,))
    database=TaskDatabase('fixture')
    monkeypatch.setattr(database,'_connect',Connection)
    monkeypatch.setattr(module,'sleep',pauses.append)
    assert database.heartbeat_policy_worker_with_residency(uuid4(),'gpu','cuda',{'features.scene'},1,2,3,'a'*64,())
    assert len(attempts)==2 and len(attempts[1].queries)==2
    assert 'heartbeat_worker_capacity_v2' in attempts[1].queries[0][0]
    assert 'report_worker_model_residency' in attempts[1].queries[1][0]
    assert pauses==[.01]


def test_unknown_commit_does_not_retry_or_acknowledge_success(monkeypatch):
    import psycopg
    calls=[]
    class Connection:
        def __enter__(self):calls.append('connect');return self
        def __exit__(self,*args):raise psycopg.OperationalError('commit acknowledgement lost')
        def execute(self,*args):return SimpleNamespace(fetchone=lambda:(True,))
    database=TaskDatabase('fixture')
    monkeypatch.setattr(database,'_connect',Connection)
    with pytest.raises(psycopg.OperationalError):
        database.heartbeat_policy_worker_with_residency(uuid4(),'gpu','cuda',set(),1,2,3,'a'*64,())
    assert calls==['connect']



def test_retry_exhaustion_propagates_after_three_transactions(monkeypatch):
    import psycopg
    from photo_worker.runtime import database as module
    calls=[];pauses=[]
    def fail():
        calls.append(True)
        raise psycopg.errors.SerializationFailure('retry exhausted')
    database=TaskDatabase('fixture')
    monkeypatch.setattr(database,'_connect',fail)
    monkeypatch.setattr(module,'sleep',pauses.append)
    with pytest.raises(psycopg.errors.SerializationFailure):
        database.heartbeat_policy_worker_with_residency(uuid4(),'gpu','cuda',set(),1,2,3,'a'*64,())
    assert len(calls)==3 and pauses==[.01,.02]
