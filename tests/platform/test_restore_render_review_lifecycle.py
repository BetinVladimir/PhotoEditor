from types import SimpleNamespace as NS
from uuid import uuid4,uuid5
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.restore_batch_adoption import RestoreBatchAdoptionEvidence
from photo_worker.runtime.restore_batch_physical import RestoreBatchPhysicalEvidence
from photo_worker.runtime import restore_render_review_lifecycle as m


@pytest.mark.parametrize('mode',['valid','pending_failure','post_unknown','foreign_observation',
                                 'late_observation','scope_failure','fixture_false','activity_false'])
def test_render_review_runs_one_post_only_after_pending_preflight(monkeypatch,mode):
    tenant,actor,batch,child,workflow,review=(uuid4() for _ in range(6))
    adoption=RestoreBatchAdoptionEvidence('restore-batch-adoption/v1','isolated',batch,child,workflow)
    task=uuid5(workflow,'render.compose:0')
    candidate=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    physical=RestoreBatchPhysicalEvidence('restore-batch-physical/v1',NS(),
        NS(working_image=candidate),NS(),(),None,None,(),None)
    pending=NS(database='isolated',tenant_id=tenant,child_run_id=child,
               review_id=review,task_id=task,revision=1,candidate=candidate)
    decision=NS(review_id=review,revision=2)
    observed=NS(review_id=uuid4() if mode=='foreign_observation' else review,
                task_id=task,candidate=candidate,revision=2)
    events=[];clock=[0.]
    monkeypatch.setattr(m,'monotonic',lambda:clock[0])
    def find(*args,**kwargs):
        events.append('pending')
        if mode=='pending_failure':raise ValueError('no pending review')
        return pending
    def post(target,**kwargs):
        events.append('post')
        assert target.review_id==review and target.expected_revision==1
        if mode=='post_unknown':raise ValueError('RESTORE_RENDER_REVIEW_DECISION_OUTCOME_UNKNOWN')
        return decision
    def observe(*args,**kwargs):
        events.append('observe')
        if mode=='late_observation':clock[0]=1.
        return observed
    monkeypatch.setattr(m,'find_fixture_pending_render_review',find)
    monkeypatch.setattr(m,'validate_fixture_render_review_decision',lambda *a,**k:events.append('validate'))
    monkeypatch.setattr(m,'request_fixture_render_review_decision',post)
    monkeypatch.setattr(m,'observe_fixture_render_review',observe)
    def scope():
        if mode=='scope_failure':raise RuntimeError('fixture fence lost')
        if mode=='fixture_false':return False
    def run():return m.run_fixture_render_review(object(),tenant,actor,adoption,physical,
        origin='https://restore.test',authorization='Bearer private',tls_context=object(),
        maximum_seconds=1,assert_installed_fixture=scope,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':
        result=run();assert result.pending==pending and result.decision==decision
        assert result.observed==observed
        assert events==['pending','validate','post','observe']
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        assert events.count('post')<=1
        if mode in ('pending_failure','scope_failure','fixture_false','activity_false'):assert 'post' not in events
        if mode in ('fixture_false','activity_false'):assert events==[]
        if mode=='post_unknown':assert 'observe' not in events
