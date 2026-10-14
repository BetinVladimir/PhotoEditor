from hashlib import sha256
from pathlib import Path
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.export import EncodedImage
from photo_worker.contracts.metadata import MetadataDocument,GPSMetadata
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.delivery_metadata_freeze import freeze_delivery_metadata
from photo_worker.v2.delivery_metadata_write import delivery_metadata_write_args


def ref():return ArtifactRef(artifact_id=uuid4(),sha256='a'*64)


@pytest.mark.parametrize('policy,fields,copied',[
    ('copyright',(),('author','copyright')),
    ('approved_exif',('camera_model',),('camera_model',)),
    ('approved_exif',('gps',),('gps',)),
    ('approved_exif',(),()),
])
def test_delivery_writer_uses_only_frozen_projection(policy,fields,copied):
    binding=VersionBinding(asset_version_id=uuid4(),source=ref(),canvas_hash='b'*64)
    source=canonical_bytes(MetadataDocument(author='Author',copyright='Copyright',camera_model='Camera',
        gps=GPSMetadata(latitude=-42.5,longitude=-23.25,altitude_m=-20)))
    metadata_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(source).hexdigest())
    raw=freeze_delivery_metadata(binding,metadata_ref,source,policy,approved_fields=fields)
    encoded=EncodedImage(artifact=ref(),format='jpeg',width=4,height=3,icc_id='srgb_v1',icc_sha256='c'*64,alpha=False,metadata_policy='strip')
    args,actual=delivery_metadata_write_args('/qualified/exiftool',Path('/scratch/output.jpg'),ref(),encoded,
        raw,sha256(raw).hexdigest(),binding,metadata_ref,source,policy=policy,approved_fields=fields)
    assert actual==copied
    assert ('-Artist=Author' in args)==('author' in copied)
    assert ('-Model=Camera' in args)==('camera_model' in copied)
    assert any(value.startswith('-GPSLatitude#=') for value in args)==('gps' in copied)
    if 'gps' in copied:assert '-GPSLatitudeRef=S' in args and '-GPSAltitudeRef#=1' in args
    assert args[-2:]==['--','/scratch/output.jpg']
    with pytest.raises(ValueError):
        delivery_metadata_write_args('/qualified/exiftool',Path('/scratch/output.jpg'),ref(),encoded,
            raw,sha256(raw).hexdigest(),binding,metadata_ref,source,policy='strip')
