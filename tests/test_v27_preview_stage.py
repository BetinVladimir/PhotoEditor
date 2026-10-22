from hashlib import sha256
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.operations.semantic.preview_stage import stage_exposure_preview
from test_v27_exposure_preview_binding import fixture
from test_v27_exposure_source import source_fixture


@pytest.mark.parametrize('accepted',[True,False])
def test_preview_stage_retains_color_and_checks_independent_admission(tmp_path,accepted):
    params,_,_=fixture();edit,image,raw=source_fixture();params['source']=edit['source']
    calls=[]
    class Artifacts:
        def stage(self,path,reservation,role,media):
            calls.append(role)
            np.testing.assert_array_equal(np.load(path,allow_pickle=False),np.full((1,2,3),.6,np.float32))
            return ArtifactRef(artifact_id=uuid4(),sha256=sha256(path.read_bytes()).hexdigest())
        def stage_contract(self,value,role):
            calls.append(role)
            assert value.color.model_dump(mode='json')==image['color']
            return ArtifactRef(artifact_id=uuid4(),sha256=sha256(canonical_bytes(value)).hexdigest())
    artifacts=Artifacts()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,2*1024**2,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch
        options=dict(verify_admission=lambda params:accepted,check_active=lambda:None,max_bytes=128*1024**2,max_output_bytes=2*1024**2)
        if accepted:
            result=stage_exposure_preview(artifacts,params,raw,np.full((1,2,3),.6,np.float32),**options)
            assert result.bytes_written>24 and calls==['pixels','working_image']
        else:
            with pytest.raises(ValueError,match='ADMISSION_DENIED'):
                stage_exposure_preview(artifacts,params,raw,np.full((1,2,3),.6,np.float32),**options)
            assert not calls


@pytest.mark.parametrize('failure',[None,'budget','evidence_hash','evidence_mutation','evidence_alias','revoked'])
def test_real_artifact_preview_compute_and_stage(tmp_path,failure):
    from test_v27_preview_loader import setup
    from photo_worker.operations.semantic.preview_stage import execute_and_stage_exposure_preview
    params,artifacts,events=setup(tmp_path)
    stages=[];revoked=False
    def stage(path,reservation,role,media):
        result=np.load(path,allow_pickle=False)
        assert result.shape==(8,8,3) and np.all(result>.5)
        stages.append(role)
        return ArtifactRef(artifact_id=uuid4(),sha256=sha256(path.read_bytes()).hexdigest())
    def contract(value,role):
        nonlocal revoked
        stages.append(role)
        from hashlib import sha256
        from photo_worker.contracts.base import canonical_bytes
        if role=='working_image':assert value.color.encoding=='gamma24'
        else:
            assert role=='preview_evidence' and value.protected_pixels_unchanged
            assert value.params.target_revision==params['target_revision']
        digest=sha256(canonical_bytes(value)).hexdigest()
        artifact_id=uuid4()
        if role=='preview_evidence':
            if failure=='evidence_hash':digest='0'*64
            if failure=='evidence_mutation':value.params.target_revision='999'
            if failure=='evidence_alias':artifact_id=value.pixels.artifact_id
            if failure=='revoked':revoked=True
        return ArtifactRef(artifact_id=artifact_id,sha256=digest)
    artifacts.stage=stage;artifacts.stage_contract=contract
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,2*1024**2,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch
        options=dict(verify_admission=lambda value:not revoked,verify_origin=lambda *args:True,
            verify_color=lambda color:True,check_active=lambda:None,
            max_bytes=128*1024**2,max_output_bytes=1 if failure=='budget' else 2*1024**2)
        if failure is None:
            result=execute_and_stage_exposure_preview(artifacts,params,**options)
            assert result.bytes_written>8*8*12 and stages==['pixels','working_image','preview_evidence']
            assert events.count('open')==events.count('close')==3
        else:
            with pytest.raises(ValueError):
                execute_and_stage_exposure_preview(artifacts,params,**options)
            if failure=='budget':assert not events and not stages
            else:assert stages==['pixels','working_image','preview_evidence']
