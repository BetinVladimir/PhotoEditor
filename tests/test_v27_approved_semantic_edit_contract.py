import json
from uuid import uuid4
import pytest
from pydantic import ValidationError
from photo_worker.contracts.semantic import ApprovedSemanticEdit


def fixture(kind='remove_object'):
    def ref():return {'artifact_id':str(uuid4()),'sha256':'a'*64}
    return {'schema_version':1,'target_id':str(uuid4()),
            'source':{'asset_version_id':str(uuid4()),'source':ref(),'canvas_hash':'b'*64},
            'intent':ref(),'candidate':ref(),'mask':ref(),'transform':ref(),'kind':kind,'strength':.5,
            'ev':.25 if kind=='local_exposure' else None,'prompt':None,'preserve_persons':True}


@pytest.mark.parametrize('kind',['remove_object','local_exposure','background_cleanup','retouch_region'])
def test_worker_receives_exact_semantic_kind_and_mask_pins(kind):
    body=fixture(kind)
    edit=ApprovedSemanticEdit.model_validate_json(json.dumps(body))
    assert edit.kind==kind and str(edit.mask.artifact_id)==body['mask']['artifact_id']
    assert edit.model_dump(mode='json')==body


@pytest.mark.parametrize('field,value',[
    ('schema_version',True),('schema_version',2),('target_id','00000000-0000-0000-0000-000000000000'),
    ('preserve_persons',False),('preserve_persons',1),('strength',True),('strength',1.1),
    ('ev',.5),('kind','unknown'),('prompt',''),('extra','untrusted'),
])
def test_invalid_downstream_compilation_input_is_denied(field,value):
    body=fixture();body[field]=value
    with pytest.raises(ValidationError):ApprovedSemanticEdit.model_validate_json(json.dumps(body))


def test_local_exposure_requires_ev_and_every_ref_requires_nonzero_id():
    body=fixture('local_exposure');body['ev']=None
    with pytest.raises(ValidationError):ApprovedSemanticEdit.model_validate_json(json.dumps(body))
    for field in ('intent','candidate','mask','transform'):
        body=fixture();body[field]['artifact_id']='00000000-0000-0000-0000-000000000000'
        with pytest.raises(ValidationError):ApprovedSemanticEdit.model_validate_json(json.dumps(body))
