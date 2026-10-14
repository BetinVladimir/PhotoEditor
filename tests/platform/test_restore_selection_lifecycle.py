from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
import ssl
import psycopg
import pytest
from photo_worker.runtime import restore_selection_lifecycle as m
from photo_worker.runtime.restore_inventory import RestoreObject


@pytest.mark.parametrize('mode',['valid','authority','existing','source','unknown','receipt','tls','inactive','fixture_denied'])
def test_selection_fresh_source_before_one_post(monkeypatch,mode):
    applied=m.RestoreLegacyApplyResult(NS(output_collection_id=uuid4(),run_id=uuid4()),uuid4(),uuid4(),uuid4(),
        RestoreObject('photo','tenant/master','v1','a'*64,512))
    target=m.RestoreSelectionTarget('https://restore.test',uuid4(),applied.registration.output_collection_id,applied.item_id,7,'fresh','Bearer secret')
    actor=uuid4();calls=[];ticket=object();registration=object()
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):calls.append('sql');yield
        def execute(self,sql,args=None):
            if sql.startswith('SELECT current_database'):row=('restore',mode!='authority')
            elif sql.startswith('SELECT EXISTS(SELECT FROM photo.idempotency_records'):
                assert args==(target.tenant_id,actor,'fresh');row=(mode=='existing',)
            elif sql.startswith('SELECT EXISTS(SELECT FROM photo.collections'):
                assert args[-4:]==(applied.item_id,applied.asset_version_id,applied.artifact_id,applied.object.sha256)
                row=(mode!='source',)
            else:row=None
            return NS(fetchone=lambda:row)
    def post(*a,**k):
        calls.append('post')
        if mode=='unknown':raise ValueError('RESTORE_SELECTION_OUTCOME_UNKNOWN')
        return ticket
    def observe(*a,**k):
        calls.append('receipt');assert a[2] is ticket
        if mode=='receipt':raise ValueError('invalid receipt')
        return registration
    monkeypatch.setattr(m,'request_fixture_selection',post)
    monkeypatch.setattr(m,'observe_fixture_selection',observe)
    context=ssl.create_default_context()
    if mode=='tls':context.check_hostname=False
    def invoke():return m.create_fixture_selection(Connection(),target,actor,applied,expected_database='restore',
        tls_context=context,assert_installed_fixture=lambda:False if mode=='fixture_denied' else None,
        check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':
        result=invoke();assert result.ticket is ticket and result.registration is registration
        assert calls==['sql','post','receipt']
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
        if mode in ('authority','existing','source','tls','inactive','fixture_denied'):assert 'post' not in calls
        if mode=='unknown':assert 'receipt' not in calls
        if mode in ('tls','inactive','fixture_denied'):assert not calls
    assert calls.count('post')<=1
