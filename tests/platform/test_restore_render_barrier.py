from types import SimpleNamespace as NS
from uuid import uuid4
import pytest
from photo_worker.runtime import restore_render_barrier as m


@pytest.mark.parametrize('mode',['release','cancelled','boolean_cancelled','timeout','foreign_task','reuse'])
def test_explicit_barrier_preserves_profiled_dispatch_and_cancel_before_compute(monkeypatch,mode):
    renderer=object.__new__(m.ProfiledTaskRenderer)
    tenant,task=uuid4(),uuid4();calls=[]
    monkeypatch.setattr(m.ProfiledTaskRenderer,'execute',lambda self,*a,**k:calls.append(k['authoritative_inputs']) or 'result')
    barrier=m.RestoreBarrierRenderer(renderer,tenant,task,maximum_seconds=1)
    command=NS(tenant_id=tenant,task_id=uuid4() if mode=='foreign_task' else task,attempt=2)
    def active():
        if mode=='cancelled':
            assert barrier.held_generation()==(2,7)
            barrier.assert_held(NS(tenant_id=tenant,task_id=task,attempt=2,fence=7))
            raise m.TaskCancelled('cancelled by heartbeat')
        if mode=='boolean_cancelled':return False
    ctx=NS(lease=NS(command=command,fence=7),check_active=active)
    if mode in ('release','reuse'):barrier.release()
    if mode=='timeout':
        clock=iter([0.,1.]);monkeypatch.setattr(m,'monotonic',lambda:next(clock))
    def run():return barrier.execute(ctx,object(),object(),authoritative_inputs='scoped')
    if mode in ('release','reuse'):
        assert run()=='result' and calls==['scoped']
        if mode=='reuse':
            with pytest.raises(m.OperationFailure):run()
            assert calls==['scoped']
    else:
        with pytest.raises((m.OperationFailure,m.TaskCancelled)):run()
        assert calls==[]
    witness=NS(tenant_id=tenant,task_id=task,attempt=2,fence=7)
    if mode in ('cancelled','boolean_cancelled'):
        barrier.assert_cancelled(witness)
        with pytest.raises(ValueError):barrier.assert_cancelled(NS(tenant_id=tenant,task_id=task,attempt=3,fence=7))
        barrier.release()
    with pytest.raises(ValueError):barrier.assert_cancelled(witness)
    with pytest.raises(ValueError,match='NOT_HELD'):barrier.held_generation()
