from dataclasses import replace
from datetime import datetime,timezone
from hashlib import sha256
from types import SimpleNamespace as NS
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.export import EncodeParams
from photo_worker.runtime import restore_selection_export as m
from photo_worker.runtime.restore_replay_receipt import VerifiedRestoreReplay
from photo_worker.runtime.restore_legacy_apply import RestoreLegacyApplyTarget,RestoreLegacyApplyTicket
from photo_worker.runtime.restore_legacy_catalogue import RestoreLegacyApplyBinding
from photo_worker.runtime.restore_export_preflight import RestoreExportSelection
from photo_worker.runtime.restore_preview_download import RestorePreviewDownloadTarget


@pytest.mark.parametrize('mode',['valid','pin','digest','final','export_failure','inactive','fixture_denied'])
def test_pinned_selection_export_dynamic_snapshot_and_final_registration(monkeypatch,tmp_path,mode):
    replay=VerifiedRestoreReplay(uuid4(),'restore','a'*64,'b'*64,'c'*64,1,datetime.now(timezone.utc))
    apply=RestoreLegacyApplyTarget('https://api.test',uuid4(),uuid4(),uuid4(),uuid4(),7,'apply','Bearer secret')
    ticket=RestoreLegacyApplyTicket(uuid4(),uuid4(),'d'*64,'e'*64)
    binding=RestoreLegacyApplyBinding(uuid4(),uuid4(),uuid4(),uuid4(),'f'*64)
    target=m.RestoreSelectionTarget(apply.origin,apply.tenant_id,ticket.output_collection_id,uuid4(),1,'selection-secret',apply.authorization)
    selection=RestoreExportSelection('1'*64,1,'2'*64);preset=uuid4()
    download=RestorePreviewDownloadTarget(apply.origin,'https://objects.test',apply.authorization)
    options=dict(params=EncodeParams(image=ArtifactRef(artifact_id=uuid4(),sha256='3'*64),format='jpeg',icc_id='srgb_v1',
        icc_sha256='4'*64,quality=90,alpha='matte',matte_rgb=[1.,1.,1.],metadata_policy='strip'),intent_key='intent-secret',
        scratch_root=str(tmp_path),maximum_seconds=30)
    raw=canonical_bytes(m.selection_export_descriptor(replay,target,preset,'export-secret',binding,selection,apply,ticket,download,**options))
    assert b'secret' not in raw
    pin=sha256(raw).hexdigest()
    if mode=='pin':target=replace(target,collection_revision=2)
    events=[];source=NS(width=20,height=10,alpha=None,result=NS(asset_version_id=uuid4()))
    registration=object();snapshot=uuid4()
    registered=NS(ticket=NS(snapshot_id=snapshot,digest='5'*64 if mode=='digest' else selection.snapshot_digest),registration=registration)
    for name in ('validate_fixture_selection','validate_preview_download_installation','validate_fixture_export','validate_fixture_download_intent'):
        monkeypatch.setattr(m,name,lambda *a,**k:None)
    monkeypatch.setattr(m,'_dimensions',lambda *a:(20,10))
    monkeypatch.setattr(m,'verify_export_geometry',lambda *a:None)
    def physical(*a,**k):events.append('source');return source
    def create(*a,**k):events.append('selection');return registered
    def export(*a,**k):
        events.append('export');assert a[3].selection_snapshot_id==snapshot
        if mode=='export_failure':raise ValueError('export failed')
        return 'completed'
    def observe(*a,**k):events.append('final');return object() if mode=='final' else registration
    monkeypatch.setattr(m,'verify_fixture_legacy_working_image',physical)
    monkeypatch.setattr(m,'create_fixture_selection',create)
    monkeypatch.setattr(m,'run_fixture_export',export)
    monkeypatch.setattr(m,'observe_fixture_selection',observe)
    def invoke():return m.run_pinned_selection_export(replay,pin,None,None,[],target,preset,'export-secret',binding,selection,
        apply,ticket,download,**options,pepper=b'p'*32,tls_context=None,
        assert_installed_fixture=lambda:False if mode=='fixture_denied' else None,
        check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':
        result=invoke();assert result.export=='completed' and result.fixture_sha256==pin
        assert events==['source','selection','export','final']
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
        if mode in ('pin','inactive','fixture_denied'):assert not events
        if mode=='digest':assert events==['source','selection']
        if mode=='export_failure':assert 'final' not in events
