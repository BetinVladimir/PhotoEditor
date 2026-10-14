from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.images import TransformChain
from photo_worker.operations.semantic.exposure_source import bind_local_exposure_source
from test_v27_approved_edit_binding import documents


def source_fixture():
    edit=documents()[0]
    image=dict(pixels=dict(artifact_id=str(uuid4()),sha256='c'*64),canvas=dict(width=2,height=1,orientation=1,space_id='working',source_sha256='d'*64),color=dict(profile_id='work_rec2020_gamma24_v1',encoding='gamma24',icc_sha256='e'*64),dtype='float32',alpha=None,transform_chain=TransformChain.create([]).model_dump(mode='json'))
    raw=canonical_bytes(image)
    edit['source']['source']['sha256']=sha256(raw).hexdigest()
    edit['source']['canvas_hash']=canonical_hash(image['canvas'])
    return edit,image,raw


def test_pinned_source_owns_exact_document_and_preserves_icc_identity():
    edit,image,raw=source_fixture();buffer=bytearray(raw)
    result=bind_local_exposure_source(edit,buffer)
    buffer[:]=b'{}';image['color']['icc_sha256']='0'*64
    assert result.color.icc_sha256=='e'*64 and result.canvas.width==2


def test_source_byte_hash_and_canvas_substitution_denied():
    edit,image,raw=source_fixture()
    with pytest.raises(ValueError,match='SOURCE_HASH'):bind_local_exposure_source(edit,raw+b' ')
    edit['source']['canvas_hash']='0'*64
    with pytest.raises(ValueError,match='SOURCE_CHANGED'):bind_local_exposure_source(edit,raw)


@pytest.mark.parametrize('field,value',[('dtype','uint16'),('color',dict(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='e'*64))])
def test_unqualified_source_representation_denied(field,value):
    edit,image,_=source_fixture();image[field]=value;raw=canonical_bytes(image)
    edit['source']['source']['sha256']=sha256(raw).hexdigest()
    with pytest.raises(ValueError,match='COLOR_UNSUPPORTED'):bind_local_exposure_source(edit,raw)


@pytest.mark.parametrize('field',['pixels','alpha'])
def test_zero_array_artifact_identity_is_denied(field):
    edit,image,_=source_fixture()
    image[field]=dict(artifact_id='00000000-0000-0000-0000-000000000000',sha256='c'*64)
    raw=canonical_bytes(image);edit['source']['source']['sha256']=sha256(raw).hexdigest()
    with pytest.raises(ValueError,match='SOURCE_INVALID'):
        bind_local_exposure_source(edit,raw)
