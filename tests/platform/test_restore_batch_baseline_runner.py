from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
from hashlib import sha256
import psycopg
import pytest
from photo_worker.runtime import restore_batch_baseline as m


@pytest.mark.parametrize('mode',['valid','failed','cancelled','missing','timeout','late_success','physical_failure','late_physical','fixture_false','activity_false'])
def test_completed_batch_baseline_requires_success_and_physical_output(monkeypatch,mode):
    events=[];clock=[0.];polls=[];raw=b'{}'
    binding=NS(source_sha256=sha256(raw).hexdigest(),source_artifact_id=uuid4(),asset_version_id=uuid4(),canvas_hash='a'*64)
    target=NS(tenant_id=uuid4());adoption=NS(workflow_id=uuid4(),child_run_id=uuid4())
    monkeypatch.setattr(m,'monotonic',lambda:clock[0])
    monkeypatch.setattr(m,'sleep',lambda n:clock.__setitem__(0,clock[0]+n))
    monkeypatch.setattr(m,'verify_fixture_batch_not_adopted',lambda *a,**k:events.append('preflight'))
    monkeypatch.setattr(m,'observe_fixture_render_batch_baseline',lambda *a,**k:events.append('post') or 'ticket')
    monkeypatch.setattr(m,'verify_fixture_batch_adoption',lambda *a,**k:events.append('graph') or adoption)
    def physical(*a,**k):
        events.append('physical')
        if mode=='physical_failure':raise ValueError('bad bytes')
        if mode=='late_physical':clock[0]=1.
        return 'verified'
    monkeypatch.setattr(m,'verify_fixture_batch_physical',physical)
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,q,params=None):
            row=None
            if q.startswith('SELECT current_database'):row=('isolated',True)
            if q.startswith('SELECT state'):
                polls.append(True)
                if mode=='late_success':clock[0]=1.
                row=None if mode=='missing' else ('failed' if mode=='failed' else 'running' if mode=='timeout' or len(polls)==1 else 'succeeded',mode=='cancelled')
                if mode=='late_success':row=('succeeded',False)
            return NS(fetchone=lambda:row)
    def run():return m.run_fixture_batch_baseline(Connection(),object(),('photos',),target,binding,object(),raw,
        expected_database='isolated',session_id=uuid4(),tls_context=object(),maximum_seconds=1,
        assert_installed_fixture=lambda:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert run().physical=='verified' and len(polls)==2
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('fixture_false','activity_false'):
        assert events==[] and polls==[]
        return
    assert events[:3]==['preflight','post','graph'] and events.count('post')==1
    if mode in ('failed','cancelled','missing','timeout','late_success'):assert 'physical' not in events
