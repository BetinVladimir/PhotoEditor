from dataclasses import replace
from types import SimpleNamespace as NS
from uuid import uuid4
import ssl
import pytest
from photo_worker.runtime import restore_volume_delivery as m
from photo_worker.runtime.restore_inventory import RestoreObject


@pytest.mark.parametrize('mode',['valid','changed_before','changed_after','redeem','consumed','invalid_origin','timeout','inactive','fixture_denied'])
def test_volume_delivery_reobserves_authority_and_consumed_intent(monkeypatch,mode):
    job,artifact,actor=uuid4(),uuid4(),uuid4()
    target=m.RestoreDownloadIntentTarget('https://restore.test',uuid4(),job,1,'fresh','Bearer secret')
    download=m.RestorePreviewDownloadTarget('https://foreign.test' if mode=='invalid_origin' else target.origin,
        'https://objects.test',target.authorization)
    obj=RestoreObject('photo','tenant/file','v1','a'*64,12)
    encoded=m.RestoreEncodedExport(NS(download_job_id=job),artifact,obj,'image/jpeg','image.jpg',1,'single')
    volume=NS(artifact_id=artifact,object=obj);registration=NS(intent_id=uuid4())
    intent=NS(ticket=object(),registration=registration);evidence=object();calls=[]
    def read(*args,**kwargs):
        calls.append('read');kwargs['check_active']()
        count=calls.count('read')
        if (mode=='changed_before' and count==2) or (mode=='changed_after' and count==3):return object()
        return volume
    def create(*args,**kwargs):calls.append('create');return intent
    def redeem(*args,**kwargs):
        calls.append('redeem')
        if mode=='redeem':raise ValueError('failed')
        return evidence
    def observe(*args,**kwargs):
        calls.append('consumed');assert kwargs['consumed'] is True
        if mode=='consumed':raise ValueError('not consumed')
        return registration
    monkeypatch.setattr(m,'read_fixture_export_volume',read)
    monkeypatch.setattr(m,'create_fixture_owner_intent',create)
    monkeypatch.setattr(m,'verify_fixture_intent_delivery',redeem)
    monkeypatch.setattr(m,'observe_fixture_owner_intent',observe)
    if mode=='timeout':
        times=iter((0,61));monkeypatch.setattr(m,'monotonic',lambda:next(times))
    def invoke():return m.run_fixture_volume_delivery(object(),target,download,actor,encoded,
        expected_database='restore',pepper=b'p'*32,tls_context=ssl.create_default_context(),maximum_seconds=60,
        assert_installed_fixture=lambda:False if mode=='fixture_denied' else None,
        check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':
        result=invoke();assert result.download is evidence and result.registration is registration
        assert calls==['read','create','read','redeem','consumed','read']
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
        if mode in ('invalid_origin','timeout','inactive','fixture_denied'):assert not calls
        if mode=='changed_before':assert 'redeem' not in calls
        if mode=='redeem':assert 'consumed' not in calls
