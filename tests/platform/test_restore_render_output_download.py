from datetime import datetime,timezone,timedelta
from hashlib import sha256
from types import SimpleNamespace as NS
from urllib.parse import urlsplit
from uuid import uuid4,uuid5

import pytest

from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.runtime.restore_batch_adoption import RestoreBatchAdoptionEvidence
from photo_worker.runtime.restore_inventory import RestoreObject
from photo_worker.runtime.restore_preview_download import RestorePreviewDownloadTarget
from photo_worker.runtime.restore_render_preview_catalogue import RestoreRenderPreviewObjects
from photo_worker.runtime.restore_render_review import RestoreRenderReviewEvidence
from photo_worker.runtime.restore_render_terminal import RestoreRenderTerminalEvidence
from photo_worker.runtime import restore_render_output_download as m


@pytest.mark.parametrize('mode',['valid','changed_catalogue','changed_terminal',
                                 'wrong_version','wrong_sha','duplicate_output','foreign_origin','fixture_false','activity_false'])
def test_completed_render_outputs_are_exact_downloaded_versions(monkeypatch,mode):
    tenant,actor,batch,child,workflow,review_id=(uuid4() for _ in range(6))
    task=uuid5(workflow,'render.compose:0')
    candidate=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    image=b'jpeg physical bytes';manifest=b'{}'
    image_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(image).hexdigest())
    manifest_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(manifest).hexdigest())
    image_obj=RestoreObject('photos','tenants/'+str(tenant)+'/image.jpg','i1',image_ref.sha256,len(image))
    manifest_obj=RestoreObject('photos','tenants/'+str(tenant)+'/manifest.json','m1',manifest_ref.sha256,len(manifest))
    preview=RestoreRenderPreviewObjects('isolated',review_id,candidate,'preview1600','d'*64,
        manifest_ref,manifest_obj,image_ref,image_obj,'image/jpeg')
    adoption=RestoreBatchAdoptionEvidence('restore-batch-adoption/v1','isolated',batch,child,workflow)
    review=RestoreRenderReviewEvidence('restore-render-review/v1','isolated',review_id,
        task,candidate,2,'b'*64,datetime.now(timezone.utc))
    terminal=RestoreRenderTerminalEvidence('restore-render-terminal/v1','isolated',batch,child,
        workflow,review_id,3,4)
    target=RestorePreviewDownloadTarget('https://api.test','https://objects.test','Bearer private')
    expires=(datetime.now(timezone.utc)+timedelta(minutes=5)).isoformat()
    def item(role,ref,obj,media):
        host='foreign.test' if mode=='foreign_origin' and role=='derivative' else 'objects.test'
        version='wrong' if mode=='wrong_version' and role=='derivative' else obj.version
        return {'artifact_id':str(ref.artifact_id),'role':role,'sha256':ref.sha256,
            'bytes':obj.size,'media_type':media,'url':f'https://{host}/{obj.bucket}/{obj.key}?versionId={version}',
            'expires_at':expires}
    items=[item('derivative_manifest',manifest_ref,manifest_obj,'application/json'),
           item('derivative',image_ref,image_obj,'image/jpeg')]
    if mode=='duplicate_output':items[1]=items[0]
    api=canonical_bytes({'items':items});requests=[];catalogue_reads=[0];terminal_reads=[0]
    class Response:
        status=200
        def __init__(self,body,media):self.parts=[body,b''];self.media=media
        def getheader(self,name,default=None):
            return self.media if name=='Content-Type' else default
        def read(self,_):return self.parts.pop(0)
    class HTTPS:
        def __init__(self,host,port,**kwargs):self.host=host
        def request(self,method,path,headers):
            requests.append((self.host,path,headers));self.path=path
        def getresponse(self):
            if self.host=='api.test':return Response(api,'application/json')
            if 'manifest.json' in self.path:return Response(manifest,'application/json')
            return Response(b'wrong physical bytes' if mode=='wrong_sha' else image,'image/jpeg')
        def close(self):pass
    monkeypatch.setattr(m.http.client,'HTTPSConnection',HTTPS)
    monkeypatch.setattr(m,'validate_preview_download_installation',lambda *a,**k:
        (urlsplit(target.api_origin),urlsplit(target.object_origin)))
    def catalogue(*args,**kwargs):
        catalogue_reads[0]+=1
        assert kwargs['accepted_revision']==2
        return NS() if mode=='changed_catalogue' and catalogue_reads[0]>1 else preview
    def complete(*args,**kwargs):
        terminal_reads[0]+=1
        return NS() if mode=='changed_terminal' and terminal_reads[0]>1 else terminal
    monkeypatch.setattr(m,'observe_fixture_after_preview_objects',catalogue)
    monkeypatch.setattr(m,'observe_fixture_render_terminal',complete)
    def run():return m.download_fixture_render_output(object(),target,tenant,actor,adoption,
        review,terminal,preview,tls_context=object(),
        assert_installed_fixture=lambda:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':
        result=run();assert result.child_run_id==child and result.derivative_sha256==image_ref.sha256
        assert catalogue_reads[0]==2 and terminal_reads[0]==2 and len(requests)==3
        assert 'Authorization' not in requests[1][2] and 'Authorization' not in requests[2][2]
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        if mode in ('wrong_version','foreign_origin','duplicate_output'):
            assert len(requests)==1
    if mode in ('fixture_false','activity_false'):
        assert requests==[] and catalogue_reads==[0] and terminal_reads==[0]
