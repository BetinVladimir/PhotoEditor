import hashlib
import json
from pathlib import Path
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.portrait_open_eyes import PortraitOpenEyesParams
from photo_worker.v2.portrait_donor_evidence import verify_open_eyes_faces


def evidence():
    path=Path(__file__).resolve().parents[2]/'backend/internal/v2portrait/testdata/open-eyes-params-parity.json'
    p=json.loads(path.read_text())['params']
    uid=lambda n:f'00000000-0000-0000-0000-{n:012d}'
    proof={'schema_version':1,'person_id':uid(20),'source':p['base'],'donor':p['donor'],
           'source_face_id':p['source_face_id'],'donor_face_id':p['donor_face_id'],
           'source_observation_id':uid(21),'donor_observation_id':uid(22),
           'consent_id':uid(23),'epoch':'1','actor_id':uid(24)}
    docs=[]
    for prefix,binding in [('source',p['base']),('donor',p['donor'])]:
        face={'observation_id':proof[prefix+'_observation_id'],'face_id':p[prefix+'_face_id'],
              'box':{'x0':.1,'y0':.1,'x1':.9,'y1':.9},'landmarks':[{'x':.5,'y':.5} for _ in range(68)],
              'embedding':None,'quality':1,'person_id':None}
        doc={'schema_version':1,'binding':binding,'faces':[face]}
        raw=canonical_bytes(doc);p[prefix+'_faces']['sha256']=hashlib.sha256(raw).hexdigest();docs.append(doc)
    raw=canonical_bytes(proof);p['confirmation']['sha256']=hashlib.sha256(raw).hexdigest()
    return p,raw,docs


def test_selected_faces_bind_observations_and_pixel_geometry():
    p,proof,docs=evidence()
    result=verify_open_eyes_faces(PortraitOpenEyesParams.model_validate_json(json.dumps(p)),proof,
                                 *[canonical_bytes(doc) for doc in docs],(100,100),(200,200),40)
    assert result.source_landmarks[0,0]==50 and result.donor_landmarks[0,0]==100
    assert not result.source_landmarks.flags.writeable


@pytest.mark.parametrize('case',['duplicate','roi','size','forged_person'])
def test_invalid_face_evidence_is_rejected(case):
    p,proof,docs=evidence();face=docs[0]['faces'][0]
    if case=='duplicate':docs[0]['faces'].append(face.copy())
    elif case=='roi':face['landmarks'][0]['x']=.95
    elif case=='size':face['box']['x1']=.11
    else:face['person_id']='00000000-0000-0000-0000-000000000020'
    source=canonical_bytes(docs[0]);p['source_faces']['sha256']=hashlib.sha256(source).hexdigest()
    with pytest.raises(ValueError):
        verify_open_eyes_faces(PortraitOpenEyesParams.model_validate_json(json.dumps(p)),proof,source,canonical_bytes(docs[1]),(100,100),(200,200),40)
