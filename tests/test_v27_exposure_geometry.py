from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.semantic.exposure_geometry import bind_local_exposure_geometry
from test_v27_exposure_source import source_fixture


def geometry_fixture(change=None):
    edit,image,source=source_fixture()
    image['canvas'].update(width=8,height=8)
    source=canonical_bytes(image)
    edit['source']['source']['sha256']=sha256(source).hexdigest()
    from photo_worker.contracts.base import canonical_hash
    edit['source']['canvas_hash']=canonical_hash(image['canvas'])
    mask=dict(artifact=dict(artifact_id=str(uuid4()),sha256='f'*64),canvas=image['canvas'],transform_chain=image['transform_chain'],object_id=None,selector='manual',confidence=.9,model_lock_id='a'*64,protected=False)
    transform=dict(schema_version=1,from_canvas_hash=edit['source']['canvas_hash'],to_canvas_hash=edit['source']['canvas_hash'],matrix=[1.,0.,0.,0.,1.,0.,0.,0.,1.],kind='homography')
    if change:change(mask,transform)
    mask_raw=canonical_bytes(mask);transform_raw=canonical_bytes(transform)
    edit['mask']['sha256']=sha256(mask_raw).hexdigest();edit['transform']['sha256']=sha256(transform_raw).hexdigest()
    candidate=dict(schema_version=1,target=edit['source'],mask=edit['mask'],transform=edit['transform'],confidence=.9,state='proposed',reason=None)
    intent=dict(schema_version=1,kind='local_exposure',reference=edit['source'],reference_mask=edit['mask'],target_snapshot_id=str(uuid4()),strength=edit['strength'],ev=edit['ev'],prompt=edit['prompt'],preserve_persons=True)
    candidate_raw=canonical_bytes(candidate);intent_raw=canonical_bytes(intent)
    edit['candidate']['sha256']=sha256(candidate_raw).hexdigest();edit['intent']['sha256']=sha256(intent_raw).hexdigest()
    return edit,candidate_raw,intent_raw,source,mask_raw,transform_raw,source


def test_transferred_geometry_remains_on_exact_source_canvas():
    image,mask,transform=bind_local_exposure_geometry(*geometry_fixture())
    assert image.canvas==mask.canvas and transform.to_canvas_hash==transform.from_canvas_hash


@pytest.mark.parametrize('change',[
    lambda m,t:m['artifact'].update(artifact_id='00000000-0000-0000-0000-000000000000'),
    lambda m,t:m.update(protected=True),lambda m,t:m.update(selector='person'),
    lambda m,t:m.update(model_lock_id=None),lambda m,t:m.update(confidence=.8),
    lambda m,t:t.update(to_canvas_hash='0'*64),lambda m,t:t.update(from_canvas_hash='0'*64),
    lambda m,t:t.update(kind='similarity'),
])
def test_geometry_substitution_is_denied_even_with_matching_document_hashes(change):
    with pytest.raises(ValueError,match='GEOMETRY_CHANGED'):
        bind_local_exposure_geometry(*geometry_fixture(change))


@pytest.mark.parametrize('index',[4,5])
def test_original_geometry_bytes_are_pinned(index):
    values=list(geometry_fixture());values[index]+=b' '
    with pytest.raises(ValueError,match='GEOMETRY_HASH'):bind_local_exposure_geometry(*values)


@pytest.mark.parametrize('matrix',[
    [0.,0.,0.,0.,0.,0.,0.,0.,1.],
    [-1.,0.,7.,0.,1.,0.,0.,0.,1.],
    [1.,0.,0.,0.,1.,0.,-.2,0.,1.],
])
def test_singular_reflected_and_horizon_crossing_geometry_is_denied(matrix):
    with pytest.raises(ValueError,match='TRANSFORM_UNSAFE'):
        bind_local_exposure_geometry(*geometry_fixture(lambda m,t:t.update(matrix=matrix)))


def test_reference_document_is_required_and_original_byte_pinned():
    values=list(geometry_fixture());values[6]+=b' '
    with pytest.raises(ValueError,match='SOURCE_HASH'):
        bind_local_exposure_geometry(*values)


def independent_geometry_fixture(matrix=None):
    from copy import deepcopy
    from photo_worker.contracts.base import canonical_hash,parse_json
    values=list(geometry_fixture())
    edit=values[0]
    reference=deepcopy(parse_json(values[3]))
    reference['canvas'].update(width=16,height=24,space_id='reference',source_sha256='1'*64)
    reference['pixels']=dict(artifact_id=str(uuid4()),sha256='2'*64)
    reference_raw=canonical_bytes(reference)
    binding=dict(asset_version_id=str(uuid4()),source=dict(artifact_id=str(uuid4()),sha256=sha256(reference_raw).hexdigest()),canvas_hash=canonical_hash(reference['canvas']))
    intent=parse_json(values[2]);intent['reference']=binding
    intent['reference_mask']=dict(artifact_id=str(uuid4()),sha256='3'*64)
    values[2]=canonical_bytes(intent);edit['intent']['sha256']=sha256(values[2]).hexdigest()
    transform=parse_json(values[5]);transform['from_canvas_hash']=binding['canvas_hash']
    if matrix is not None:transform['matrix']=matrix
    values[5]=canonical_bytes(transform);edit['transform']['sha256']=sha256(values[5]).hexdigest()
    candidate=parse_json(values[1]);candidate['transform']=edit['transform']
    values[1]=canonical_bytes(candidate);edit['candidate']['sha256']=sha256(values[1]).hexdigest()
    values[6]=reference_raw
    return values


def test_reference_and_target_are_independent_pinned_images():
    image,mask,transform=bind_local_exposure_geometry(*independent_geometry_fixture())
    assert image.canvas.width==8 and image.canvas.height==8
    assert mask.canvas==image.canvas
    assert transform.from_canvas_hash!=transform.to_canvas_hash


def test_horizon_is_checked_over_reference_not_smaller_target():
    # Denominator remains positive over the8px target but crosses the16px reference.
    values=independent_geometry_fixture([1.,0.,0.,0.,1.,0.,-.1,0.,1.])
    with pytest.raises(ValueError,match='TRANSFORM_UNSAFE'):
        bind_local_exposure_geometry(*values)


def test_reference_and_target_document_swap_fails_exact_byte_pins():
    values=independent_geometry_fixture()
    values[3],values[6]=values[6],values[3]
    with pytest.raises(ValueError,match='SOURCE_HASH'):
        bind_local_exposure_geometry(*values)
