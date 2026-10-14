from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.v2.portrait_donor_input_documents import load_donor_input_documents
from test_portrait_donor_manual import manual_case
from test_portrait_open_eyes_stage import stage_case


@pytest.mark.parametrize('mode',['ok','ambiguous','corrupt','undeclared','mask'])
def test_donor_original_document_loader(mode):
    args,_=manual_case()
    params,source,target,guard,root,root_raw,producer,producer_raw,*_=args
    other,donor,_,_,proof,faces=stage_case()
    params.donor=other.donor
    params.confirmation=other.confirmation
    params.source_faces=other.source_faces;params.donor_faces=other.donor_faces
    body=canonical_bytes(params)
    pin=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
    documents={pin.artifact_id:body,params.base.source.artifact_id:source,
        params.donor.source.artifact_id:donor,params.confirmation.artifact_id:proof,
        params.source_faces.artifact_id:faces[0],params.donor_faces.artifact_id:faces[1],
        params.eyes_mask.artifact_id:target,params.protected.artifact_id:guard,
        root.artifact_id:root_raw,producer.artifact_id:producer_raw}
    if mode=='mask':
        # Rehashed mask must still match immutable rasterizer root exactly.
        from photo_worker.contracts.images import MaskRef
        changed=MaskRef.model_validate_json(target).model_copy(update={'confidence':0.5})
        documents[params.eyes_mask.artifact_id]=canonical_bytes(changed)
        params.eyes_mask.sha256=sha256(documents[params.eyes_mask.artifact_id]).hexdigest()
        body=canonical_bytes(params);pin.sha256=sha256(body).hexdigest();documents[pin.artifact_id]=body
    class Artifacts:
        def require_declared_input(self,ref):
            if mode=='undeclared' and ref==root:raise ValueError('undeclared')
        def declared_contract_refs(self,contract,maximum):
            if contract=='Params':return (pin,producer)
            return () if mode=='ambiguous' else (root,)
        def contract_bytes(self,ref,model):
            raw=documents[ref.artifact_id]
            return raw+b' ' if mode=='corrupt' and ref==producer else raw
    if mode=='ok':
        result=load_donor_input_documents(Artifacts(),pin,check_active=lambda:None)
        assert result.root==root_raw and result.producer==producer_raw and result.vectors is None
        assert result.params==body and result.source==source
    else:
        with pytest.raises(ValueError):load_donor_input_documents(Artifacts(),pin,check_active=lambda:None)
