from hashlib import sha256
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.operations.layers.prepare import prepare_layer
from photo_worker.operations.layers.result import stage_layer_result
from photo_worker.operations.optical.contracts import ImageResult,ImageReport
from photo_worker.runtime.scratch import ScratchSpace

@pytest.mark.parametrize('converted',[False,True])
@pytest.mark.parametrize('parent_status',['pass','review'])
@pytest.mark.parametrize('change',['none','rgb','alpha','both'])
def test_stage_layer_reuses_independent_refs_and_closes_output_graph(tmp_path,change,parent_status,converted):
    ref=lambda:ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    source=WorkingImage(pixels=ref(),alpha=ref(),dtype='float32',canvas=Canvas(width=2,height=1,orientation=1,space_id='source',source_sha256='b'*64),color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),transform_chain=TransformChain.create([]))
    source_ref=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(source))
    parent=ImageResult(schema_version=1,working_image=ref(),report=ref(),changed_mask=ref(),generated_mask=None)
    binding=VersionBinding(asset_version_id=uuid4(),source=source_ref,canvas_hash=canonical_hash(source.canvas))
    class Artifacts:
        values={};roles=[]
        def put(self,body,role,value):
            reference=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
            self.values[reference.artifact_id]=value;self.roles.append(role);return reference
        def stage(self,path,reservation,role,media):
            assert media=='application/x-npy';reservation.validate_file(path)
            return self.put(path.read_bytes(),role,np.load(path,allow_pickle=False))
        def contract(self,reference,model):
            assert reference==parent.report and model is ImageReport
            return ImageReport(schema_version=1,status=parent_status,operation='fixture',skipped_count=0,reason=None,outside_mask_max_error=0,clipping_fraction=0,identity=None,transform=None,sources=[binding],registrations=[])
        def stage_contract(self,value,role):return self.put(canonical_bytes(value),role,value)
    artifacts=Artifacts();space=ScratchSpace(tmp_path,uuid4(),uuid4(),1,65536,min_free_bytes=0)
    s=np.full((1,2,3),.2,np.float32);p=np.full_like(s,.8) if change in ('rgb','both') else s
    a=np.full((1,2),.5,np.float32);pa=np.ones_like(a) if change in ('alpha','both') else a
    try:
        with prepare_layer(s,p,lambda *args:np.ones((1,2),np.float32),space,color=source.color,opacity=1,blend='normal',source_alpha=a,proposal_alpha=pa,source_alpha_converted=converted,max_tile_bytes=4096) as files:
            staged=stage_layer_result(artifacts,source_ref,source,binding,files,proposal_result=parent)
            document=artifacts.values[staged.document.artifact_id]
            image=source if change=='none' and not converted else artifacts.values[document.working_image.artifact_id]
            assert image.canvas==source.canvas and image.transform_chain==source.transform_chain and image.color==source.color
            assert (image.pixels==source.pixels)==(change not in ('rgb','both'))
            assert (image.alpha==source.alpha)==(change not in ('alpha','both') and not converted)
            assert (document.working_image==source_ref)==(change=='none' and not converted)
            assert document.generated_mask is None
            mask=artifacts.values[document.changed_mask.artifact_id]
            np.testing.assert_array_equal(artifacts.values[mask.artifact.artifact_id],0 if change=='none' else 1)
            report=artifacts.values[document.report.artifact_id]
            assert staged.review_required==(parent_status=='review')
            assert report.status==parent_status
            assert report.reason==('LAYER_PARENT_REVIEW' if parent_status=='review' else None)
            assert report.operation=='layer.compose' and report.sources==[binding] and report.transform is None
            assert len(staged.outputs)==4+int(change!='none' or converted)+int(change in ('rgb','both'))+int(change in ('alpha','both') or converted)
            assert files.any_changed==(change!='none')
            assert files.alpha_converted==converted
            assert staged.bytes_written>0
            before=len(artifacts.roles)
            wrong=binding.model_copy(update={'canvas_hash':'f'*64})
            with pytest.raises(ValueError):stage_layer_result(artifacts,source_ref,source,wrong,files,proposal_result=parent)
            assert len(artifacts.roles)==before
            generated=parent.model_copy(update={'generated_mask':ref()})
            with pytest.raises(ValueError,match='GENERATED_BINDING_INVALID'):
                stage_layer_result(artifacts,source_ref,source,binding,files,proposal_result=generated)
            assert len(artifacts.roles)==before
    finally:space.cleanup()
