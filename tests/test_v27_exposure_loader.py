from contextlib import contextmanager
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.operations.semantic.exposure_loader import load_local_exposure_arrays
from test_v27_exposure_geometry import geometry_fixture


def loader_fixture(tmp_path):
    edit,candidate,intent,source,mask,transform,reference=geometry_fixture()
    image=parse_json(source);mask_doc=parse_json(mask)
    protection=parse_json(mask);protection['protected']=True
    protection['artifact']=dict(artifact_id=str(uuid4()),sha256='e'*64)
    protection_raw=canonical_bytes(protection)
    root=dict(artifact_id=str(uuid4()),sha256=sha256(protection_raw).hexdigest())
    documents={edit['candidate']['artifact_id']:candidate,edit['intent']['artifact_id']:intent,edit['source']['source']['artifact_id']:source,edit['mask']['artifact_id']:mask,edit['transform']['artifact_id']:transform,root['artifact_id']:protection_raw}
    arrays={image['pixels']['artifact_id']:np.full((8,8,3),.5,np.float32),mask_doc['artifact']['artifact_id']:np.ones((8,8),np.float32),protection['artifact']['artifact_id']:np.zeros((8,8),np.float32)}
    paths={}
    for aid,value in arrays.items():
        path=tmp_path/(aid+'.npy');np.save(path,value,allow_pickle=False);paths[aid]=path
    events=[]
    class Artifacts:
        def contract_bytes(self,ref,model):return documents[str(ref.artifact_id)]
        @contextmanager
        def input(self,ref,*,suffix):
            events.append('open')
            try:yield SimpleNamespace(path=paths[str(ref.artifact_id)])
            finally:events.append('close')
    return edit,root,Artifacts(),events


def test_verified_metadata_loads_real_npy_and_converts_gamma24(tmp_path):
    edit,root,artifacts,events=loader_fixture(tmp_path)
    result=load_local_exposure_arrays(artifacts,edit,protection_ref=root,verify_origin=lambda *args:True,verify_color=lambda color:True,verify_admission=lambda edit:True,max_bytes=128*1024**2,check_active=lambda:None)
    np.testing.assert_allclose(result.rgb_linear,.5**2.4,rtol=1e-6)
    assert result.rgb_linear.flags.owndata and result.mask.flags.owndata and result.protected.flags.owndata
    assert events==['open','open','open','close','close','close']
    assert result.retained_loader_bytes==0


@pytest.mark.parametrize('budget,color',[(96*64,True),(128*1024**2,False)])
def test_budget_and_icc_denial_precede_array_io(tmp_path,budget,color):
    edit,root,artifacts,events=loader_fixture(tmp_path)
    with pytest.raises(ValueError,match='RESOURCE_PROFILE|ICC_UNQUALIFIED'):
        load_local_exposure_arrays(artifacts,edit,protection_ref=root,verify_origin=lambda *args:True,verify_color=lambda value:color,verify_admission=lambda edit:True,max_bytes=budget,check_active=lambda:None)
    assert events==[]


def test_real_npy_loader_and_executor_preserve_unmasked_linear_pixels(tmp_path):
    from photo_worker.operations.semantic.execute_exposure import execute_local_exposure
    edit,root,artifacts,events=loader_fixture(tmp_path)
    mask_doc=parse_json(artifacts.contract_bytes(SimpleNamespace(artifact_id=edit['mask']['artifact_id']),None))
    mask=np.zeros((8,8),np.float32);mask[:,4:]=.5
    np.save(tmp_path/(mask_doc['artifact']['artifact_id']+'.npy'),mask,allow_pickle=False)
    candidate=artifacts.contract_bytes(SimpleNamespace(artifact_id=edit['candidate']['artifact_id']),None)
    intent=artifacts.contract_bytes(SimpleNamespace(artifact_id=edit['intent']['artifact_id']),None)
    def load(value,*,max_bytes):
        return load_local_exposure_arrays(artifacts,value,protection_ref=root,verify_origin=lambda *args:True,verify_color=lambda color:True,verify_admission=lambda edit:True,max_bytes=max_bytes,check_active=lambda:None)
    result=execute_local_exposure(edit,candidate,intent,load_verified_arrays=load,verify_admission=lambda edit:True,max_bytes=128*1024**2,check_active=lambda:None)
    baseline=np.float32(.5**2.4)
    np.testing.assert_array_equal(result[:,:4],np.full((8,4,3),baseline,np.float32))
    np.testing.assert_allclose(result[:,4:],baseline*2**(.25*.5*.5),rtol=1e-6)
    assert events.count('open')==events.count('close')==3


def test_fractional_protected_overlap_denied_before_color_conversion(tmp_path,monkeypatch):
    from photo_worker.operations.semantic import exposure_loader
    edit,root,artifacts,events=loader_fixture(tmp_path)
    protection=parse_json(artifacts.contract_bytes(SimpleNamespace(artifact_id=root['artifact_id']),None))
    values=np.zeros((8,8),np.float32);values[0,0]=.001
    np.save(tmp_path/(protection['artifact']['artifact_id']+'.npy'),values,allow_pickle=False)
    def forbidden(*args):raise AssertionError('converted RGB before overlap check')
    monkeypatch.setattr(exposure_loader,'to_working_linear',forbidden)
    with pytest.raises(ValueError,match='PROTECTED_OVERLAP'):
        load_local_exposure_arrays(artifacts,edit,protection_ref=root,verify_origin=lambda *args:True,verify_color=lambda color:True,verify_admission=lambda edit:True,max_bytes=128*1024**2,check_active=lambda:None)
    assert events.count('open')==events.count('close')==3
