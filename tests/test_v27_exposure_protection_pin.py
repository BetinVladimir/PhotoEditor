from types import SimpleNamespace
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,ArtifactRef as ContractRef
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.contracts.images import MaskRef
from photo_worker.v2.adoption import ArtifactRef,ArtifactEvidence,AdoptionInvalid
from photo_worker.v2.exposure_protection_pin import load_exposure_protection_pin
from test_v27_exposure_loader import loader_fixture


@pytest.mark.parametrize('failure',[None,'manual','missing','hash','source','unprotected','unlocked_model'])
def test_pinned_protection_document_reader(tmp_path,failure):
    edit,root,artifacts,_=loader_fixture(tmp_path)
    edit=ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit))
    root=ContractRef.model_validate_json(canonical_bytes(root))
    mask=MaskRef.model_validate_json(artifacts.contract_bytes(root,MaskRef))
    if failure=='unprotected':mask.protected=False
    if failure in ('manual','unlocked_model'):
        mask.model_lock_id=None
        mask.selector='manual' if failure=='manual' else 'person'
    body=canonical_bytes(mask)
    pin=ArtifactRef(root.artifact_id,sha256(body).hexdigest() if failure!='hash' else 'f'*64)
    raw=canonical_bytes(edit);params=ArtifactRef(uuid4(),sha256(raw).hexdigest())
    context=SimpleNamespace(attempt=1,fence=2,result_id=uuid4())
    db=SimpleNamespace(autocommit=True,execute=lambda *a:SimpleNamespace(fetchone=lambda:
        None if failure=='missing' else (pin.artifact_id,pin.sha256)))
    reader=SimpleNamespace(read_json=lambda ref,limit:raw if ref==params else body,
        evidence=lambda ref:ArtifactEvidence(ref,'application/json',len(body),None,
            'output' if failure=='source' else 'input'))
    args=(db,uuid4(),uuid4(),context,params,edit,reader)
    if failure not in (None,'manual'):
        with pytest.raises(AdoptionInvalid):load_exposure_protection_pin(*args,check_active=lambda:None)
    else:
        actual,decoded=load_exposure_protection_pin(*args,check_active=lambda:None)
        assert actual.artifact_id==pin.artifact_id and decoded.protected
