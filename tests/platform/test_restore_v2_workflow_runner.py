from datetime import datetime,timezone
from hashlib import sha256
from types import SimpleNamespace as NS
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.runtime.restore_preview_download import RestorePreviewDownloadTarget
from photo_worker.runtime.restore_revoke_batch import RestoreRenderBatchTarget
from photo_worker.runtime.restore_revoke_freeze import RestoreFreezeBaselineBinding,RestoreFreezeBaselineEvidence
from photo_worker.runtime.restore_replay_receipt import VerifiedRestoreReplay
from photo_worker.runtime import restore_v2_workflow_runner as m


def test_inactive_v2_report_does_not_read_report_root(monkeypatch,tmp_path):
    monkeypatch.setattr(m,'validate_probe_report_root',
        lambda root: (_ for _ in ()).throw(AssertionError('report root read')))
    replay=VerifiedRestoreReplay(uuid4(),'isolated','a'*64,'b'*64,'c'*64,1,
        datetime(2020,1,1,tzinfo=timezone.utc))
    with pytest.raises(ValueError,match='RESTORE_V2_WORKFLOW_INACTIVE'):
        m.run_fixture_v2_workflow_report(tmp_path,replay,'0'*64,object(),object(),(),
            object(),object(),object(),b'',object(),session_id=uuid4(),srgb_profile=b'',
            tls_context=object(),maximum_seconds=30,assert_installed_fixture=lambda:None,
            check_active=lambda:False)


@pytest.mark.parametrize('mode',['valid','baseline_failure','preview_failure','review_failure',
                                 'terminal_late','terminal_timeout','download_failure','changed_review',
                                 'false_fixture','false_active'])
def test_v2_workflow_stages_and_global_deadline(monkeypatch,mode):
    tenant,project,snapshot,actor,version,source_id,recipe,batch,child,workflow,review_id=(uuid4() for _ in range(11))
    source=ArtifactRef(artifact_id=source_id,sha256='a'*64)
    candidate=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    target=RestoreRenderBatchTarget('https://api.test',tenant,project,snapshot,'c'*64,
        'unique-key','Bearer private')
    binding=RestoreFreezeBaselineBinding(actor,version,source_id,source.sha256,'d'*64)
    frozen=RestoreFreezeBaselineEvidence('v1',uuid4(),recipe,'e'*64,'f'*64,'0'*64,
        datetime.now(timezone.utc))
    download_target=RestorePreviewDownloadTarget('https://api.test','https://objects.test','Bearer private')
    adoption=NS(database='isolated',batch_id=batch,child_run_id=child,workflow_id=workflow)
    physical=NS(image_result=NS(working_image=candidate))
    baseline=NS(adoption=adoption,physical=physical,polls=2)
    pending=NS(candidate=candidate,review_id=review_id)
    preview=NS(pending=pending,after_catalogue=NS(),before=NS(verified=NS(image=NS(sha256='1'*64))),
        after=NS(verified=NS(image=NS(sha256='2'*64),manifest=NS(sha256='3'*64))))
    decided=NS(candidate=candidate,review_id=review_id)
    review_result=NS(pending=pending,observed=decided)
    terminal=NS(review_id=review_id)
    download=NS(child_run_id=child,derivative_sha256='2'*64,manifest_sha256='3'*64)
    events=[];clock=[0.0];terminal_calls=[0]
    monkeypatch.setattr(m,'monotonic',lambda:clock[0])
    monkeypatch.setattr(m,'sleep',lambda _:clock.__setitem__(0,clock[0]+.3))
    monkeypatch.setattr(m,'validate_preview_download_installation',
        lambda *a,**k:events.append('install'))
    def batch_call(*a,**k):
        events.append('batch')
        if mode=='baseline_failure':raise ValueError('batch failed')
        return baseline
    def preview_call(*a,**k):
        events.append('previews')
        if mode=='preview_failure':raise ValueError('preview failed')
        return preview
    def review_call(*a,**k):
        events.append('review')
        if mode=='review_failure':raise ValueError('review failed')
        return NS(pending=NS(),observed=decided) if mode=='changed_review' else review_result
    def terminal_call(*a,**k):
        terminal_calls[0]+=1;events.append('terminal')
        if mode in ('terminal_late','terminal_timeout'):
            if mode=='terminal_timeout':clock[0]=2.0
            if terminal_calls[0]==1:raise ValueError('RESTORE_RENDER_TERMINAL_NOT_COMPLETE')
        return terminal
    def download_call(*a,**k):
        events.append('download')
        if mode=='download_failure':raise ValueError('download failed')
        return download
    monkeypatch.setattr(m,'run_fixture_batch_baseline',batch_call)
    monkeypatch.setattr(m,'find_fixture_pending_render_review',lambda *a,**k:events.append('pending') or pending)
    monkeypatch.setattr(m,'run_fixture_render_review_previews',preview_call)
    monkeypatch.setattr(m,'run_fixture_render_review',review_call)
    monkeypatch.setattr(m,'observe_fixture_render_terminal',terminal_call)
    monkeypatch.setattr(m,'download_fixture_render_output',download_call)
    def run():return m.run_fixture_v2_workflow(object(),object(),('photos',),target,binding,
        frozen,b'{}',download_target,expected_database='isolated',session_id=uuid4(),
        srgb_profile=b'P'*128,tls_context=object(),maximum_seconds=1,
        assert_installed_fixture=lambda:False if mode=='false_fixture' else None,
        check_active=lambda:False if mode=='false_active' else None)
    if mode in ('valid','terminal_late'):
        result=run();assert result.download is download
        assert result.terminal_polls==(2 if mode=='terminal_late' else 1)
        assert events==(['install','batch','pending','previews','review']+
            ['terminal']*result.terminal_polls+['download'])
    else:
        with pytest.raises(ValueError):run()
        if mode in ('false_fixture','false_active'):assert events==['install']
        if mode in ('baseline_failure','preview_failure','review_failure','changed_review','terminal_timeout'):
            assert 'download' not in events
        if mode in ('baseline_failure','preview_failure'):assert 'review' not in events


