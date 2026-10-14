from types import SimpleNamespace as NS
from uuid import uuid4
from photo_worker.contracts.export import EncodeParams
from photo_worker.contracts.base import ArtifactRef
import ssl
import pytest
from photo_worker.runtime import restore_export_lifecycle as m
from photo_worker.runtime.restore_export import RestoreExportTarget
from photo_worker.runtime.restore_preview_download import RestorePreviewDownloadTarget


@pytest.mark.parametrize('mode',['single','zip','not_ready','physical_changed','source_changed','archive_changed','post_unknown','geometry','unknown_kind','inactive','fixture_denied'])
def test_export_lifecycle_complete_order_and_no_retry(monkeypatch,tmp_path,mode):
    tenant,job=uuid4(),uuid4();target=RestoreExportTarget('https://restore.test',tenant,uuid4(),uuid4(),'export','Bearer secret')
    apply=NS(tenant_id=tenant,origin=target.origin,authorization=target.authorization)
    download=RestorePreviewDownloadTarget(target.origin,'https://objects.test',target.authorization)
    source=NS(width=20,height=10,alpha=None,result=NS(asset_version_id=uuid4()))
    volume=object();encoded=NS(delivery_kind='zip' if mode in ('zip','archive_changed') else 'unknown' if mode=='unknown_kind' else 'single',volume_no=1)
    ticket=NS(download_job_id=job);events=[]
    for name in ('validate_fixture_export','validate_preview_download_installation','validate_fixture_download_intent'):
        monkeypatch.setattr(m,name,lambda *a,**k:None)
    monkeypatch.setattr(m,'_dimensions',lambda *a:(20,10))
    def geometry(*args):
        if mode=='geometry':raise ValueError('RESTORE_EXPORT_GEOMETRY_BINDING')
    monkeypatch.setattr(m,'verify_export_geometry',geometry)
    monkeypatch.setattr(m,'sleep',lambda _:None)
    def read_source(*a,**k):
        events.append('source')
        return object() if mode=='source_changed' and events.count('source')>1 else source
    def preflight(*a,**k):events.append('preflight')
    def post(*a,**k):
        events.append('post')
        if mode=='post_unknown':raise ValueError('unknown')
        return ticket
    def observe(*a,**k):
        events.append('observe')
        if mode=='not_ready' and events.count('observe')==1:raise ValueError('RESTORE_EXPORT_RESULT_NOT_READY')
        return encoded
    def physical(*a,**k):events.append('physical');return NS(result=object() if mode=='physical_changed' else encoded)
    def archive(*a,**k):events.append('archive');return NS(volume=object() if mode=='archive_changed' else volume)
    def delivery(*a,**k):events.append('delivery');return NS(volume=volume)
    monkeypatch.setattr(m,'verify_fixture_legacy_working_image',read_source)
    monkeypatch.setattr(m,'verify_fixture_export_preflight',preflight)
    monkeypatch.setattr(m,'request_fixture_export',post)
    monkeypatch.setattr(m,'observe_fixture_encoded_export',observe)
    monkeypatch.setattr(m,'read_fixture_export_volume',lambda *a,**k:volume)
    monkeypatch.setattr(m,'verify_fixture_encoded_export',physical)
    monkeypatch.setattr(m,'verify_fixture_archive',archive)
    monkeypatch.setattr(m,'run_fixture_volume_delivery',delivery)
    if mode=='inactive':
        original_resolve=type(tmp_path).resolve
        def resolve(path,*args,**kwargs):
            if path==tmp_path:raise AssertionError('scratch root read before active guard')
            return original_resolve(path,*args,**kwargs)
        monkeypatch.setattr(type(tmp_path),'resolve',resolve)
    def invoke():return m.run_fixture_export(object(),object(),{},target,NS(actor_id=uuid4()),object(),apply,object(),download,
        expected_database='restore',params=EncodeParams(image=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),format='jpeg',icc_id='srgb_v1',
            icc_sha256='b'*64,quality=90,alpha='matte',matte_rgb=[1.,1.,1.],metadata_policy='strip'),
        intent_key='intent',pepper=b'p'*32,scratch_root=str(tmp_path),
        tls_context=ssl.create_default_context(),maximum_seconds=300,
        assert_installed_fixture=lambda:False if mode=='fixture_denied' else None,
        check_active=lambda:False if mode=='inactive' else None)
    if mode in ('single','zip','not_ready'):
        result=invoke();assert result.polls==(2 if mode=='not_ready' else 1)
        assert events[-2:] == ['source','preflight'] and events.index('physical')<events.index('delivery')
        assert (result.archive is not None)==(mode=='zip')
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
        if mode in ('physical_changed','archive_changed','post_unknown','geometry','unknown_kind'):assert 'delivery' not in events
    assert events.count('post')==(0 if mode in ('geometry','inactive','fixture_denied') else 1)
    if mode in ('inactive','fixture_denied'):assert not events
