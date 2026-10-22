from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.images import WorkingImage, MaskRef
from photo_worker.contracts.restoration import GenerationReport
from photo_worker.operations.editor.coverage_trace import prepare_coverage_trace
from photo_worker.runtime.scratch import ScratchSpace
from test_interchange_import_stage import adopted_fixture


def reference():return ArtifactRef(artifact_id=uuid4(),sha256='a'*64)


class Artifacts:
    def __init__(self,root,documents,arrays):
        root.mkdir(parents=True,exist_ok=True)
        self.scratch=ScratchSpace(root,uuid4(),uuid4(),1,16*1048576)
        self.documents=documents;self.arrays=arrays
    def contract(self,ref,model):return self.documents[ref.artifact_id]
    @contextmanager
    def input(self,ref,**kwargs):yield SimpleNamespace(path=self.arrays[ref.artifact_id])


def fixture(tmp_path):
    _,_,_,raw=adopted_fixture();image=WorkingImage.model_validate_json(raw)
    refs=[reference() for _ in range(3)];reports=[reference(),reference()]
    documents={ref.artifact_id:image.model_copy(update={'pixels':reference()}) for ref in refs}
    arrays={};entries=[]
    for index,report_ref in enumerate(reports):
        mask_ref,pixels_ref=reference(),reference()
        physical=np.zeros((image.canvas.height,image.canvas.width),np.float32);physical[0,index]=1
        path=tmp_path/f'{index}.npy';np.save(path,physical,allow_pickle=False);arrays[pixels_ref.artifact_id]=path
        documents[mask_ref.artifact_id]=MaskRef(artifact=pixels_ref,canvas=image.canvas,transform_chain=image.transform_chain,
            object_id=None,selector='generated',confidence=1,model_lock_id=None,protected=False)
        documents[report_ref.artifact_id]=GenerationReport(image=refs[index],mask=pixels_ref,model_id='sd15_inpaint',
            model_lock_id='b'*64,backend='cpu',seed=1,guidance_scale=7.5,generated_pixels=1,protected_pixels=0,
            side=None,source_rect=[0,0,image.canvas.width,image.canvas.height],generated_mask=mask_ref)
        entries.append(SimpleNamespace(task_id=uuid4(),source=refs[index],output=refs[index+1],operation='image.generate',report=report_ref))
    params=SimpleNamespace(coverage_trace=entries,generated=None,generative_requested=True,processed_source=SimpleNamespace(source=refs[-1]))
    return Artifacts(tmp_path/'scratch',documents,arrays),params


def test_canonical_trace_unions_all_actual_generation_masks(tmp_path):
    artifacts,params=fixture(tmp_path)
    with prepare_coverage_trace(artifacts,params,lambda:None) as (path,owner,image):
        mask=np.load(path,allow_pickle=False)
        assert mask[0,0]==mask[0,1]==1 and np.count_nonzero(mask)==2
        assert mask.dtype==np.float32


def test_disconnected_source_or_foreign_report_is_refused(tmp_path):
    artifacts,params=fixture(tmp_path)
    params.coverage_trace[1].source=reference()
    with pytest.raises(ValueError,match='TRACE_CHAIN'):
        with prepare_coverage_trace(artifacts,params,lambda:None):pass


def test_trace_cannot_drop_requested_generation_flag(tmp_path):
    artifacts,params=fixture(tmp_path);params.generative_requested=False
    with pytest.raises(ValueError,match='TRACE_TARGET'):
        with prepare_coverage_trace(artifacts,params,lambda:None):pass


def test_trace_crops_previous_generation_support_before_final_union(tmp_path):
    from photo_worker.contracts.images import Canvas,Rect,CropTransform,TransformChain
    artifacts,params=fixture(tmp_path)
    params.coverage_trace=params.coverage_trace[:1]
    previous=params.coverage_trace[0].output
    image=artifacts.documents[previous.artifact_id]
    canvas=Canvas(width=image.canvas.width-1,height=image.canvas.height,orientation=1,space_id='cropped',source_sha256=image.canvas.source_sha256)
    step=CropTransform(kind='crop',from_canvas=image.canvas,to_canvas=canvas,invertibility='partial',
        valid_region=Rect(x=0,y=0,width=canvas.width,height=canvas.height),
        crop_rect=Rect(x=1,y=0,width=canvas.width,height=canvas.height))
    target=reference();artifacts.documents[target.artifact_id]=image.model_copy(update={
        'canvas':canvas,'transform_chain':TransformChain.create([*image.transform_chain.steps,step]),'pixels':reference()})
    params.coverage_trace.append(SimpleNamespace(task_id=uuid4(),source=previous,output=target,operation='geometry.crop',report=None))
    params.processed_source.source=target
    with prepare_coverage_trace(artifacts,params,lambda:None) as (path,owner,metadata):
        physical=np.load(path,allow_pickle=False)
        assert physical.shape==(canvas.height,canvas.width)
        # The only generated source pixel was cropped away; no false nearest
        # fallback, guide spread, or fabricated remaining generated pixels.
        assert np.count_nonzero(physical)==0
