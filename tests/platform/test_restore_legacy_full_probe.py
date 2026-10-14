from datetime import datetime,timezone
from hashlib import sha256
from types import SimpleNamespace as NS
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.export import EncodeParams
from photo_worker.runtime.restore_legacy_apply import RestoreLegacyApplyTarget
from photo_worker.runtime.restore_legacy_lifecycle import RestoreLegacyInstallation
from photo_worker.runtime.restore_preview_download import RestorePreviewDownloadTarget
from photo_worker.runtime.restore_replay_receipt import VerifiedRestoreReplay
from photo_worker.runtime import restore_legacy_full_probe as m


@pytest.mark.parametrize('mode',['valid','wrong_pin','legacy_failure','binder_failure',
                                 'export_failure','wrong_download','changed_source','false_fixture','false_active'])
def test_legacy_complete_probe_requires_encoded_export_and_redeemed_volume(monkeypatch,tmp_path,mode):
    replay=VerifiedRestoreReplay(uuid4(),'isolated','a'*64,'b'*64,'c'*64,1,
        datetime(2020,1,1,tzinfo=timezone.utc))
    tenant,collection,item,style,output,run,applied_id,preset=(uuid4() for _ in range(8))
    apply=RestoreLegacyApplyTarget('https://api.test',tenant,collection,item,style,1,
        'apply-key','Bearer private')
    preview_target=NS(tenant_id=tenant,origin=apply.origin,authorization=apply.authorization,
                      idempotency_key='preview-key')
    download_target=RestorePreviewDownloadTarget(apply.origin,'https://objects.test',apply.authorization)
    installation=RestoreLegacyInstallation(preview_target,apply,NS(),download_target,100,80,30)
    placeholder=ArtifactRef(artifact_id=uuid4(),sha256='0'*64)
    params=EncodeParams(image=placeholder,format='jpeg',icc_id='srgb_v1',icc_sha256='d'*64,
        quality=90,alpha='matte',matte_rgb=[1.,1.,1.],metadata_policy='strip')
    plan=m.RestoreLegacyExportPlan(preset,1,'e'*64,'selection-key','export-key','intent-key',
        params,tmp_path,30)
    monkeypatch.setattr(m,'legacy_fixture_descriptor',lambda *a:{'schema_version':'legacy-test'})
    descriptor=m.legacy_complete_fixture_descriptor(replay,('photos',),installation,plan,b'P'*128,b'K'*32,300)
    pin=sha256(canonical_bytes(descriptor)).hexdigest()
    assert b'Bearer private' not in canonical_bytes(descriptor)
    applied=NS(artifact_id=applied_id,object=NS(sha256='f'*64))
    ticket=NS(run_id=run,output_collection_id=output)
    legacy=NS(preview=NS(download=NS(sha256='1'*64),polls=1),
        apply=NS(ticket=ticket,result=applied,
            download=NS(artifact_id=str(applied_id),sha256='f'*64),polls=1))
    selection=NS(target=NS(),expected=NS(snapshot_digest='3'*64))
    encoded=NS(artifact_id=uuid4(),delivery_kind='single',object=NS(sha256='4'*64,size=123))
    volume=NS(artifact_id=encoded.artifact_id,delivery_kind='single',object=NS(sha256='4'*64,size=123))
    completed=NS(physical=NS(result=encoded,sha256='4'*64,bytes_verified=123),
        delivery=NS(volume=volume,download=NS(artifact_id=str(volume.artifact_id),
            sha256='6'*64 if mode=='wrong_download' else '4'*64,bytes_verified=123)),
        archive=None,polls=1)
    events=[];binding_reads=[0]
    monkeypatch.setattr(m,'validate_probe_report_root',lambda root:root)
    if mode=='false_active':
        monkeypatch.setattr(m,'validate_probe_report_root',
            lambda root: (_ for _ in ()).throw(AssertionError('report root read')))
    monkeypatch.setattr(m,'validate_fixture_export',lambda *a,**k:events.append('export preflight'))
    monkeypatch.setattr(m,'validate_fixture_download_intent',lambda *a,**k:events.append('intent preflight'))
    def legacy_call(*a,**k):
        events.append('legacy')
        if mode=='legacy_failure':raise ValueError('legacy failed')
        return legacy
    def binder(*a,**k):
        binding_reads[0]+=1;events.append('binding')
        if mode=='binder_failure':raise ValueError('binding failed')
        return NS() if mode=='changed_source' and binding_reads[0]>1 else selection
    def dynamic(*a,**k):
        events.append('dynamic')
        assert k['params'].image==ArtifactRef(artifact_id=applied_id,sha256='f'*64)
        return {'dynamic':'exact'}
    dynamic_pin=sha256(canonical_bytes({'dynamic':'exact'})).hexdigest()
    def export_call(*a,**k):
        events.append('export')
        assert a[1]==dynamic_pin and k['params'].image.artifact_id==applied_id
        if mode=='export_failure':raise ValueError('export failed')
        return NS(fixture_sha256=dynamic_pin,selection=NS(ticket=NS(snapshot_id=uuid4(),digest='3'*64)),
                  export=completed)
    monkeypatch.setattr(m,'run_pinned_legacy_observations',legacy_call)
    monkeypatch.setattr(m,'bind_fixture_legacy_selection',binder)
    monkeypatch.setattr(m,'selection_export_descriptor',dynamic)
    monkeypatch.setattr(m,'run_pinned_selection_export',export_call)
    monkeypatch.setattr(m,'persist_restore_probe_report',lambda *a,**k:events.append('report') or 'receipt')
    def run():return m.run_legacy_apply_probe(replay,'0'*64 if mode=='wrong_pin' else pin,
        report_root=tmp_path,connection=object(),client=object(),buckets=('photos',),
        installation=installation,plan=plan,srgb_profile=b'P'*128,icc_registry=object(),
        pepper=b'K'*32,tls_context=object(),maximum_seconds=300,
        assert_installed_fixture=lambda:False if mode=='false_fixture' else None,
        check_active=lambda:False if mode=='false_active' else None)
    if mode=='valid':
        assert run()=='receipt' and events[-1]=='report' and binding_reads[0]==2
    else:
        with pytest.raises(ValueError):run()
        assert 'report' not in events
        if mode in ('wrong_pin','false_fixture','false_active'):assert 'legacy' not in events
        if mode in ('legacy_failure','binder_failure'):assert 'export' not in events
