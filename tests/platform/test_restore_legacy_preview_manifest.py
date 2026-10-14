from types import SimpleNamespace as NS
from uuid import uuid4
from hashlib import sha256
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.media.derivative import media_profile_hash
from photo_worker.runtime import restore_legacy_preview_manifest as m


@pytest.mark.parametrize('mode',['valid','source','derivative','variant','geometry','profile','alpha','media','hash','noncanonical','roi','activity_false','late_activity_false'])
def test_preview_real_manifest_requires_independent_geometry_profile_and_exact_refs(mode):
    source,derivative=uuid4(),uuid4()
    target=NS(scale='proxy',roi=(3900,0,200,10) if mode=='roi' else None)
    binding=NS(source_artifact_id=source,source_sha256='a'*64)
    value={'source':{'artifact_id':str(uuid4() if mode=='source' else source),'sha256':'a'*64},
        'derivative':{'artifact_id':str(uuid4() if mode=='derivative' else derivative),'sha256':'b'*64},
        'variant':'preview2560' if mode=='variant' else 'preview1600',
        'profile_hash':'e'*64 if mode=='profile' else media_profile_hash('preview1600','c'*64),
        'width':1599 if mode=='geometry' else 1600,'height':800,
        'media_type':'image/webp' if mode=='media' else 'image/jpeg','color_profile':'srgb',
        'metadata_policy':'stripped','alpha':mode=='alpha'}
    raw=canonical_bytes(value)
    if mode=='noncanonical':raw=b' '+raw
    result=NS(derivative=NS(artifact_id=derivative,object=NS(sha256='b'*64)),
        manifest=NS(object=NS(sha256='f'*64 if mode=='hash' else sha256(raw).hexdigest(),size=len(raw))))
    checks=[]
    def active():
        checks.append(True)
        return False if mode=='activity_false' or mode=='late_activity_false' and len(checks)>=2 else None
    def invoke():return m.validate_legacy_preview_manifest(raw,target,binding,result,source_width=4000,
        source_height=2000,srgb_sha256='c'*64,check_active=active)
    if mode=='valid':assert invoke().width==1600
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
