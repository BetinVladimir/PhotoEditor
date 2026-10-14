from io import BytesIO
from hashlib import sha256
from uuid import uuid4
from PIL import Image
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.operations.media.derivative import media_profile_hash
from photo_worker.runtime.restore_render_preview_bytes import verify_render_preview_bytes


@pytest.mark.parametrize('mode',['valid','foreign_source','wrong_manifest','changed_image',
                                 'wrong_dimensions','wrong_profile','extra_api_field',
                                 'activity_false','post_manifest_false'])
def test_render_review_preview_binds_actual_manifest_and_pixels(mode):
    review=uuid4();source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    profile=b'P'*128
    image=Image.new('RGB',(100,80),(32,72,120));stream=BytesIO()
    image.save(stream,format='JPEG',icc_profile=profile)
    encoded=stream.getvalue()
    if mode=='changed_image':encoded=encoded[:-1]+b'0'
    image_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(stream.getvalue()).hexdigest())
    profile_hash=media_profile_hash('preview1600',sha256(profile).hexdigest())
    manifest={'source':{'artifact_id':str(source.artifact_id),'sha256':source.sha256},
        'derivative':{'artifact_id':str(image_ref.artifact_id),'sha256':image_ref.sha256},
        'variant':'preview1600','profile_hash':profile_hash,'width':100,'height':80,
        'media_type':'image/jpeg','color_profile':'srgb','metadata_policy':'stripped','alpha':False}
    if mode=='wrong_manifest':manifest['metadata_policy']='preserved'
    manifest_raw=canonical_bytes(manifest)
    manifest_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(manifest_raw).hexdigest())
    def link(ref,raw,media):return {'artifact':{'artifact_id':str(ref.artifact_id),'sha256':ref.sha256},
        'bytes':len(raw),'media_type':media,'url':'https://objects.test/signed',
        'expires_at':'2027-01-01T00:00:00Z'}
    api={'review_id':str(review),'side':'after',
        'source':{'artifact_id':str(source.artifact_id),'sha256':source.sha256},
        'profile_hash':profile_hash,'width':100,'height':80,
        'manifest':link(manifest_ref,manifest_raw,'application/json'),
        'image':link(image_ref,stream.getvalue(),'image/jpeg')}
    if mode=='foreign_source':api['source']['artifact_id']=str(uuid4())
    if mode=='wrong_dimensions':api['width']=101
    if mode=='wrong_profile':api['profile_hash']='f'*64
    if mode=='extra_api_field':api['extra']=True
    checks=[]
    def active():
        checks.append(True)
        return False if mode=='activity_false' or mode=='post_manifest_false' and len(checks)>=2 else None
    def run():return verify_render_preview_bytes(canonical_bytes(api),manifest_raw,encoded,
        review_id=review,side='after',source=source,source_width=100,source_height=80,
        source_alpha=False,variant='preview1600',srgb_profile=profile,check_active=active)
    if mode=='valid':
        result=run();assert result.image==image_ref and result.manifest==manifest_ref
        assert (result.width,result.height)==(100,80)
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode=='post_manifest_false':assert len(checks)==2
