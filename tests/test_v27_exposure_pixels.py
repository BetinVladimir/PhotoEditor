from hashlib import sha256
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage, MaskRef
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.v2.adoption import ArtifactRef, ArtifactEvidence, AdoptionInvalid
from photo_worker.v2.exposure_pixels import verify_exposure_pixels
from photo_worker.operations.semantic.exposure_pipeline import execute_encoded_local_exposure
from test_v27_exposure_pipeline import inputs


@pytest.mark.parametrize('failure', [None, 'outside', 'inside', 'protected'])
def test_real_npy_replay_and_substitutions(tmp_path, failure):
    edit, root, artifacts, _, candidate, intent = inputs(tmp_path)
    edit = ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit))
    source = WorkingImage.model_validate_json(artifacts.contract_bytes(edit.source.source, WorkingImage))
    mask = MaskRef.model_validate_json(artifacts.contract_bytes(edit.mask, MaskRef))
    from photo_worker.contracts.base import ArtifactRef as ContractRef
    root = ContractRef.model_validate_json(canonical_bytes(root))
    protection = MaskRef.model_validate_json(artifacts.contract_bytes(root, MaskRef))
    expected = execute_encoded_local_exposure(artifacts, edit, candidate, intent,
        protection_ref=root, verify_origin=lambda *args:True, verify_color=lambda color:True,
        verify_admission=lambda edit:True, max_bytes=128*1024**2, check_active=lambda:None)
    if failure == 'outside':expected[0,0,0] = .51
    if failure == 'inside':expected[0,7,0] = .51
    output = source.model_copy(deep=True)
    data = {}; evidence = {}
    def add(values, source_kind, role):
        path = tmp_path/(str(uuid4())+'.npy');np.save(path, values, allow_pickle=False)
        body = path.read_bytes();ref = ArtifactRef(uuid4(),sha256(body).hexdigest())
        data[ref] = body
        evidence[ref] = ArtifactEvidence(ref,'application/x-npy',len(body),role,source_kind)
        return dict(artifact_id=ref.artifact_id,sha256=ref.sha256)
    from photo_worker.contracts.base import ArtifactRef as ContractRef
    source.pixels = ContractRef(**add(np.full((8,8,3),.5,np.float32),'input',None))
    output.pixels = ContractRef(**add(expected,'output','pixels'))
    weights = np.zeros((8,8),np.float32);weights[:,4:] = .5
    mask.artifact = ContractRef(**add(weights,'input',None))
    protected = np.zeros((8,8),np.float32)
    if failure == 'protected':protected[0,7] = .1
    protection.artifact = ContractRef(**add(protected,'input',None))
    class Reader:
        def evidence(self, ref):return evidence[ref]
        def iter_bytes(self, ref, size):yield data[ref]
    args = (Reader(),edit,source,output,mask,protection)
    options = dict(scratch_root=tmp_path,max_bytes=2*1024**2,
        verify_color=lambda color:True,check_active=lambda:None)
    if failure:
        with pytest.raises(AdoptionInvalid):verify_exposure_pixels(*args,**options)
    else:verify_exposure_pixels(*args,**options)
    assert not list(tmp_path.glob('adoption-*'))
