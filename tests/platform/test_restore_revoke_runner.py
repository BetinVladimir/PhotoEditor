from dataclasses import replace
from types import SimpleNamespace as NS
from uuid import uuid4
from hashlib import sha256
import ssl
import pytest
from photo_worker.runtime import restore_revoke_runner as module
from photo_worker.runtime.restore_access_probe import RestoreAccessTarget
from photo_worker.runtime.restore_revoke_probe import RestoreRevokeFixture


@pytest.mark.parametrize('mode', ['valid','same_key','foreign_asset','foreign_credential',
    'baseline_failure','physical_failure','revoke_failure','denial_failure','changed_final','fixture_false','activity_false'])
def test_whole_revoke_freeze_sequence_preflights_before_mutation_and_stops_on_failure(monkeypatch, mode):
    f=RestoreRevokeFixture(uuid4(),uuid4(),uuid4(),'learning_person',uuid4(),3,5,'v1',uuid4(),uuid4())
    rev=module.RestoreConsentRevokeTarget('https://restore.test',f,'revoke','Bearer private')
    asset,doc=uuid4(),uuid4()
    access=RestoreAccessTarget(rev.origin,f'/api/v1/tenants/{f.tenant_id}/v2-documents/{doc}',
        '/control',rev.authorization,409,'PROFILE_CONSENT_REVOKED','b'*64)
    document=module.RestoreRevokeDocumentTarget(f,doc,'a'*64,access)
    baseline=module.RestoreFreezeDenialTarget(rev.origin,f.tenant_id,asset,doc,7,'positive',rev.authorization)
    denial=replace(baseline,idempotency_key='negative')
    if mode=='same_key':denial=replace(denial,idempotency_key='positive')
    if mode=='foreign_asset':denial=replace(denial,asset_id=uuid4())
    if mode=='foreign_credential':denial=replace(denial,authorization='Bearer other')
    binding=module.RestoreFreezeBaselineBinding(f.actor_id,uuid4(),uuid4(),'c'*64,'d'*64)
    events=[]
    def step(name):
        events.append(name)
        if mode==name+'_failure':raise ValueError(name)
        return name
    def consent(*args,**kwargs):
        step('consent-after' if kwargs['revoked'] else 'consent-before')
        return 'changed' if mode=='changed_final' and kwargs['revoked'] else 'consent'
    def catalogue(connection,t,*args,**kwargs):step('catalogue-'+t.idempotency_key)
    monkeypatch.setattr(module,'observe_fixture_learning_consent',consent)
    monkeypatch.setattr(module,'verify_fixture_freeze_catalogue',catalogue)
    monkeypatch.setattr(module,'observe_fixture_freeze_baseline',lambda *a,**k:step('baseline'))
    monkeypatch.setattr(module,'verify_fixture_frozen_result',lambda *a,**k:step('physical'))
    def revoke(*args,**kwargs):step('revoke');return NS(final_consent='consent')
    monkeypatch.setattr(module,'observe_fixture_revoke_lifecycle',revoke)
    monkeypatch.setattr(module,'observe_guarded_fixture_freeze_denial',lambda *a,**k:step('denial'))
    def run():return module.run_fixture_revoke_freeze_lifecycle(object(),object(),('photos',),rev,document,
        baseline,denial,binding,expected_database='isolated',tls_context=ssl.create_default_context(),
        assert_installed_fixture=lambda:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert run().final_consent=='consent'
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('same_key','foreign_asset','foreign_credential','fixture_false','activity_false'):assert not events
    else:
        assert events[:3]==['consent-before','catalogue-positive','catalogue-negative']
        order=['baseline','physical','revoke','denial']
        completed=[e for e in events if e in order]
        assert completed==order[:len(completed)]
        if mode.endswith('_failure'):assert events[-1]==mode.removesuffix('_failure')


@pytest.mark.parametrize('mode', ['valid','same_batch_key','foreign_snapshot','foreign_policy',
    'foreign_project','foreign_token','source_hash','zero_session','boolean_timeout',
    'preflight_failure','worker_failure','negative_failure'])
def test_batch_revoke_sequence_requires_completed_worker_before_revoke(monkeypatch, mode):
    f=RestoreRevokeFixture(uuid4(),uuid4(),uuid4(),'learning_person',uuid4(),3,5,'v1',uuid4(),uuid4())
    rev=module.RestoreConsentRevokeTarget('https://restore.test',f,'revoke','Bearer private')
    asset,doc=uuid4(),uuid4()
    access=RestoreAccessTarget(rev.origin,f'/api/v1/tenants/{f.tenant_id}/v2-documents/{doc}',
        '/control',rev.authorization,409,'PROFILE_CONSENT_REVOKED','b'*64)
    document=module.RestoreRevokeDocumentTarget(f,doc,'a'*64,access)
    positive=module.RestoreFreezeDenialTarget(rev.origin,f.tenant_id,asset,doc,7,'positive',rev.authorization)
    negative=replace(positive,idempotency_key='negative')
    source=b'{}'
    binding=module.RestoreFreezeBaselineBinding(f.actor_id,uuid4(),uuid4(),sha256(source).hexdigest(),'d'*64)
    batch=module.RestoreRenderBatchTarget(rev.origin,f.tenant_id,f.project_id,uuid4(),'e'*64,'batch-positive',rev.authorization)
    denied=replace(batch,idempotency_key='batch-negative')
    changes={'same_batch_key':{'idempotency_key':batch.idempotency_key},
        'foreign_snapshot':{'snapshot_id':uuid4()},'foreign_policy':{'policy_sha256':'f'*64},
        'foreign_project':{'project_id':uuid4()},'foreign_token':{'authorization':'Bearer other'}}
    if mode in changes:denied=replace(denied,**changes[mode])
    installation=module.RestoreRevokeBatchInstallation(batch,denied,uuid4(),30,source)
    if mode=='source_hash':installation=replace(installation,source_raw=b'[]')
    if mode=='zero_session':installation=replace(installation,session_id=module.UUID(int=0))
    if mode=='boolean_timeout':installation=replace(installation,maximum_seconds=True)
    events=[]
    frozen=object()
    def step(name):
        events.append(name)
        if mode==name+'_failure':raise ValueError(name)
        return name
    monkeypatch.setattr(module,'observe_fixture_learning_consent',lambda *a,**k:'consent')
    monkeypatch.setattr(module,'verify_fixture_freeze_catalogue',lambda *a,**k:None)
    monkeypatch.setattr(module,'verify_fixture_batch_not_adopted',lambda *a,**k:step('preflight'))
    def freeze(*a,**k):step('freeze');return frozen
    monkeypatch.setattr(module,'observe_fixture_freeze_baseline',freeze)
    monkeypatch.setattr(module,'verify_fixture_frozen_result',lambda *a,**k:step('physical'))
    def worker(*a,**k):
        assert a[3] is batch and a[5] is frozen and a[6]==source
        assert k['session_id']==installation.session_id
        return step('worker')
    monkeypatch.setattr(module,'run_fixture_batch_baseline',worker)
    def revoke(*a,**k):step('revoke');return NS(final_consent='consent')
    monkeypatch.setattr(module,'observe_fixture_revoke_lifecycle',revoke)
    monkeypatch.setattr(module,'observe_guarded_fixture_freeze_denial',lambda *a,**k:step('freeze-denied'))
    def refusal(*a,**k):
        assert a[1] is denied and a[3] is frozen
        return step('negative')
    monkeypatch.setattr(module,'observe_guarded_fixture_batch_refusal',refusal)
    def run():
        return module.run_fixture_revoke_batch_lifecycle(object(),object(),('photos',),rev,document,
            positive,negative,binding,expected_database='isolated',tls_context=ssl.create_default_context(),
            assert_installed_fixture=lambda:None,check_active=lambda:None,batch_installation=installation)
    order=['preflight','preflight','freeze','physical','worker','revoke','freeze-denied','negative']
    if mode=='valid':
        result=run()
        assert result.positive_batch=='worker' and result.negative_batch=='negative'
        assert events==order
    else:
        with pytest.raises(ValueError):run()
        if mode.endswith('_failure'):
            assert events==order[:len(events)]
            assert events[-1]==mode.removesuffix('_failure')
        else:assert events==[]


def test_batch_runner_refuses_missing_installation_before_any_operation():
    with pytest.raises(ValueError,match='BATCH_REQUIRED'):
        module.run_fixture_revoke_batch_lifecycle(batch_installation=None)
