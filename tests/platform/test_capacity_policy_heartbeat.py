from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.runtime.database import TaskDatabase
from photo_worker.runtime.capacity import Capacity
from photo_worker.runtime.capacity_policy import load_capacity_policy
from photo_worker.runtime.service import _report_capacity


def test_policy_heartbeat_owns_wire_order_and_requires_exact_success(monkeypatch):
    database=TaskDatabase('fixture');calls=[]
    monkeypatch.setattr(database,'_query',lambda query,params:calls.append((query,params)) or (True,))
    worker=uuid4()
    assert database.heartbeat_policy_worker(worker,'gpu','mps',frozenset({'z','a'}),100,200,300,'b'*64)
    assert calls[0][1]==(worker,'gpu','mps',['a','z'],100,200,300,'b'*64)
    assert 'heartbeat_worker_capacity_v2' in calls[0][0]
    monkeypatch.setattr(database,'_query',lambda *args:(1,))
    assert not database.heartbeat_policy_worker(worker,'gpu','mps',frozenset({'a'}),100,200,300,'b'*64)


@pytest.mark.parametrize('pin',[None,'','A'*64,'b'*63])
def test_invalid_policy_sha_refuses_before_sql(monkeypatch,pin):
    database=TaskDatabase('fixture')
    monkeypatch.setattr(database,'_query',lambda *args:pytest.fail('SQL reached'))
    with pytest.raises(ValueError,match='CAPACITY_POLICY_SHA_INVALID'):
        database.heartbeat_policy_worker(uuid4(),'gpu','mps',frozenset({'a'}),1,1,1,pin)


@pytest.mark.parametrize('resource,backend',[('gpu','mps'),('gpu','cuda'),('gpu','cpu'),('cpu','cpu'),('media','cpu'),('archive','cpu')])
def test_runtime_reports_canonical_policy_sha(resource,backend):
    policy=load_capacity_policy(Path(__file__).resolve().parents[2]/'configs/capacity-metal-verification.json')
    calls=[]
    database=SimpleNamespace(heartbeat_policy_worker=lambda *args:calls.append(args) or True)
    config=SimpleNamespace(worker_id=uuid4(),resource_class=resource,backend=backend,capacity_policy=policy)
    assert _report_capacity(config,database,frozenset({'a'}),Capacity(100,300,200))
    assert calls[0][-1]==policy.sha256
    assert calls[0][5]==(200 if resource=='gpu' else 0)


@pytest.mark.parametrize('accepted',[True,False])
def test_policy_heartbeat_publishes_residency_atomically(accepted):
    from photo_worker.models.pool import ResidentModel
    policy=load_capacity_policy(Path(__file__).resolve().parents[2]/'configs/capacity-metal-verification.json')
    config=SimpleNamespace(worker_id=uuid4(),resource_class='gpu',backend='mps',capacity_policy=policy)
    resident=ResidentModel('a'*64,'sfd','mps','b'*64,3)
    calls=[]
    database=SimpleNamespace(heartbeat_policy_worker_with_residency=lambda *args:calls.append(args) or accepted)
    assert _report_capacity(config,database,frozenset({'a'}),Capacity(100,300,200),lambda:(resident,)) is accepted
    assert calls[0][-2:] == (policy.sha256,(resident,))



def test_model_residency_database_detaches_wire(monkeypatch):
    from photo_worker.models.pool import ResidentModel
    database=TaskDatabase('fixture');calls=[]
    monkeypatch.setattr(database,'_query',lambda query,args:calls.append((query,args)) or (True,))
    worker=uuid4();resident=ResidentModel('a'*64,'u2net','mps','b'*64,2)
    assert database.report_model_residency(worker,'c'*64,(resident,))
    assert calls[0][1][:2]==(worker,'c'*64)
    assert calls[0][1][2].obj==[{'model_lock_sha':'a'*64,'model_id':'u2net','backend':'mps','qualification_report_sha':'b'*64,'generation':2}]
    monkeypatch.setattr(database,'_query',lambda *args:pytest.fail('invalid residency reached SQL'))
    with pytest.raises(ValueError):database.report_model_residency(worker,'c'*64,[resident])



def test_residency_snapshot_failure_does_not_publish_capacity():
    policy=load_capacity_policy(Path(__file__).resolve().parents[2]/'configs/capacity-metal-verification.json')
    config=SimpleNamespace(worker_id=uuid4(),resource_class='gpu',backend='mps',capacity_policy=policy)
    def unexpected(*args):pytest.fail('snapshot failure must precede SQL publication')
    database=SimpleNamespace(heartbeat_policy_worker_with_residency=unexpected)
    def failed_snapshot():raise RuntimeError('model snapshot unavailable')
    with pytest.raises(RuntimeError,match='snapshot unavailable'):
        _report_capacity(config,database,frozenset({'a'}),Capacity(100,300,200),failed_snapshot)
