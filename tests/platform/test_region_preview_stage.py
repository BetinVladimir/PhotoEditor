from hashlib import sha256
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.operations.region.presentation import prepare_region_presentation
from photo_worker.operations.region.result import RegionDerivativeManifest,verify_region_encoded
from photo_worker.operations.region.stage import stage_region_result
from photo_worker.runtime.scratch import ScratchSpace
from test_region_preview_presentation import fixture,ROOT,ICC_LOCK


class Artifacts:
    def __init__(self,scratch):self.scratch=scratch;self.outputs={};self.manifest=None
    def stage(self,path,reservation,role,media_type):
        reservation.validate_file(path)
        body=path.read_bytes();ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
        self.outputs[role]=(ref,body,media_type)
        return ref
    def stage_contract(self,value,role):
        self.manifest=value
        body=canonical_bytes(value)
        with self.scratch.reserve(len(body)) as reservation:
            path=reservation.path('region-manifest','.json')
            with self.scratch.open_writer(path,reservation) as writer:writer.write(body)
            return self.stage(path,reservation,role,'application/json')


@pytest.mark.parametrize('alpha',[False,True])
def test_region_stage_encodes_uploads_manifest_and_cleans_scratch(tmp_path,alpha):
    params,body=fixture(alpha=alpha)
    rgb=np.full((300,400,3),.5,np.float32)
    opacity=np.full((300,400),.5,np.float32) if alpha else None
    presentation=prepare_region_presentation(params,body,rgb,opacity,ROOT,runtime_icc_lock_sha=ICC_LOCK)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,8*1024*1024,min_free_bytes=0) as scratch:
        artifacts=Artifacts(scratch)
        result=stage_region_result(artifacts,params,body,presentation)
        assert len(result.outputs)==2
        assert list(artifacts.outputs)==['derivative','document']
        assert result.bytes_written==sum(len(value[1]) for value in artifacts.outputs.values())
        manifest=RegionDerivativeManifest.model_validate_json(artifacts.outputs['document'][1])
        verify_region_encoded(manifest,params,artifacts.outputs['derivative'][1],target_sha256=presentation.profile_sha256,expected_alpha=alpha)
    assert not any(tmp_path.rglob('*.jpg')) and not any(tmp_path.rglob('*.webp'))


def test_invalid_presentation_rejected_before_any_upload(tmp_path):
    params,body=fixture()
    presentation=prepare_region_presentation(params,body,np.zeros((300,400,3),np.float32),None,ROOT,runtime_icc_lock_sha=ICC_LOCK)
    presentation.rgb[0,0,0]=np.nan
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,8*1024*1024,min_free_bytes=0) as scratch:
        artifacts=Artifacts(scratch)
        with pytest.raises(ValueError,match='PRESENTATION'):
            stage_region_result(artifacts,params,body,presentation)
        assert artifacts.outputs=={}


def test_gateway_digest_substitution_cannot_produce_successful_result(tmp_path):
    class WrongDigest(Artifacts):
        def stage(self,path,reservation,role,media_type):
            ref=super().stage(path,reservation,role,media_type)
            if role=='derivative':return ArtifactRef(artifact_id=ref.artifact_id,sha256='f'*64)
            return ref
    params,body=fixture()
    presentation=prepare_region_presentation(params,body,np.zeros((300,400,3),np.float32),None,ROOT,runtime_icc_lock_sha=ICC_LOCK)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,8*1024*1024,min_free_bytes=0) as scratch:
        artifacts=WrongDigest(scratch)
        with pytest.raises(ValueError,match='BYTES'):
            stage_region_result(artifacts,params,body,presentation)
        assert 'document' not in artifacts.outputs


def test_cancel_after_derivative_upload_prevents_manifest_and_cleans(tmp_path):
    params,body=fixture()
    presentation=prepare_region_presentation(params,body,np.zeros((300,400,3),np.float32),None,ROOT,runtime_icc_lock_sha=ICC_LOCK)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,8*1024*1024,min_free_bytes=0) as scratch:
        artifacts=Artifacts(scratch)
        def check():
            if artifacts.outputs:raise RuntimeError('cancelled')
        with pytest.raises(RuntimeError,match='cancelled'):
            stage_region_result(artifacts,params,body,presentation,check_active=check)
        assert 'document' not in artifacts.outputs
    assert not any(tmp_path.rglob('*.jpg'))
