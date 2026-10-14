from hashlib import sha256
from uuid import uuid4
from PIL import Image
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.delivery_metadata_prepare import prepare_delivery_metadata
from photo_worker.v2.delivery_metadata_freeze import verify_frozen_delivery_metadata


@pytest.mark.parametrize('policy,fields',[('strip',()),('copyright',()),('approved_exif',('camera_model','author'))])
def test_prepare_original_and_frozen_metadata(tmp_path,policy,fields):
    path=(tmp_path/'original.jpg').resolve()
    exif=Image.Exif();exif[315]='Author';exif[272]='Camera';exif[305]='Private'
    Image.new('RGB',(4,3)).save(path,exif=exif)
    original=ArtifactRef(artifact_id=uuid4(),sha256=sha256(path.read_bytes()).hexdigest())
    binding=VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),canvas_hash='b'*64)
    result=prepare_delivery_metadata(path,original,'image/jpeg',path.stat().st_size,binding,uuid4(),policy,
        approved_fields=fields,max_bytes=100000)
    ref=ArtifactRef.model_validate_json(result.metadata_ref_jcs)
    frozen=verify_frozen_delivery_metadata(result.frozen_jcs,result.frozen_sha256,binding,ref,
        result.source.metadata_jcs,policy=policy,approved_fields=fields)
    assert frozen.projection.author==('Author' if policy!='strip' else None)
    assert frozen.projection.camera_model==('Camera' if policy=='approved_exif' else None)
    assert b'Private' not in result.source.metadata_jcs


@pytest.mark.parametrize('mode',['alias_original','alias_source','bad_fields','wrong_policy','cancel'])
def test_prepare_metadata_refuses_before_file_io(tmp_path,mode):
    original=ArtifactRef(artifact_id=uuid4(),sha256='c'*64)
    binding=VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),canvas_hash='b'*64)
    metadata_id=original.artifact_id if mode=='alias_original' else binding.source.artifact_id if mode=='alias_source' else uuid4()
    fields=('gps',) if mode=='wrong_policy' else ('unknown',) if mode=='bad_fields' else ()
    def active():
        if mode=='cancel':raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError if mode=='cancel' else ValueError):
        prepare_delivery_metadata(tmp_path/'missing',original,'image/jpeg',1,binding,metadata_id,'copyright',
            approved_fields=fields,max_bytes=100,check_active=active)
