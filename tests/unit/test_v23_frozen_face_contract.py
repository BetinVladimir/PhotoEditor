from uuid import uuid4, UUID
import pytest

from photo_worker.operations.session.contracts import FaceObservations


def body():
    return dict(schema_version=1,binding=dict(asset_version_id=uuid4(),
        source=dict(artifact_id=uuid4(),sha256='a'*64),canvas_hash='b'*64),
        faces=[dict(observation_id=uuid4(),face_id=uuid4(),
            box=dict(x0=.1,y0=.1,x1=.9,y1=.9),
            landmarks=[dict(x=.5,y=.5)]*68,embedding=None,quality=1,person_id=None)])


@pytest.mark.parametrize('mode',['valid','empty','confirmed','zero-observation','zero-face',
    'zero-person','embedding-only','person-only','duplicate-face','duplicate-observation',
    'zero-source','zero-version','flat-x','flat-y','reversed-x'])
def test_worker_face_contract_matches_frozen_backend_invariants(mode):
    value=body();face=value['faces'][0]
    if mode=='empty':value['faces']=[]
    if mode in ('confirmed','zero-person'):
        face['person_id']=UUID(int=0) if mode=='zero-person' else uuid4()
        face['embedding']=[1.]+[0.]*511
    if mode=='zero-observation':face['observation_id']=UUID(int=0)
    if mode=='zero-face':face['face_id']=UUID(int=0)
    if mode=='embedding-only':face['embedding']=[1.]+[0.]*511
    if mode=='person-only':face['person_id']=uuid4()
    if mode.startswith('duplicate-'):
        other={**face}
        if mode=='duplicate-face':other['observation_id']=uuid4()
        else:other['face_id']=uuid4()
        value['faces'].append(other)
    if mode=='zero-source':value['binding']['source']['artifact_id']=UUID(int=0)
    if mode=='zero-version':value['binding']['asset_version_id']=UUID(int=0)
    if mode=='flat-x':face['box']['x1']=face['box']['x0']
    if mode=='flat-y':face['box']['y1']=face['box']['y0']
    if mode=='reversed-x':face['box']['x0']=.95
    if mode in ('valid','empty','confirmed'):
        parsed=FaceObservations.model_validate(value)
        assert len(parsed.faces)==len(value['faces'])
    else:
        with pytest.raises(ValueError,match='INVALID_FACE_'):
            FaceObservations.model_validate(value)
