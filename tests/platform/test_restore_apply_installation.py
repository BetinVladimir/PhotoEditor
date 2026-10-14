from dataclasses import replace
from datetime import datetime,timezone
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_apply_installation as m


@pytest.mark.parametrize('mode',['valid','database','build','source','revision','style','item','key','timeout','dtype','storage','fixture_false','activity_false'])
def test_apply_independent_fixture_pin_refuses_substitution_before_io(monkeypatch,mode):
    replay=m.VerifiedRestoreReplay(uuid4(),'restore','a'*64,'b'*64,'c'*64,1,datetime.now(timezone.utc))
    target=m.RestoreLegacyApplyTarget('https://api.test',uuid4(),uuid4(),uuid4(),uuid4(),7,'secret-key','Bearer secret-token')
    binding=m.RestoreLegacyApplyBinding(uuid4(),uuid4(),uuid4(),uuid4(),'d'*64)
    download=m.RestorePreviewDownloadTarget(target.origin,'https://objects.test',target.authorization)
    options={'maximum_seconds':30,'alpha_dtype':'uint16'}
    raw=canonical_bytes(m.apply_fixture_descriptor(replay,target,binding,download,**options));pin=sha256(raw).hexdigest()
    assert b'secret' not in raw
    if mode=='database':replay=replace(replay,database='other')
    if mode=='build':replay=replace(replay,build_sha256='e'*64)
    if mode=='source':binding=replace(binding,source_sha256='f'*64)
    if mode=='revision':target=replace(target,source_revision=8)
    if mode=='style':target=replace(target,style_version_id=uuid4())
    if mode=='item':target=replace(target,item_id=uuid4())
    if mode=='key':target=replace(target,idempotency_key='other')
    if mode=='timeout':options['maximum_seconds']=31
    if mode=='storage':download=replace(download,object_origin='https://other.test')
    if mode=='dtype':options['alpha_dtype']='float32'
    calls=[]
    monkeypatch.setattr(m,'run_fixture_legacy_apply',lambda *a,**k:calls.append(k['expected_database']) or 'complete')
    def invoke():return m.run_pinned_legacy_apply(replay,pin,None,None,[],target,binding,download,**options,
        tls_context=None,
        assert_installed_fixture=lambda:calls.append('scope') or (False if mode=='fixture_false' else None),
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert invoke()=='complete' and calls==['scope','restore']
    else:
        with pytest.raises((ValueError,RuntimeError),match='FIXTURE_PIN' if mode not in ('fixture_false','activity_false') else 'RESTORE_'):invoke()
        assert calls==(['scope'] if mode=='fixture_false' else [])
