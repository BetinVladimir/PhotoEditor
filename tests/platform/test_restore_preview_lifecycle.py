from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
import psycopg
import pytest
import ssl
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_preview_lifecycle as m


@pytest.mark.parametrize('mode',['valid','existing','authority','changed','physical','parity','unknown_post','poll','fixture_false','activity_false'])
def test_preview_lifecycle_no_mutation_retry_and_final_catalogue_recheck(monkeypatch,mode):
    version=uuid4();target=m.RestoreLegacyPreviewTarget('https://api.test',uuid4(),version,0,
        canonical_bytes({'strength':1}),'fresh','Bearer private')
    binding=m.RestoreLegacyApplyBinding(uuid4(),uuid4(),version,uuid4(),'a'*64)
    download=m.RestorePreviewDownloadTarget(target.origin,'https://objects.test',target.authorization)
    derivative_id,manifest_id=uuid4(),uuid4()
    derivative_sha,manifest_sha='b'*64,'c'*64
    result=NS(identity='initial',derivative=NS(artifact_id=derivative_id,object=NS(sha256=derivative_sha,size=123)),
        manifest=NS(artifact_id=manifest_id,object=NS(sha256=manifest_sha,size=45)))
    events=[];observations=0
    def post(*a,**k):
        events.append('post')
        if mode=='unknown_post':raise ValueError('RESTORE_LEGACY_PREVIEW_OUTCOME_UNKNOWN')
        return NS(run_id=uuid4())
    def observe(*a,**k):
        nonlocal observations
        observations+=1
        if mode=='poll' and observations==1:raise ValueError('RESTORE_PREVIEW_RESULT_NOT_READY')
        if mode=='changed' and observations>1:return NS(identity='changed')
        return result
    def physical(*a,**k):
        events.append('manifest')
        if mode=='physical':raise ValueError('bad bytes')
        return NS(source=NS(artifact_id=binding.source_artifact_id,sha256=binding.source_sha256),
            derivative=NS(artifact_id=derivative_id,sha256=derivative_sha),
            manifest_sha256=manifest_sha,width=32,height=16)
    monkeypatch.setattr(m,'request_fixture_legacy_preview',post)
    monkeypatch.setattr(m,'observe_fixture_legacy_preview_result',observe)
    monkeypatch.setattr(m,'verify_fixture_legacy_preview_manifest',physical)
    def jpeg(*a,**k):
        events.append('jpeg')
        return NS(sha256=derivative_sha,bytes_verified=123,width=32,height=16,icc_sha256=m.sha256(b'x'*128).hexdigest())
    def download_result(*a,**k):
        events.append('download')
        return NS(artifact_id=str(derivative_id),sha256=derivative_sha,bytes_verified=123,
            manifest_sha256='d'*64 if mode=='parity' else manifest_sha,manifest_bytes_verified=45)
    monkeypatch.setattr(m,'verify_fixture_legacy_preview_jpeg',jpeg)
    monkeypatch.setattr(m,'verify_fixture_preview_download',download_result)
    monkeypatch.setattr(m,'sleep',lambda _:None)
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def execute(self,sql,args=None):
            row=('restore',mode!='authority') if sql.startswith('SELECT current_database') else (mode=='existing',)
            return NS(fetchone=lambda:row)
    def invoke():return m.run_fixture_preview(Connection(),None,[],target,binding,download,expected_database='restore',
        source_width=32,source_height=16,srgb_profile=b'x'*128,tls_context=ssl.create_default_context(),maximum_seconds=10,
        assert_installed_fixture=lambda:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode in ('valid','poll'):
        assert invoke().polls==(2 if mode=='poll' else 1)
        assert events==['post','manifest','jpeg','download']
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    assert events.count('post')<=1
    if mode in ('existing','authority','fixture_false','activity_false'):assert not events


@pytest.mark.parametrize('mode',['tls','object_http','object_port','api_port','roi','key'])
def test_preview_invalid_installation_refuses_before_database_or_post(monkeypatch,mode):
    from dataclasses import replace
    version=uuid4()
    target=m.RestoreLegacyPreviewTarget('https://api.test',uuid4(),version,0,
        canonical_bytes({'strength':1}),'fresh','Bearer private')
    download=m.RestorePreviewDownloadTarget(target.origin,'https://objects.test',target.authorization)
    if mode=='object_http':download=replace(download,object_origin='http://objects.test')
    if mode=='object_port':download=replace(download,object_origin='https://objects.test:99999')
    if mode=='api_port':
        target=replace(target,origin='https://api.test:99999');download=replace(download,api_origin=target.origin)
    if mode=='roi':target=replace(target,roi=(31,0,2,1))
    if mode=='key':target=replace(target,idempotency_key='bad key')
    binding=m.RestoreLegacyApplyBinding(uuid4(),uuid4(),version,uuid4(),'a'*64)
    def forbidden(*a,**k):raise AssertionError('mutation before installation validation')
    monkeypatch.setattr(m,'request_fixture_legacy_preview',forbidden)
    with pytest.raises(ValueError):
        m.run_fixture_preview(None,None,[],target,binding,download,expected_database='restore',
            source_width=32,source_height=16,srgb_profile=b'x'*128,
            tls_context=None if mode=='tls' else ssl.create_default_context(),maximum_seconds=10,
            assert_installed_fixture=lambda:None,check_active=lambda:None)


@pytest.mark.parametrize('fault',['source','derivative','manifest','jpeg_sha','jpeg_size','geometry','icc','download_id','download_sha','download_size','download_manifest','download_manifest_size'])
def test_preview_evidence_parity_refuses_cross_source_or_output_substitution(fault):
    source_id,derivative_id=uuid4(),uuid4()
    binding=NS(source_artifact_id=source_id,source_sha256='a'*64)
    result=NS(derivative=NS(artifact_id=derivative_id,object=NS(sha256='b'*64,size=123)),
        manifest=NS(object=NS(sha256='c'*64,size=45)))
    manifest=NS(source=NS(artifact_id=source_id,sha256='a'*64),
        derivative=NS(artifact_id=derivative_id,sha256='b'*64),
        manifest_sha256='c'*64,width=32,height=16)
    jpeg=NS(sha256='b'*64,bytes_verified=123,width=32,height=16,
        icc_sha256=m.sha256(b'x'*128).hexdigest())
    download=NS(artifact_id=str(derivative_id),sha256='b'*64,bytes_verified=123,
        manifest_sha256='c'*64,manifest_bytes_verified=45)
    changes={
        'source':lambda:setattr(manifest.source,'artifact_id',uuid4()),
        'derivative':lambda:setattr(manifest.derivative,'artifact_id',uuid4()),
        'manifest':lambda:setattr(manifest,'manifest_sha256','d'*64),
        'jpeg_sha':lambda:setattr(jpeg,'sha256','d'*64),
        'jpeg_size':lambda:setattr(jpeg,'bytes_verified',122),
        'geometry':lambda:setattr(jpeg,'width',31),
        'icc':lambda:setattr(jpeg,'icc_sha256','d'*64),
        'download_id':lambda:setattr(download,'artifact_id',str(uuid4())),
        'download_sha':lambda:setattr(download,'sha256','d'*64),
        'download_size':lambda:setattr(download,'bytes_verified',122),
        'download_manifest':lambda:setattr(download,'manifest_sha256','d'*64),
        'download_manifest_size':lambda:setattr(download,'manifest_bytes_verified',44),
    }
    changes[fault]()
    with pytest.raises(ValueError,match='RESTORE_PREVIEW_LIFECYCLE_EVIDENCE'):
        m.require_preview_evidence_parity(binding,result,manifest,jpeg,download,b'x'*128)
