from uuid import uuid4
import pytest
from photo_worker.runtime import restore_revoke_running_lifecycle as m


@pytest.mark.parametrize('mode',['valid','changed_generation','unknown_post','timeout','authority','late_success','consent_changed','fixture_false','activity_false'])
def test_running_revoke_mutates_once_and_bounds_only_readonly_polling(monkeypatch,mode):
    target=m.RestoreConsentRevokeTarget('https://restore.test',object(),'fresh','Bearer private')
    events=[];clock=[0.];reads=[0];polls=[0];consents=[0]
    monkeypatch.setattr(m,'monotonic',lambda:clock[0])
    monkeypatch.setattr(m,'sleep',lambda n:clock.__setitem__(0,clock[0]+n))
    def consent(*a,**k):
        consents[0]+=1
        return 'changed' if mode=='consent_changed' and consents[0]==3 else 'consent'
    def running(*a,**k):
        reads[0]+=1;events.append('running')
        return reads[0] if mode=='changed_generation' else 'generation'
    def post(*a,**k):
        events.append('post')
        if mode=='unknown_post':raise ValueError('RESTORE_REVOKE_REQUEST_OUTCOME_UNKNOWN')
        return 'receipt'
    def cancelled(*a,**k):
        assert a[2]=='generation';polls[0]+=1;events.append('poll')
        if mode=='authority':raise ValueError('RESTORE_CANCELLED_RENDER_AUTHORITY')
        if mode=='late_success':clock[0]=1.;return 'cancelled'
        if mode=='timeout' or polls[0]==1:raise ValueError('RESTORE_CANCELLED_RENDER_NOT_SETTLED')
        return 'cancelled'
    monkeypatch.setattr(m,'observe_fixture_learning_consent',consent)
    monkeypatch.setattr(m,'observe_fixture_running_render',running)
    monkeypatch.setattr(m,'request_fixture_consent_revoke',post)
    monkeypatch.setattr(m,'observe_fixture_cancelled_render',cancelled)
    def run():return m.run_fixture_running_revoke(object(),target,uuid4(),expected_database='restore',
        tls_context=object(),maximum_seconds=1,
        assert_installed_fixture=lambda:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':
        result=run();assert result.polls==2 and result.cancelled=='cancelled'
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    assert events.count('post')==(0 if mode in ('changed_generation','fixture_false','activity_false') else 1)
    if mode in ('fixture_false','activity_false'):assert events==[]
    if mode=='unknown_post':assert polls[0]==0
    if mode in ('authority','late_success'):assert polls[0]==1
