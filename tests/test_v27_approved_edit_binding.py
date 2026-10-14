from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.semantic.approved_binding import bind_approved_edit_documents
from test_v27_approved_semantic_edit_contract import fixture


def documents(ev=.25,strength=.5):
    edit=fixture('local_exposure');edit['ev']=ev;edit['strength']=strength
    candidate={'schema_version':1,'target':edit['source'],'mask':edit['mask'],'transform':edit['transform'],'confidence':.9,'state':'proposed','reason':None}
    intent={'schema_version':1,'kind':edit['kind'],'reference':edit['source'],'reference_mask':edit['mask'],'target_snapshot_id':str(uuid4()),'strength':edit['strength'],'ev':edit['ev'],'prompt':edit['prompt'],'preserve_persons':True}
    candidate_raw=canonical_bytes(candidate);intent_raw=canonical_bytes(intent)
    edit['candidate']['sha256']=sha256(candidate_raw).hexdigest()
    edit['intent']['sha256']=sha256(intent_raw).hexdigest()
    return edit,candidate_raw,intent_raw


def test_binding_owns_exact_verified_document_content():
    edit,candidate,intent=documents()
    owned,proposal,original=bind_approved_edit_documents(edit,bytearray(candidate),bytearray(intent))
    assert proposal.target==owned.source and original.ev==owned.ev
    edit['ev']=-1
    assert owned.ev==.25 and original.ev==.25


@pytest.mark.parametrize('field,value',[('ev',-.5),('strength',.1),('prompt','substituted'),('kind','remove_object')])
def test_parameter_substitution_is_denied(field,value):
    edit,candidate,intent=documents();edit[field]=value
    if field=='kind':edit['ev']=None
    with pytest.raises(ValueError):bind_approved_edit_documents(edit,candidate,intent)


@pytest.mark.parametrize('field',['mask','transform','source'])
def test_candidate_geometry_or_source_substitution_is_denied(field):
    edit,candidate,intent=documents()
    if field=='source':edit[field]['canvas_hash']='c'*64
    else:edit[field]['artifact_id']=str(uuid4())
    with pytest.raises(ValueError,match='CANDIDATE_BINDING'):
        bind_approved_edit_documents(edit,candidate,intent)


def test_raw_byte_hash_substitution_is_denied_before_parsing():
    edit,candidate,intent=documents()
    with pytest.raises(ValueError,match='DOCUMENT_HASH'):
        bind_approved_edit_documents(edit,candidate+b' ',intent)