@pytest.mark.parametrize('mode',['valid','changed_download','execution_failure','bad_fixture','wrong_pin','false_fixture_report'])
def test_v2_workflow_report_is_published_only_after_bound_download(monkeypatch,tmp_path,mode):
    replay=VerifiedRestoreReplay(uuid4(),'isolated','a'*64,'b'*64,'c'*64,1,
        datetime(2020,1,1,tzinfo=timezone.utc))
    child,review_id,batch=(uuid4() for _ in range(3))
    tenant,project,snapshot,actor,version,source,recipe,session=(uuid4() for _ in range(8))
    target=RestoreRenderBatchTarget('https://api.test',tenant,project,snapshot,'a'*64,
        'unique-key','Bearer private')
    binding=RestoreFreezeBaselineBinding(actor,version,source,'b'*64,'c'*64)
    frozen=RestoreFreezeBaselineEvidence('v1',uuid4(),recipe,'d'*64,'e'*64,'f'*64,
        datetime.now(timezone.utc))
    download_target=RestorePreviewDownloadTarget('https://api.test','https://objects.test','Bearer private')
    srgb_profile=b'P'*128
    descriptor=m.v2_workflow_fixture_descriptor(replay,('photos',),target,binding,frozen,
        download_target,session,srgb_profile,30)
    fixture_pin=sha256(canonical_bytes(descriptor)).hexdigest()
    result=m.RestoreV2WorkflowResult(
        NS(adoption=NS(batch_id=batch,child_run_id=child),polls=2),
        NS(before=NS(verified=NS(image=NS(sha256='1'*64))),
           after=NS(verified=NS(image=NS(sha256='2'*64),manifest=NS(sha256='3'*64)))),
        NS(observed=NS(review_id=review_id,candidate=NS(sha256='4'*64))),
        NS(),NS(child_run_id=child,derivative_sha256='5'*64 if mode=='changed_download' else '2'*64,
                manifest_sha256='3'*64),1)
    calls=[]
    monkeypatch.setattr(m,'validate_probe_report_root',lambda root:calls.append('root') or root)
    def execute(*args,**kwargs):
        calls.append('execute')
        if mode=='execution_failure':raise ValueError('stage failed')
        return result
    def publish(*args,**kwargs):
        calls.append('publish')
        assert args[2]=='v2_workflow' and args[6]['download_sha256']=='2'*64
        kwargs['check_active']()
        return 'receipt'
    monkeypatch.setattr(m,'run_fixture_v2_workflow',execute)
    monkeypatch.setattr(m,'persist_restore_probe_report',publish)
    def run():return m.run_fixture_v2_workflow_report(tmp_path,replay,
        'invalid' if mode=='bad_fixture' else '0'*64 if mode=='wrong_pin' else fixture_pin,
        object(),object(),('photos',),target,binding,frozen,b'{}',download_target,
        session_id=session,srgb_profile=srgb_profile,tls_context=object(),maximum_seconds=30,
        check_active=lambda:None,
        assert_installed_fixture=lambda:False if mode=='false_fixture_report' else None)
    if mode=='valid':assert run()=='receipt' and calls==['root','execute','publish']
    else:
        with pytest.raises(ValueError):run()
        assert 'publish' not in calls
        if mode=='bad_fixture':assert calls==[]
        if mode=='wrong_pin':assert calls==['root']
