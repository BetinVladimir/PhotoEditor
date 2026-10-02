from contextlib import contextmanager
import hashlib,json
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.derivative import ResizeParams,SharpenParams,WatermarkParams
from photo_worker.contracts.images import Canvas,ColorSpace,TransformChain,WorkingImage
from photo_worker.operations.derivative.resize import execute as resize
from photo_worker.operations.derivative.sharpen import execute as sharpen
from photo_worker.operations.derivative.watermark import execute as watermark
from photo_worker.runtime.scratch import ScratchSpace
from test_asset_adapters import context_for


class Artifacts:
    def __init__(self,root,param_type):
        self.paths={};self.contracts={};self.staged=[];self.outputs={}
        y,x=np.mgrid[0:32,0:48];rgb=np.stack((x/47,y/31,np.full_like(x,.25)),axis=-1).astype(np.float32);self.rgb=rgb;self.image_ref=self.image(root,'source',rgb,None)
        if param_type is ResizeParams:self.value=ResizeParams(image=self.image_ref,long_edge=24)
        elif param_type is SharpenParams:self.value=SharpenParams(image=self.image_ref,radius=1,strength=1,threshold=0,max_delta=.02)
        else:
            mark=np.ones((8,16,3),np.float32);alpha=np.full((8,16),65535,np.uint16);self.mark_ref=self.image(root,'mark',mark,alpha);self.value=WatermarkParams(image=self.image_ref,watermark=self.mark_ref,opacity=.5,anchor='bottom_right')
    def ref(self,data):return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(data).hexdigest())
    def array(self,root,name,value):
        path=root/(name+'.npy');np.save(path,value,allow_pickle=False);r=self.ref(path.read_bytes());self.paths[r.artifact_id]=path;return r
    def image(self,root,name,rgb,alpha):
        pixels=self.array(root,name+'-rgb',rgb);aref=None if alpha is None else self.array(root,name+'-alpha',alpha);canvas=Canvas(width=rgb.shape[1],height=rgb.shape[0],orientation=1,space_id=name,source_sha256=pixels.sha256);image=WorkingImage(pixels=pixels,canvas=canvas,color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='a'*64,encoding='gamma24'),dtype='float32',alpha=aref,transform_chain=TransformChain.create([]));r=self.ref(canonical_bytes(image));self.contracts[r.artifact_id]=image;return r
    def params(self,model):return model.model_validate_json(json.dumps(self.value.model_dump(mode='json')))
    def contract(self,r,model):return self.contracts[r.artifact_id]
    @contextmanager
    def input(self,r,*,suffix):yield SimpleNamespace(path=self.paths[r.artifact_id])
    def stage(self,path,reservation,role,media_type):reservation.validate_file(path);self.outputs[role]=np.load(path,allow_pickle=False);return self.ref(path.read_bytes())
    def stage_contract(self,value,role):self.staged.append((role,value));return self.ref(canonical_bytes(value))


def run(tmp_path,kind,fn):
    a=Artifacts(tmp_path,kind);ctx=context_for(a.image_ref,{'ResizeParams':'derivative.resize','SharpenParams':'derivative.sharpen','WatermarkParams':'derivative.watermark'}[kind.__name__]);p=a.value;refs=[p.image,*([p.watermark] if hasattr(p,'watermark') else [])];ctx.lease.command.inputs=[]
    for r in refs:
        image=a.contracts[r.artifact_id];ctx.lease.command.inputs.extend([r,image.pixels,*([image.alpha] if image.alpha else [])])
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:a.scratch=scratch;result=fn(ctx,a)
    return a,result


def test_resize_preserves_aspect_and_appends_transform(tmp_path):
    a,result=run(tmp_path,ResizeParams,resize);image=a.staged[0][1]
    assert a.outputs['working_rgb'].shape==(16,24,3) and (image.canvas.width,image.canvas.height)==(24,16)
    assert image.transform_chain.steps[-1].kind=='resize' and len(result.outputs)==3


def test_sharpen_caps_linear_channel_delta(tmp_path):
    a,_=run(tmp_path,SharpenParams,sharpen);report=a.staged[1][1]
    assert report.applied and report.max_channel_delta<=.0200001 and a.outputs['working_rgb'].shape==a.rgb.shape


def test_watermark_uses_alpha_and_keeps_geometry(tmp_path):
    a,_=run(tmp_path,WatermarkParams,watermark);image=a.staged[0][1]
    assert image.canvas==a.contracts[a.image_ref.artifact_id].canvas and image.transform_chain==a.contracts[a.image_ref.artifact_id].transform_chain
    assert np.mean(a.outputs['working_rgb'][-8:,-12:])>np.mean(a.rgb[-8:,-12:])
