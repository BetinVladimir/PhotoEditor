from types import SimpleNamespace as NS
from uuid import uuid4
import pytest
from photo_worker.runtime import restore_batch_catalogue as module
from photo_worker.runtime import restore_revoke_probe as consent_module
from photo_worker.runtime import restore_revoke_batch as batch_module
from photo_worker.runtime.restore_revoke_freeze import RestoreFreezeBaselineBinding


@pytest.mark.parametrize('mode',['valid','consent_failure','catalogue_failure','http_failure','changed_final'])
def test_guarded_batch_requires_fresh_consent_snapshot_and_no_adoption_after_http(monkeypatch,mode):
    f=consent_module.RestoreRevokeFixture(uuid4(),uuid4(),uuid4(),'learning_person',uuid4(),3,5,'v1',uuid4(),uuid4())
    t=batch_module.RestoreRenderBatchTarget('https://restore.test',f.tenant_id,f.project_id,uuid4(),'a'*64,'batch','Bearer secret')
    b=RestoreFreezeBaselineBinding(f.actor_id,uuid4(),uuid4(),'b'*64,'c'*64);frozen=NS();events=[]
    def step(name):
        events.append(name)
        if mode==name+'_failure':raise ValueError(name)
    def consent(*args,**kwargs):
        assert kwargs['revoked'] is True;step('consent')
        return 'changed' if mode=='changed_final' and 'http' in events else 'consent'
    def catalogue(*args,**kwargs):step('catalogue');return 'catalogue'
    def http(*args,**kwargs):
        kwargs['assert_current_fixture'](*args)
        step('http')
        kwargs['assert_current_fixture'](*args)
        return 'refused'
    monkeypatch.setattr(consent_module,'observe_fixture_learning_consent',consent)
    monkeypatch.setattr(module,'verify_fixture_batch_not_adopted',catalogue)
    monkeypatch.setattr(batch_module,'observe_fixture_render_batch_refused',http)
    def run():return module.observe_guarded_fixture_batch_refusal(object(),t,b,frozen,f,expected_database='isolated',
        session_id=uuid4(),tls_context=object(),assert_fixture_scope=lambda *args:None,check_active=lambda:None)
    if mode=='valid':
        before,refusal,after=run();assert before==after and refusal=='refused'
        assert events.count('consent')==4 and events.count('catalogue')==4
    else:
        with pytest.raises(ValueError):run()
    if mode in ('consent_failure','catalogue_failure'):assert 'http' not in events
