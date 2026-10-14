from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.operations.semantic.exposure_execution import execute_and_stage_local_exposure
from test_v27_exposure_pipeline import inputs


def test_computation_to_staged_manifest(tmp_path):
    edit, root, artifacts, events, _, _ = inputs(tmp_path)
    uploads = []
    def stage(path, reservation, role, media):
        actual = np.load(path, allow_pickle=False)
        np.testing.assert_array_equal(actual[:, :4], np.full((8,4,3), .5, np.float32))
        np.testing.assert_allclose(actual[:, 4:], .5*2**(.25*.5*.5/2.4), rtol=1e-6)
        uploads.append(role)
        return ArtifactRef(artifact_id=uuid4(), sha256='f'*64)
    def stage_contract(image, role):
        assert image.pixels.sha256 == 'f'*64
        uploads.append(role)
        return ArtifactRef(artifact_id=uuid4(), sha256='e'*64)
    artifacts.stage = stage
    artifacts.stage_contract = stage_contract
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 2*1024**2, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        result = execute_and_stage_local_exposure(artifacts, edit, protection_ref=root,
            verify_origin=lambda *args: True, verify_color=lambda color: True,
            verify_admission=lambda edit: True, check_active=lambda: None,
            max_bytes=64*1024**2+204*64, max_output_bytes=2*1024**2)
        assert result.bytes_written > 12*64
    assert uploads == ['pixels', 'working_image']
    assert events.count('open') == events.count('close') == 4


def test_output_budget_denies_before_pixel_io(tmp_path):
    edit, root, artifacts, events, _, _ = inputs(tmp_path)
    with pytest.raises(ValueError, match='RESOURCE_PROFILE'):
        execute_and_stage_local_exposure(artifacts, edit, protection_ref=root,
            verify_origin=lambda *args: True, verify_color=lambda color: True,
            verify_admission=lambda edit: True, check_active=lambda: None,
            max_bytes=64*1024**2+204*64, max_output_bytes=12*64+16384+1048576-1)
    assert events == []


def test_measured_image_result_publication_with_real_scratch(tmp_path):
    from photo_worker.operations.semantic.exposure_execution import execute_and_publish_local_exposure
    edit,root,artifacts,events,_,_=inputs(tmp_path);roles=[];reports=[]
    def stage(path,reservation,role,media):
        assert role=='pixels' and media=='application/x-npy'
        values=np.load(path,allow_pickle=False)
        np.testing.assert_array_equal(values[:,:4],np.full((8,4,3),.5,np.float32))
        roles.append(role)
        return ArtifactRef(artifact_id=uuid4(),sha256='f'*64)
    def stage_contract(value,role):
        roles.append(role)
        if role=='report':
            assert value.status=='pass' and value.outside_mask_max_error==0
            reports.append(value)
        if role=='document':assert value.report.sha256=='e'*64 and value.generated_mask is None
        return ArtifactRef(artifact_id=uuid4(),sha256='e'*64)
    artifacts.stage=stage;artifacts.stage_contract=stage_contract
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,2*1024**2,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch
        result=execute_and_publish_local_exposure(artifacts,edit,protection_ref=root,
            verify_origin=lambda *args:True,verify_color=lambda color:True,
            verify_admission=lambda edit:True,check_active=lambda:None,
            max_bytes=64*1024**2+204*64,max_output_bytes=2*1024**2)
        assert result.bytes_written>12*64 and result.review_required is False
    assert roles==['pixels','working_image','report','document'] and len(reports)==1
    assert events.count('open')==events.count('close')==8
