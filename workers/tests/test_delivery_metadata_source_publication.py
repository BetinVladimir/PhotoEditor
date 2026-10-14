from dataclasses import replace
from hashlib import sha256
import json
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.metadata import MetadataDocument
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.adoption import ArtifactRef as EvidenceRef
from photo_worker.v2.delivery_metadata_original_reader import MetadataOriginalScope
from photo_worker.v2.delivery_metadata_original_service import PreparedAuthorizedMetadataOriginal
from photo_worker.v2.delivery_metadata_source import PreparedMetadataSource
from photo_worker.v2.delivery_metadata_prepare import PreparedDeliveryMetadata
from photo_worker.v2.delivery_metadata_freeze import freeze_delivery_metadata
from photo_worker.v2.delivery_metadata_source_publication import compile_metadata_source_publication


def test_metadata_source_publication_external_binding_and_approval():
    original=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    source_ref=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    binding=VersionBinding(asset_version_id=uuid4(),source=source_ref,canvas_hash='c'*64)
    scope=MetadataOriginalScope(uuid4(),uuid4(),binding.asset_version_id,EvidenceRef(source_ref.artifact_id,source_ref.sha256),uuid4(),uuid4(),EvidenceRef(original.artifact_id,original.sha256))
    source=canonical_bytes(MetadataDocument(author='Artist'))
    ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(source).hexdigest())
    frozen=freeze_delivery_metadata(binding,ref,source,'copyright')
    prepared=PreparedAuthorizedMetadataOriginal(scope,canonical_bytes(binding),PreparedDeliveryMetadata(
        PreparedMetadataSource(canonical_bytes(original),100,source,ref.sha256),canonical_bytes(ref),frozen,sha256(frozen).hexdigest()))
    raw,pin=compile_metadata_source_publication(prepared,'image/jpeg','copyright')
    envelope=json.loads(raw)
    assert sha256(raw).hexdigest()==pin and canonical_bytes(envelope)==raw
    assert len(envelope)==8 and len(envelope['scope'])==8
    assert envelope['scope']['binding']==binding.model_dump(mode='json')
    assert envelope['approved_fields']==[]
    for mode in ('version','original','size','metadata','policy','approval','mime'):
        current=prepared;policy='copyright';approved=();mime='image/jpeg'
        if mode=='version':current=replace(prepared,scope=replace(scope,version=uuid4()))
        elif mode=='original':current=replace(prepared,scope=replace(scope,original=EvidenceRef(uuid4(),'a'*64)))
        elif mode=='size':current=replace(prepared,prepared=replace(prepared.prepared,source=replace(prepared.prepared.source,original_bytes=2*1024**3+1)))
        elif mode=='metadata':current=replace(prepared,prepared=replace(prepared.prepared,source=replace(prepared.prepared.source,metadata_jcs=b'{}')))
        elif mode=='policy':policy='approved_exif'
        elif mode=='approval':approved=('gps',)
        elif mode=='mime':mime='image/raw'
        with pytest.raises(ValueError):compile_metadata_source_publication(current,mime,policy,approved_fields=approved)
