from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.operations.semantic.exposure_stage import stage_local_exposure_result
from test_v27_exposure_source import source_fixture


def test_stage_preserves_working_metadata_and_real_npy(tmp_path):
    edit,image,source=source_fixture();staged=[]
    class Artifacts:
        def stage(self,path,reservation,role,media):
            assert role == 'pixels' and media == 'application/x-npy'
            values=np.load(path,allow_pickle=False)
            np.testing.assert_array_equal(values,np.full((1,2,3),1.2,np.float32))
            staged.append(role);return ArtifactRef(artifact_id=uuid4(),sha256='f'*64)
        def stage_contract(self,result,role):
            assert result.canvas.model_dump(mode='json')==image['canvas']
            assert result.color.model_dump(mode='json')==image['color']
            assert result.alpha is None
            staged.append(role);return ArtifactRef(artifact_id=uuid4(),sha256='e'*64)
    artifacts=Artifacts()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,2*1024**2,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch
        result=stage_local_exposure_result(artifacts,edit,source,np.full((1,2,3),1.2,np.float32),verify_admission=lambda edit:True,check_active=lambda:None,max_bytes=128*1024**2,max_output_bytes=2*1024**2)
        assert result.bytes_written>24
    assert staged==['pixels','working_image']


def test_denied_stage_never_uploads(tmp_path):
    edit,_,source=source_fixture()
    with pytest.raises(ValueError,match='ADMISSION_DENIED'):
        stage_local_exposure_result(object(),edit,source,np.ones((1,2,3),np.float32),verify_admission=lambda edit:False,check_active=lambda:None,max_bytes=128*1024**2,max_output_bytes=2*1024**2)


@pytest.mark.parametrize('failure',['upload','mutate_manifest','zero_pixel','zero_manifest'])
def test_upload_failure_or_output_substitution_never_returns_stage(tmp_path,failure):
    from uuid import UUID
    edit,_,source=source_fixture();paths=[]
    class Artifacts:
        def stage(self,path,reservation,role,media):
            paths.append(path)
            if failure=='upload':raise RuntimeError('upload failed')
            return ArtifactRef(artifact_id=UUID(int=0) if failure=='zero_pixel' else uuid4(),sha256='f'*64)
        def stage_contract(self,result,role):
            if failure=='mutate_manifest':result.color.icc_sha256='0'*64
            return ArtifactRef(artifact_id=UUID(int=0) if failure=='zero_manifest' else uuid4(),sha256='e'*64)
    artifacts=Artifacts()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,2*1024**2,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch
        with pytest.raises((RuntimeError,ValueError)):
            stage_local_exposure_result(artifacts,edit,source,np.ones((1,2,3),np.float32),verify_admission=lambda edit:True,check_active=lambda:None,max_bytes=128*1024**2,max_output_bytes=2*1024**2)
        assert all(not path.exists() for path in paths)
