from contextlib import contextmanager
import hashlib,json
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import Canvas,ColorSpace,MaskRef,TransformChain,WorkingImage
from photo_worker.contracts.quality import QualityParams,QualityReport,Seam
from photo_worker.operations.quality.check import execute
from test_asset_adapters import context_for


class Artifacts:
    def __init__(self,root,protected_value=1):
        self.paths={};self.contracts={};self.staged=[];h=w=32;base=np.random.default_rng(2).random((h,w,3),dtype=np.float32)*.8+.1;target=base.copy();target[10,10]=.9
        canvas=Canvas(width=w,height=h,orientation=1,space_id='source',source_sha256='a'*64);chain=TransformChain.create([]);color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='b'*64,encoding='gamma24')
        self.target=self.image(root,'target',target,canvas,chain,color);self.baseline=self.image(root,'base',base,canvas,chain,color);mask=np.zeros((h,w),np.float32);mask[10,10]=protected_value;mr=self.array(root,'mask',mask);protected=MaskRef(artifact=mr,canvas=canvas,transform_chain=chain,selector='manual',confidence=1,model_lock_id=None,protected=True)
        self.value=QualityParams(target=self.target,target_kind='working',stage='master',threshold_profile_id='master_v1',baseline=self.baseline,protected=[protected],seams=[Seam(orientation='vertical',position=.5)])
    def array(self,root,name,value):path=root/(name+'.npy');np.save(path,value,allow_pickle=False);r=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest());self.paths[r.artifact_id]=path;return r
    def image(self,root,name,rgb,canvas,chain,color,alpha=None):
        image=WorkingImage(pixels=self.array(root,name,rgb),canvas=canvas,color=color,dtype='float32',alpha=alpha,transform_chain=chain);r=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(image)).hexdigest());self.contracts[r.artifact_id]=image;return r
    def params(self,model):return model.model_validate_json(json.dumps(self.value.model_dump(mode='json')))
    def contract(self,r,model):return self.contracts[r.artifact_id]
    @contextmanager
    def input(self,r,*,suffix):yield SimpleNamespace(path=self.paths[r.artifact_id])
    def stage_contract(self,value,role):self.staged.append((role,value));return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest())


@pytest.mark.parametrize('protected_value',[1,.25,np.nextafter(np.float32(0),np.float32(1))])
def test_master_quality_fails_changed_protected_pixel(tmp_path,protected_value):
    a=Artifacts(tmp_path,protected_value);target=a.contracts[a.target.artifact_id];base=a.contracts[a.baseline.artifact_id];ctx=context_for(a.target,'quality.check');ctx.lease.command.inputs=[a.target,target.pixels,a.baseline,base.pixels,a.value.protected[0].artifact]
    result=execute(ctx,a);report=a.staged[0][1];QualityReport.model_validate_json(canonical_bytes(report))
    assert report.status=='fail' and 'protected_pixels_changed' in report.reasons
    assert report.reference.protected_max_delta>2/65535 and result.review_required


def test_master_quality_zero_mask_does_not_claim_protected_change(tmp_path):
    a=Artifacts(tmp_path,0);target=a.contracts[a.target.artifact_id];base=a.contracts[a.baseline.artifact_id];ctx=context_for(a.target,'quality.check');ctx.lease.command.inputs=[a.target,target.pixels,a.baseline,base.pixels,a.value.protected[0].artifact]
    execute(ctx,a);report=a.staged[0][1]
    assert report.reference.protected_max_delta==0
    assert 'protected_pixels_changed' not in report.reasons


def test_quality_contract_rejects_export_baseline():
    import pytest
    with pytest.raises(ValueError):QualityParams(target=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),target_kind='encoded',stage='export',threshold_profile_id='export_v1',baseline=ArtifactRef(artifact_id=uuid4(),sha256='b'*64))


def test_transparent_rgb_zero_is_not_black_clipping(tmp_path):
    a=Artifacts(tmp_path);old=a.contracts[a.target.artifact_id];rgb=np.zeros((32,32,3),np.float32);rgb[8:24,8:24]=.4;alpha=np.zeros((32,32),np.uint16);alpha[8:24,8:24]=65535;alpha_ref=a.array(tmp_path,'alpha',alpha)
    a.target=a.image(tmp_path,'transparent',rgb,old.canvas,old.transform_chain,old.color,alpha_ref)
    a.value=QualityParams(target=a.target,target_kind='working',stage='master',threshold_profile_id='master_v1')
    target=a.contracts[a.target.artifact_id];ctx=context_for(a.target,'quality.check');ctx.lease.command.inputs=[a.target,target.pixels,alpha_ref]
    execute(ctx,a);report=a.staged[0][1]
    assert 'excessive_black_clipping' not in report.reasons and report.status!='fail'
