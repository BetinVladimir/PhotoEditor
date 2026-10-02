from types import SimpleNamespace as NS
import pytest
from photo_worker.operations.optical.adapter import execute
from photo_worker.runtime.executor import OperationFailure


def test_lens_adapter_requires_authoritative_admission_before_inputs():
    class Artifacts:
        def params(self,model):raise AssertionError('must not read inputs')
    ctx=NS(lease=NS(execution_profile={'renderer_version':'v2.2','backend':'cpu'}))
    with pytest.raises(OperationFailure):execute(ctx,Artifacts(),None)
    ctx.lease.execution_profile['renderer_version']='legacy'
    with pytest.raises(OperationFailure):execute(ctx,Artifacts(),None)


@pytest.mark.parametrize('strength',[0.,1.])
def test_lens_adapter_executes_source_files_and_returns_single_root(tmp_path,strength):
    from contextlib import contextmanager
    from uuid import uuid4
    from hashlib import sha256
    import numpy as np
    from photo_worker.contracts.base import ArtifactRef,canonical_hash,canonical_bytes
    from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain
    from photo_worker.operations.optical.contracts import OpticalLensParams,ImageResult
    from photo_worker.runtime.scratch import ScratchSpace
    from test_v25_lens_map import profile
    def ref(raw=b'input'):return ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    rgb=np.arange(27,dtype=np.float32).reshape(3,3,3)/27
    path=tmp_path/'rgb.npy';np.save(path,rgb,allow_pickle=False)
    pixels=ref(path.read_bytes())
    source=WorkingImage(pixels=pixels,alpha=None,dtype='float32',
        canvas=Canvas(width=3,height=3,orientation=1,space_id='source',source_sha256='b'*64),
        color=ColorSpace(profile_id='work_rec2020_gamma24_v1',encoding='gamma24',icc_sha256='c'*64),
        transform_chain=TransformChain.create([]))
    source_ref=ref(canonical_bytes(source));lens=profile(k=[.2,0,0]);lens_ref=ref(canonical_bytes(lens))
    params=OpticalLensParams(schema_version=1,base={'asset_version_id':uuid4(),'source':source_ref,'canvas_hash':canonical_hash(source.canvas)},strength=strength,profile=lens_ref,focal_mm=50,aperture=2)
    class Admission:
        calls=0
        def admit(self,ctx,p,selected):
            self.calls+=1;assert p==params and selected==lens
            return 'Camera','Lens',50,2
    class Artifacts:
        def __init__(self,scratch):self.scratch=scratch;self.members={};self.roles=[]
        def params(self,model):return params
        def contract(self,value,model):return source if value==source_ref else lens
        @contextmanager
        def input(self,value,suffix):
            assert value==pixels;yield NS(path=path)
        def stage(self,path,reservation,role,media_type):
            reservation.validate_file(path);value=ref(path.read_bytes())
            self.members[value.artifact_id]=np.load(path,allow_pickle=False);self.roles.append(role);return value
        def stage_contract(self,value,role):
            result=ref(canonical_bytes(value));self.members[result.artifact_id]=value;self.roles.append(role);return result
    from photo_worker.operations.optical.resources import lens_resources
    ctx=NS(lease=NS(execution_profile={'renderer_version':'v2.2','backend':'cpu','resource_estimate':lens_resources(3,3,strength=strength)}),check_active=lambda:None,progress=lambda done,total:None)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,65536,min_free_bytes=0) as scratch:
        artifacts=Artifacts(scratch);admission=Admission()
        result=execute(ctx,artifacts,admission)
        assert admission.calls==1 and len(result.outputs)==1
        assert result.bytes_written>0
        document=artifacts.members[result.outputs[0].artifact_id]
        assert isinstance(document,ImageResult) and document.generated_mask is None
        if strength==0:
            assert document.working_image==source_ref and not result.review_required
            assert 'pixels' not in artifacts.roles
        else:
            output=artifacts.members[document.working_image.artifact_id]
            assert output.color==source.color and output.transform_chain.steps[-1].pixel_centers=='half_pixel'
            assert result.review_required
        assert not list(scratch.directory.iterdir())
        class WrongAdmission:
            def admit(self,*args):return 'Other','Lens',50,2
        rejected=Artifacts(scratch)
        with pytest.raises(OperationFailure):execute(ctx,rejected,WrongAdmission())
        assert rejected.roles==[]
