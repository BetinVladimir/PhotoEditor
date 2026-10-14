from hashlib import sha256
from uuid import uuid4,uuid5
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.export import EncodedImage
from photo_worker.contracts.metadata import MetadataDocument,MetadataParams
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.delivery_metadata_reader import MetadataResultScope
from photo_worker.v2.delivery_metadata_freeze import freeze_delivery_metadata
from photo_worker.v2.delivery_metadata_adoption_input import MetadataWriteProof,restore_metadata_adoption_proof


def test_metadata_adoption_proof_recompiles_exact_plan():
    scope=MetadataResultScope(uuid4(),uuid4(),1,2,uuid4())
    def ref(raw=b'x'):return ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    binding=VersionBinding(asset_version_id=uuid4(),source=ref(),canvas_hash='a'*64)
    source=canonical_bytes(MetadataDocument(author='Artist'));metadata=ref(source)
    frozen=freeze_delivery_metadata(binding,metadata,source,'copyright')
    before=EncodedImage(artifact=ref(),format='jpeg',width=4,height=3,icc_id='srgb_v1',icc_sha256='b'*64,alpha=False,metadata_policy='strip')
    encoded=canonical_bytes(before);encoded_ref=ref(encoded)
    projection=canonical_bytes(MetadataDocument(author='Artist'))
    projected=ArtifactRef(artifact_id=uuid5(scope.task_id,'delivery:metadata:projection:1'),sha256=sha256(projection).hexdigest())
    params=canonical_bytes(MetadataParams(encoded=encoded_ref,document=projected,policy='copyright_only'))
    proof=MetadataWriteProof(schema_version=1,task_id=scope.task_id,encoded=encoded_ref,encoded_jcs=encoded.decode(),
        frozen_jcs=frozen.decode(),frozen_sha256=sha256(frozen).hexdigest(),binding=binding,metadata_source=metadata,
        metadata_jcs=source.decode(),policy='copyright',approved_fields=[],
        params=ArtifactRef(artifact_id=uuid5(scope.task_id,'delivery:metadata:params:1'),sha256=sha256(params).hexdigest()),
        params_jcs=params.decode(),projection=projected,projection_jcs=projection.decode())
    raw=canonical_bytes(proof)
    assert restore_metadata_adoption_proof(raw,sha256(raw).hexdigest(),scope)==proof
    for mode in ('task','params','projection','policy','source','approval','canonical'):
        changed=proof.model_copy(deep=True)
        if mode=='task':changed.task_id=uuid4()
        elif mode=='params':changed.params_jcs='{}'
        elif mode=='projection':changed.projection_jcs='{}'
        elif mode=='policy':changed.policy='approved_exif'
        elif mode=='source':changed.metadata_jcs='{}'
        elif mode=='approval':changed.approved_fields=['gps']
        body=canonical_bytes(changed)+(b' ' if mode=='canonical' else b'')
        with pytest.raises(AdoptionInvalid):restore_metadata_adoption_proof(body,sha256(body).hexdigest(),scope)
