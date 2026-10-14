from dataclasses import replace
from datetime import datetime,timezone
from hashlib import sha256
from types import SimpleNamespace as NS
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_revoke_installation as module
from photo_worker.runtime.restore_revoke_probe import RestoreRevokeFixture
from photo_worker.runtime.restore_revoke_runner import RestoreRevokeBatchInstallation
from photo_worker.runtime.restore_revoke_batch import RestoreRenderBatchTarget
from photo_worker.runtime.restore_revoke_freeze import RestoreFreezeBaselineBinding,RestoreFreezeDenialTarget
from photo_worker.runtime.restore_replay_receipt import VerifiedRestoreReplay


@pytest.mark.parametrize('mode',['valid','bad_pin','changed_policy','changed_session','changed_source','changed_key',
    'changed_run','changed_build','changed_recovery','changed_replay','inactive','fixture_denied'])
def test_independent_pin_precedes_mutation_and_excludes_secrets(monkeypatch,mode):
    replay=VerifiedRestoreReplay(uuid4(),'restore','a'*64,'b'*64,'c'*64,1,datetime.now(timezone.utc))
    fixture=RestoreRevokeFixture(uuid4(),uuid4(),uuid4(),'learning_person',uuid4(),3,5,'v1',uuid4(),uuid4())
    revoke=NS(fixture=fixture,origin='https://restore.test',idempotency_key='secret-revoke',authorization='Bearer secret')
    document=NS(document_id=uuid4(),document_sha256='d'*64,access=NS(denied_path='/private',
        control_path='/control',control_sha256='e'*64,expected_status=409,expected_code='PROFILE_CONSENT_REVOKED'))
    positive=RestoreFreezeDenialTarget(revoke.origin,fixture.tenant_id,uuid4(),document.document_id,7,'secret-positive',revoke.authorization)
    negative=replace(positive,idempotency_key='secret-negative')
    source=b'{"secret-source":true}'
    binding=RestoreFreezeBaselineBinding(fixture.actor_id,uuid4(),uuid4(),sha256(source).hexdigest(),'f'*64)
    batch=RestoreRenderBatchTarget(revoke.origin,fixture.tenant_id,fixture.project_id,uuid4(),'1'*64,'secret-batch',revoke.authorization)
    installation=RestoreRevokeBatchInstallation(batch,replace(batch,idempotency_key='secret-denied'),uuid4(),30,source)
    descriptor=module.revoke_fixture_descriptor(replay,revoke,document,positive,negative,binding,installation)
    raw=canonical_bytes(descriptor)
    assert b'secret' not in raw and source not in raw
    pin=sha256(raw).hexdigest()
    if mode=='bad_pin':pin='0'*64
    if mode=='changed_policy':installation=replace(installation,positive=replace(batch,policy_sha256='2'*64))
    if mode=='changed_session':installation=replace(installation,session_id=uuid4())
    if mode=='changed_source':installation=replace(installation,source_raw=b'[]')
    if mode=='changed_key':installation=replace(installation,positive=replace(batch,idempotency_key='other'))
    if mode=='changed_run':replay=replace(replay,run_id=uuid4())
    if mode=='changed_build':replay=replace(replay,build_sha256='2'*64)
    if mode=='changed_recovery':replay=replace(replay,recovery_sha256='2'*64)
    if mode=='changed_replay':replay=replace(replay,replay_sha256='2'*64)
    calls=[]
    def execute(*args,**kwargs):
        calls.append('execute')
        assert kwargs['expected_database']==replay.database
        assert kwargs['batch_installation'] is installation
        return 'observations'
    monkeypatch.setattr(module,'run_fixture_revoke_batch_lifecycle',execute)
    def run():return module.run_pinned_revoke_observations(replay,pin,object(),object(),('photos',),
        revoke,document,positive,negative,binding,batch_installation=installation,tls_context=object(),
        assert_installed_fixture=lambda:calls.append('scope') or (False if mode=='fixture_denied' else None),
        check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':
        assert run()=='observations'
        assert calls==['scope','execute']
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        assert 'execute' not in calls
