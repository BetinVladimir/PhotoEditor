from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.portrait.donor_adapter import DonorRuntimeOptions
from photo_worker.v2 import portrait_donor_adoption_prepare as module
from photo_worker.v2.lens_bypass import _ref
from test_portrait_donor_runtime_lock import current_runtime


@pytest.mark.parametrize('mode',['ok','runtime','revoked'])
def test_preparer_derives_threshold_and_authority_from_completed_evidence(tmp_path,monkeypatch,mode):
    ref=lambda:ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    params,root,producer=ref(),ref(),ref();calls=[]
    evidence=dict(profile={'owned':True},params={},params_ref=params.model_dump(mode='json'),proof_jcs='{}',
        manual={'root':root.model_dump(mode='json'),'original_params':producer.model_dump(mode='json')})
    context=SimpleNamespace(root=_ref(ref()))
    options=DonorRuntimeOptions(current_runtime().model_copy(deep=True),'b'*64,tmp_path,tmp_path,100,200)
    monkeypatch.setattr(module,'load_donor_adoption_evidence',lambda *args,**kwargs:evidence)
    def checkpoint():
        calls.append('current')
        if mode=='revoked' and 'replay' in calls:raise ValueError('revoked')
        return evidence
    monkeypatch.setattr(module,'donor_adoption_checkpoint',lambda *args,**kwargs:checkpoint)
    monkeypatch.setattr(module,'load_donor_adoption_qualification',lambda *args,**kwargs:
        (SimpleNamespace(runtime_lock_sha='c'*64 if mode=='runtime' else 'b'*64),SimpleNamespace(min_face_edge_px=77)))
    authority=lambda *args:True
    monkeypatch.setattr(module,'donor_adoption_manual_authority',lambda *args,**kwargs:authority)
    def replay(reader,params_pin,result_pin,**kwargs):
        calls.append('replay')
        assert params_pin==_ref(params) and result_pin==context.root
        assert kwargs['min_face_edge_px']==77 and kwargs['producer_authority'] is authority
        assert kwargs['authority']({},b'{}') is True
        kwargs['check_active']()
    monkeypatch.setattr(module,'verify_donor_reconstruction',replay)
    def run():return module.prepare_donor_reconstruction(None,None,None,uuid4(),uuid4(),context,
        bucket='bucket',policy={},options=options,output_scratch_root=tmp_path,output_max_bytes=300,ram_max_bytes=1<<30,check_active=lambda:None)
    if mode=='ok':assert run().params_ref==_ref(params) and calls[-1]=='current'
    else:
        with pytest.raises(ValueError):run()
        if mode=='runtime':assert calls==[]
