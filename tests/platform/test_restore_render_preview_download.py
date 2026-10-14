from datetime import datetime,timezone,timedelta
from hashlib import sha256
from io import BytesIO
from types import SimpleNamespace as NS
from uuid import uuid4
import ssl
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.runtime.restore_inventory import RestoreObject
from photo_worker.runtime.restore_preview_download import RestorePreviewDownloadTarget
from photo_worker.runtime import restore_render_preview_download as m


@pytest.mark.parametrize('mode',['valid','foreign_version','foreign_host','expired',
                                 'changed_image','bad_api','fixture_false','activity_false'])
def test_render_preview_download_pins_both_signed_physical_versions(monkeypatch,mode):
    tenant,review=uuid4(),uuid4();source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    manifest_raw=b'{}';image_raw=b'JPEG'
    manifest_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(manifest_raw).hexdigest())
    image_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(image_raw).hexdigest())
    manifest_obj=RestoreObject('photos','manifest.json','v1',manifest_ref.sha256,len(manifest_raw))
    image_obj=RestoreObject('photos','image.jpg','v2',image_ref.sha256,len(image_raw))
    expiry=(datetime.now(timezone.utc)+timedelta(minutes=-1 if mode=='expired' else 5)).isoformat()
    def link(ref,obj,media):return {'artifact':{'artifact_id':str(ref.artifact_id),'sha256':ref.sha256},
        'bytes':obj.size,'media_type':media,'url':'https://'+(
            'foreign.test' if mode=='foreign_host' and media=='image/jpeg' else 'objects.test')+
            '/photos/'+obj.key+'?versionId='+(
            'other' if mode=='foreign_version' and media=='image/jpeg' else obj.version),
        'expires_at':expiry}
    api={'review_id':str(review),'side':'after',
         'source':{'artifact_id':str(source.artifact_id),'sha256':source.sha256},
         'profile_hash':'c'*64,'width':100,'height':80,
         'manifest':link(manifest_ref,manifest_obj,'application/json'),
         'image':link(image_ref,image_obj,'image/jpeg')}
    api_raw=b'bad' if mode=='bad_api' else canonical_bytes(api)
    requests=[];verified=[]
    class Connection:
        def __init__(self,host,port,*,context,timeout):self.host=host
        def request(self,method,path,headers):
            assert method=='GET';self.path=path;requests.append((self.host,path,headers))
        def getresponse(self):
            if self.host=='restore.test':raw,media=api_raw,'application/json'
            elif 'manifest.json' in self.path:raw,media=manifest_raw,'application/json'
            else:raw,media=(b'JPE0' if mode=='changed_image' else image_raw),'image/jpeg'
            stream=BytesIO(raw)
            return NS(status=200,read=stream.read,
                getheader=lambda name,default=None:media if name=='Content-Type' else default)
        def close(self):pass
    monkeypatch.setattr(m.http.client,'HTTPSConnection',Connection)
    monkeypatch.setattr(m,'verify_render_preview_bytes',lambda *a,**k:verified.append(True) or
        NS(manifest=manifest_ref,image=image_ref))
    target=RestorePreviewDownloadTarget('https://restore.test','https://objects.test','Bearer private')
    def run():return m.download_fixture_render_preview(target,tenant,review,'after','preview1600',
        source=source,source_width=100,source_height=80,source_alpha=False,
        manifest_ref=manifest_ref,manifest_object=manifest_obj,image_ref=image_ref,
        image_object=image_obj,srgb_profile=b'P'*128,tls_context=ssl.create_default_context(),
        assert_fixture_scope=lambda:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':
        result=run();assert result.manifest_bytes==2 and result.image_bytes==4
        assert len(requests)==3 and verified==[True]
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        if mode in ('foreign_version','foreign_host','expired','bad_api'):
            assert len(requests)==1 and verified==[]
    if mode in ('fixture_false','activity_false'):assert requests==[] and verified==[]
