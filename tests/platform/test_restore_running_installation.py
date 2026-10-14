from dataclasses import replace
from datetime import datetime,timezone
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_running_installation as m


@pytest.mark.parametrize('mode',['valid','child','restore_run','build','consent','epoch','key','timeout','bad_pin',
    'barrier_removed','barrier_added','barrier_valid','inactive','fixture_denied'])
def test_running_fixture_pin_rejects_cross_run_and_consent_substitution(monkeypatch,mode):
    replay=m.VerifiedRestoreReplay(uuid4(),'restore','a'*64,'b'*64,'c'*64,1,datetime.now(timezone.utc))
    fixture=m.RestoreRevokeFixture(uuid4(),uuid4(),uuid4(),'learning_person',uuid4(),3,5,'v1',uuid4(),uuid4())
    target=m.RestoreConsentRevokeTarget('https://restore.test',fixture,'secret-key','Bearer secret-token')
    child=uuid4();maximum=30
    barrier=object.__new__(m.RestoreBarrierRenderer) if mode in ('barrier_valid','barrier_added') else None
    raw=canonical_bytes(m.running_revoke_fixture_descriptor(replay,target,child,maximum,
        barrier_required=mode in ('barrier_valid','barrier_removed')))
    assert b'secret' not in raw
    pin=sha256(raw).hexdigest()
    if mode=='child':child=uuid4()
    if mode=='restore_run':replay=replace(replay,run_id=uuid4())
    if mode=='build':replay=replace(replay,build_sha256='d'*64)
    if mode=='consent':target=replace(target,fixture=replace(fixture,consent_id=uuid4()))
    if mode=='epoch':target=replace(target,fixture=replace(fixture,epoch=4))
    if mode=='key':target=replace(target,idempotency_key='other')
    if mode=='timeout':maximum=31
    if mode=='bad_pin':pin='0'*64
    calls=[]
    def execute(*a,**k):
        assert a[2]==child and k['expected_database']=='restore'
        assert k['renderer_barrier'] is barrier
        calls.append('execute');return 'observation'
    monkeypatch.setattr(m,'run_fixture_running_revoke',execute)
    def run():return m.run_pinned_running_revoke(replay,pin,object(),target,child,maximum_seconds=maximum,
        tls_context=object(),
        assert_installed_fixture=lambda:calls.append('scope') or (False if mode=='fixture_denied' else None),
        check_active=lambda:False if mode=='inactive' else None,
        renderer_barrier=barrier)
    if mode in ('valid','barrier_valid'):assert run()=='observation' and calls==['scope','execute']
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        assert 'execute' not in calls
