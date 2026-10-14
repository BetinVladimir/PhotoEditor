from dataclasses import replace
from types import SimpleNamespace as NS
from datetime import datetime,timezone
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_preview_installation as m


@pytest.mark.parametrize('mode',['valid','database','build','source','width','icc','key','params','origin','timeout','alpha_dtype','fixture_false','activity_false'])
def test_preview_independent_pin_binds_installation_before_io(monkeypatch,mode):
    replay=m.VerifiedRestoreReplay(uuid4(),'restore','a'*64,'b'*64,'c'*64,1,datetime.now(timezone.utc))
    version=uuid4();target=m.RestoreLegacyPreviewTarget('https://api.test',uuid4(),version,0,
        canonical_bytes({'strength':1}),'secret-key','Bearer secret-token')
    binding=m.RestoreLegacyApplyBinding(uuid4(),uuid4(),version,uuid4(),'d'*64)
    download=m.RestorePreviewDownloadTarget(target.origin,'https://objects.test',target.authorization)
    options=dict(source_width=32,source_height=16,srgb_profile=b'x'*128,maximum_seconds=30)
    raw=canonical_bytes(m.preview_fixture_descriptor(replay,target,binding,download,**options))
    assert b'secret' not in raw and b'x'*128 not in raw
    pin=sha256(raw).hexdigest()
    if mode=='database':replay=replace(replay,database='other')
    if mode=='build':replay=replace(replay,build_sha256='e'*64)
    if mode=='source':binding=replace(binding,source_sha256='f'*64)
    if mode=='width':options['source_width']=33
    if mode=='icc':options['srgb_profile']=b'y'*128
    if mode=='key':target=replace(target,idempotency_key='other')
    if mode=='params':target=replace(target,params_raw=canonical_bytes({'strength':0}))
    if mode=='origin':download=replace(download,object_origin='https://other.test')
    if mode=='alpha_dtype':options['alpha_dtype']='float32'
    if mode=='timeout':options['maximum_seconds']=31
    calls=[]
    monkeypatch.setattr(m,'verify_preview_icc',lambda *a,**k:None)
    monkeypatch.setattr(m,'verify_fixture_preview_source',lambda *a,**k:NS(width=32,height=16))
    monkeypatch.setattr(m,'run_fixture_preview',lambda *a,**k:calls.append(k['expected_database']) or 'complete')
    def invoke():return m.run_pinned_preview(replay,pin,None,None,[],target,binding,download,**options,
        tls_context=None,icc_registry=object.__new__(m.ICCRegistry),
        assert_installed_fixture=lambda:calls.append('scope') or (False if mode=='fixture_false' else None),
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert invoke()=='complete' and calls==['scope','scope','restore']
    else:
        with pytest.raises((ValueError,RuntimeError),match='FIXTURE_PIN' if mode not in ('fixture_false','activity_false') else 'RESTORE_'):invoke()
        assert calls==(['scope'] if mode=='fixture_false' else [])
