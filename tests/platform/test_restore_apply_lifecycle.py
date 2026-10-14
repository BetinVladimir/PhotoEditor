from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
import psycopg
import pytest
import ssl
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_apply_lifecycle as m


@pytest.mark.parametrize('mode',['valid','existing','authority','changed','physical','mismatch','unknown_post','poll','download','fixture_false','activity_false'])
def test_apply_lifecycle_no_mutation_retry_and_final_catalogue_recheck(monkeypatch,mode):
    target=m.RestoreLegacyApplyTarget('https://api.test',uuid4(),uuid4(),uuid4(),uuid4(),7,'fresh','Bearer private')
    binding=m.RestoreLegacyApplyBinding(uuid4(),uuid4(),uuid4(),uuid4(),'a'*64)
    download=m.RestorePreviewDownloadTarget(target.origin,'https://objects.test',target.authorization)
    artifact_id=uuid4();events=[]
    result=NS(identity='initial',artifact_id=artifact_id,object=NS(sha256='b'*64,size=123))
    observations=0
    def post(*a,**k):
        events.append('post')
        if mode=='unknown_post':raise ValueError('RESTORE_LEGACY_APPLY_OUTCOME_UNKNOWN')
        return NS(run_id=uuid4())
    def observe(*a,**k):
        nonlocal observations
        observations+=1
        if mode=='poll' and observations==1:raise ValueError('RESTORE_LEGACY_RESULT_NOT_READY')
        if mode=='changed' and observations>1:return NS(identity='changed')
        return result
    def physical(*a,**k):
        events.append('physical')
        if mode=='physical':raise ValueError('bad bytes')
        return NS(result=result)
    monkeypatch.setattr(m,'request_fixture_legacy_apply',post)
    monkeypatch.setattr(m,'observe_fixture_legacy_apply_result',observe)
    monkeypatch.setattr(m,'verify_fixture_legacy_working_image',physical)
    def downloaded(*a,**k):
        events.append('download')
        if mode=='download':raise ValueError('bad delivery')
        return NS(artifact_id=str(uuid4() if mode=='mismatch' else artifact_id),sha256='b'*64,bytes_verified=123)
    monkeypatch.setattr(m,'verify_fixture_apply_download',downloaded)
    monkeypatch.setattr(m,'sleep',lambda _:None)
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,args=None):
            row=('restore',mode!='authority') if sql.startswith('SELECT current_database') else (mode=='existing',)
            return NS(fetchone=lambda:row)
    def invoke():return m.run_fixture_legacy_apply(Connection(),None,[],target,binding,m.RestorePreviewDownloadTarget(target.origin,'https://objects.test',target.authorization),expected_database='restore',
        tls_context=ssl.create_default_context(),maximum_seconds=10,
        assert_installed_fixture=lambda:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode in ('valid','poll'):
        assert invoke().polls==(2 if mode=='poll' else 1)
        assert events==['post','physical','download']
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    assert events.count('post')<=1
    if mode in ('existing','authority','fixture_false','activity_false'):assert not events


@pytest.mark.parametrize('mode',['tls','http','port','revision','key','authorization','nil_style'])
def test_apply_invalid_installation_never_reads_database_or_posts(monkeypatch,mode):
    from dataclasses import replace
    from uuid import UUID
    target=m.RestoreLegacyApplyTarget('https://api.test',uuid4(),uuid4(),uuid4(),uuid4(),7,'fresh','Bearer private')
    if mode=='http':target=replace(target,origin='http://api.test')
    if mode=='port':target=replace(target,origin='https://api.test:99999')
    if mode=='revision':target=replace(target,source_revision=True)
    if mode=='key':target=replace(target,idempotency_key='bad key')
    if mode=='authorization':target=replace(target,authorization='Bearer private\r\ninjected')
    if mode=='nil_style':target=replace(target,style_version_id=UUID(int=0))
    binding=m.RestoreLegacyApplyBinding(uuid4(),uuid4(),uuid4(),uuid4(),'a'*64)
    def forbidden(*a,**k):raise AssertionError('POST before preflight')
    monkeypatch.setattr(m,'request_fixture_legacy_apply',forbidden)
    with pytest.raises(ValueError):
        m.run_fixture_legacy_apply(None,None,[],target,binding,m.RestorePreviewDownloadTarget(target.origin,'https://objects.test',target.authorization),expected_database='restore',maximum_seconds=10,
            tls_context=None if mode=='tls' else ssl.create_default_context(),
            assert_installed_fixture=lambda:None,check_active=lambda:None)


@pytest.mark.parametrize('fault',['artifact','sha','size'])
def test_apply_download_evidence_must_match_adopted_catalogue(fault):
    artifact=uuid4()
    result=NS(artifact_id=artifact,object=NS(sha256='a'*64,size=123))
    download=NS(artifact_id=str(artifact),sha256='a'*64,bytes_verified=123)
    if fault=='artifact':download.artifact_id=str(uuid4())
    if fault=='sha':download.sha256='b'*64
    if fault=='size':download.bytes_verified=122
    with pytest.raises(ValueError,match='RESTORE_APPLY_LIFECYCLE_EVIDENCE'):
        m.require_apply_download_parity(result,download)
