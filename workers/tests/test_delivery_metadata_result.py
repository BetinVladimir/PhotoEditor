from hashlib import sha256
from uuid import uuid4,uuid5
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.export import EncodedImage
from photo_worker.contracts.metadata import MetadataDocument,MetadataParams,MetadataReport
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.delivery_metadata_freeze import freeze_delivery_metadata
from photo_worker.v2.delivery_metadata_result import validate_delivery_metadata_result


def ref():return ArtifactRef(artifact_id=uuid4(),sha256='a'*64)


@pytest.mark.parametrize('mode',['valid','params','policy','report','duplicate-fields','projection','dimensions','alias'])
def test_metadata_result_exact_frozen_authority(mode):
    task=uuid4();encoded=ref()
    binding=VersionBinding(asset_version_id=uuid4(),source=ref(),canvas_hash='b'*64)
    source=canonical_bytes(MetadataDocument(author='Author',camera_model='Camera'))
    metadata=ArtifactRef(artifact_id=uuid4(),sha256=sha256(source).hexdigest())
    raw=freeze_delivery_metadata(binding,metadata,source,'copyright')
    projection=canonical_bytes(MetadataDocument(author='Author'))
    projected=ArtifactRef(artifact_id=uuid5(task,'delivery:metadata:projection:1'),sha256=sha256(projection).hexdigest())
    params=MetadataParams(encoded=encoded,document=projected,policy='copyright_only')
    before=EncodedImage(artifact=ref(),format='jpeg',width=4,height=3,icc_id='srgb_v1',icc_sha256='c'*64,alpha=False,metadata_policy='strip')
    encoded=encoded.model_copy(update={'sha256':sha256(canonical_bytes(before)).hexdigest()})
    params=params.model_copy(update={'encoded':encoded})
    after=before.model_copy(update={'artifact':ref(),'metadata_policy':'copyright_only'})
    report=MetadataReport(encoded=encoded,document=projected,policy='copyright_only',copied_fields=['author'],orientation=1,stale_previews_removed=True,icc_preserved=True)
    if mode=='params':params=params.model_copy(update={'encoded':ref()})
    if mode=='policy':params=params.model_copy(update={'policy':'safe_camera'})
    if mode=='report':report=report.model_copy(update={'document':ref()})
    if mode=='duplicate-fields':report=report.model_copy(update={'copied_fields':['author','author']})
    if mode=='projection':params=params.model_copy(update={'document':ref()})
    if mode=='dimensions':after=after.model_copy(update={'width':5})
    if mode=='alias':after=after.model_copy(update={'artifact':before.artifact})
    def run():return validate_delivery_metadata_result(task,encoded,params,before,after,report,raw,sha256(raw).hexdigest(),binding,metadata,source,policy='copyright')
    if mode=='valid':assert run().projection_jcs==projection
    else:
        with pytest.raises(ValueError):run()
