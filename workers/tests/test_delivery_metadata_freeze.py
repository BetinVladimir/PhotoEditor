from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.metadata import MetadataDocument
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.delivery_metadata_freeze import freeze_delivery_metadata,verify_frozen_delivery_metadata


@pytest.mark.parametrize('mode',['valid','binding','source','projection','digest'])
def test_frozen_delivery_metadata_exact_source_scope_and_projection(mode):
    binding=VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),canvas_hash='b'*64)
    source=canonical_bytes(MetadataDocument(author='Author',camera_model='Model'))
    ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(source).hexdigest())
    raw=freeze_delivery_metadata(binding,ref,source,'approved_exif',approved_fields=('camera_model','author'))
    pin=sha256(raw).hexdigest()
    if mode=='binding':binding=binding.model_copy(update={'asset_version_id':uuid4()})
    if mode=='source':source=canonical_bytes(MetadataDocument(author='substitution'))
    if mode=='projection':
        from photo_worker.v2.delivery_metadata_freeze import FrozenDeliveryMetadata
        value=FrozenDeliveryMetadata.model_validate_json(raw);value.projection.author='substitution'
        raw=canonical_bytes(value);pin=sha256(raw).hexdigest()
    if mode=='digest':pin='0'*64
    if mode=='valid':
        value=verify_frozen_delivery_metadata(raw,pin,binding,ref,source,policy="approved_exif",approved_fields=("camera_model","author"))
        assert value.projection==MetadataDocument(author='Author',camera_model='Model')
        assert value.approved_fields==['author','camera_model']
    else:
        with pytest.raises(ValueError):verify_frozen_delivery_metadata(raw,pin,binding,ref,source,policy="approved_exif",approved_fields=("camera_model","author"))


def test_delivery_metadata_shared_wire_corpus():
    import json
    from pathlib import Path
    corpus=json.loads((Path(__file__).resolve().parents[2]/'docs/implementation-v2/contracts/delivery-metadata-crosslang.json').read_text())
    assert len(corpus['cases'])==4
    for case in corpus['cases']:
        binding=VersionBinding.model_validate_json(canonical_bytes(case['binding']));ref=ArtifactRef.model_validate_json(canonical_bytes(case['metadata_source']))
        source=case['source_jcs'].encode()
        raw=freeze_delivery_metadata(binding,ref,source,case['policy'],approved_fields=tuple(case['approved_fields']))
        assert raw==case['frozen_jcs'].encode()
        assert sha256(raw).hexdigest()==case['frozen_sha256']
        verify_frozen_delivery_metadata(raw,case['frozen_sha256'],binding,ref,source,policy=case['policy'],approved_fields=tuple(case['approved_fields']))


@pytest.mark.parametrize('receipt_policy,receipt_fields,task_policy,task_fields',[
    ('approved_exif',('gps',),'approved_exif',()),
    ('approved_exif',('author','camera_model'),'approved_exif',('author',)),
    ('approved_exif',('author',),'copyright',()),
    ('copyright',(),'strip',()),
    ('strip',(),'copyright',()),
])
def test_metadata_receipt_cannot_choose_task_permission(receipt_policy,receipt_fields,task_policy,task_fields):
    binding=VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),canvas_hash='b'*64)
    source=canonical_bytes(MetadataDocument(author='Author',camera_model='Model'))
    ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(source).hexdigest())
    raw=freeze_delivery_metadata(binding,ref,source,receipt_policy,approved_fields=receipt_fields)
    # Even a valid receipt SHA and identical projections cannot change approval.
    with pytest.raises(ValueError):
        verify_frozen_delivery_metadata(raw,sha256(raw).hexdigest(),binding,ref,source,
            policy=task_policy,approved_fields=task_fields)


def test_metadata_restore_owns_projection_and_approval():
    binding=VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),canvas_hash='b'*64)
    source=canonical_bytes(MetadataDocument(author='Author'))
    ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(source).hexdigest())
    raw=freeze_delivery_metadata(binding,ref,source,'approved_exif',approved_fields=('author',))
    pin=sha256(raw).hexdigest()
    first=verify_frozen_delivery_metadata(raw,pin,binding,ref,source,policy='approved_exif',approved_fields=('author',))
    first.projection.author='Changed';first.approved_fields.append('gps')
    second=verify_frozen_delivery_metadata(raw,pin,binding,ref,source,policy='approved_exif',approved_fields=('author',))
    assert second.projection.author=='Author' and second.approved_fields==['author']
