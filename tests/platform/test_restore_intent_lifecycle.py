from contextlib import contextmanager
from datetime import datetime,timedelta,timezone
from types import SimpleNamespace as NS
from uuid import uuid4
import ssl
import psycopg
import pytest
from photo_worker.runtime import restore_intent_lifecycle as m
from photo_worker.runtime.restore_inventory import RestoreObject


@pytest.mark.parametrize('mode',['valid','existing','authority','post_unknown','registration','scope_changed','invalid_tls','inactive','fixture_denied'])
def test_fresh_intent_admission_is_single_attempt_and_requires_registration(monkeypatch,mode):
    target=m.RestoreDownloadIntentTarget('https://restore.test',uuid4(),uuid4(),1,'fresh','Bearer secret')
    actor,artifact=uuid4(),uuid4()
    volume=m.RestoreExportVolume(artifact,RestoreObject('photo','tenant/file','v1','a'*64,12),None,None,'single')
    ticket=m.RestoreDownloadIntentTicket('x'*43,datetime.now(timezone.utc)+timedelta(seconds=50),'b'*64,'c'*64)
    registration=m.RestoreIntentRegistration(uuid4(),artifact,'d'*64)
    events=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):
            events.append('transaction')
            yield
            events.append('commit')
        def execute(self,sql,args=None):
            if sql.startswith('SELECT current_database'):row=('restore',mode!='authority')
            elif sql.startswith('SELECT EXISTS'):
                assert "route='Download.Intent'" in sql
                assert args==(target.tenant_id,actor,'fresh')
                row=(mode=='existing',)
            else:row=None
            return NS(fetchone=lambda:row)
    def post(*args,**kwargs):
        events.append('post')
        assert events.count('commit')==1
        if mode=='post_unknown':raise ValueError('RESTORE_DOWNLOAD_INTENT_OUTCOME_UNKNOWN')
        return ticket
    def observe(*args,**kwargs):
        events.append('registration')
        assert args[2] is ticket and kwargs['pepper']==b'p'*32
        if mode=='registration':raise ValueError('RESTORE_INTENT_CATALOGUE_BINDING')
        return registration
    def scope():
        if mode=='scope_changed' and 'post' in events:raise ValueError('fixture changed')
        if mode=='fixture_denied':return False
    monkeypatch.setattr(m,'request_fixture_download_intent',post)
    monkeypatch.setattr(m,'observe_fixture_owner_intent',observe)
    context=ssl.create_default_context()
    if mode=='invalid_tls':context.check_hostname=False
    def invoke():return m.create_fixture_owner_intent(Connection(),target,actor,volume,
        expected_database='restore',pepper=b'p'*32,tls_context=context,
        assert_installed_fixture=scope,check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':
        result=invoke()
        assert result.ticket is ticket and result.registration is registration
        assert ticket.token not in repr(result)
        assert events.count('post')==1 and events.count('registration')==1
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
        assert events.count('post')<=1
        if mode in ('existing','authority','invalid_tls','inactive','fixture_denied'):assert 'post' not in events
        if mode in ('invalid_tls','inactive','fixture_denied'):assert 'transaction' not in events
        if mode in ('post_unknown','scope_changed'):assert 'registration' not in events
