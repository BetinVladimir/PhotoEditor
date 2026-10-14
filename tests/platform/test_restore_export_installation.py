from dataclasses import replace
from datetime import datetime,timezone
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.export import EncodeParams
from photo_worker.runtime import restore_export_installation as m
from photo_worker.runtime.restore_replay_receipt import VerifiedRestoreReplay
from photo_worker.runtime.restore_legacy_apply import RestoreLegacyApplyTarget
from photo_worker.runtime.restore_legacy_catalogue import RestoreLegacyApplyBinding
from photo_worker.runtime.restore_preview_download import RestorePreviewDownloadTarget


@pytest.mark.parametrize('mode',['valid','preset','snapshot','params','intent','scratch','ticket','replay','inactive','fixture_denied'])
def test_export_pin_rejects_substitution_before_any_io(monkeypatch,tmp_path,mode):
    replay=VerifiedRestoreReplay(uuid4(),'restore','a'*64,'b'*64,'c'*64,1,datetime.now(timezone.utc))
    apply=RestoreLegacyApplyTarget('https://api.test',uuid4(),uuid4(),uuid4(),uuid4(),7,'apply','Bearer secret')
    binding=RestoreLegacyApplyBinding(uuid4(),uuid4(),uuid4(),uuid4(),'d'*64)
    target=m.RestoreExportTarget(apply.origin,apply.tenant_id,uuid4(),uuid4(),'export-secret',apply.authorization)
    ticket=m.RestoreLegacyApplyTicket(uuid4(),uuid4(),'e'*64,'f'*64)
    selection=m.RestoreExportSelection('1'*64,1,'2'*64)
    download=RestorePreviewDownloadTarget(apply.origin,'https://objects.test',apply.authorization)
    options=dict(params=EncodeParams(image=ArtifactRef(artifact_id=uuid4(),sha256='3'*64),format='jpeg',
        icc_id='srgb_v1',icc_sha256='4'*64,quality=90,alpha='matte',matte_rgb=[1.,1.,1.],metadata_policy='strip'),
        intent_key='intent-secret',scratch_root=str(tmp_path),maximum_seconds=30)
    def descriptor():return m.export_fixture_descriptor(replay,target,binding,selection,apply,ticket,download,**options)
    raw=canonical_bytes(descriptor());pin=sha256(raw).hexdigest();assert b'secret' not in raw
    if mode=='preset':target=replace(target,preset_id=uuid4())
    if mode=='snapshot':selection=replace(selection,snapshot_digest='5'*64)
    if mode=='params':options['params']=options['params'].model_copy(update={'quality':91})
    if mode=='intent':options['intent_key']='other'
    if mode=='scratch':options['scratch_root']=str(tmp_path/'other')
    if mode=='ticket':ticket=replace(ticket,run_id=uuid4())
    if mode=='replay':replay=replace(replay,build_sha256='6'*64)
    calls=[]
    monkeypatch.setattr(m,'run_fixture_export',lambda *a,**k:calls.append(k['expected_database']) or 'complete')
    def invoke():return m.run_pinned_export(replay,pin,None,None,[],target,binding,selection,apply,ticket,download,
        **options,pepper=b'p'*32,tls_context=None,
        assert_installed_fixture=lambda:calls.append('scope') or (False if mode=='fixture_denied' else None),
        check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':assert invoke()=='complete' and calls==['scope','restore']
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
        if mode in ('inactive','fixture_denied'):assert 'restore' not in calls
        else:assert calls==[]
