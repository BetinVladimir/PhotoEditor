from hashlib import sha256
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.operations.region.result import build_region_manifest,verify_region_encoded,RegionDerivativeManifest
from photo_worker.operations.region.presentation import prepare_region_presentation,write_region_presentation
from test_region_preview_presentation import fixture,ROOT,ICC_LOCK


@pytest.mark.parametrize('alpha',[False,True])
def test_region_result_binds_actual_encoded_bytes_and_frozen_plan(tmp_path,alpha):
    params,source=fixture(alpha=alpha)
    rgb=np.full((300,400,3),.5,np.float32)
    opacity=np.full((300,400),.5,np.float32) if alpha else None
    presentation=prepare_region_presentation(params,source,rgb,opacity,ROOT,runtime_icc_lock_sha=ICC_LOCK)
    path=tmp_path/('region.webp' if alpha else 'region.jpg')
    size=write_region_presentation(path,presentation)
    body=path.read_bytes()
    ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
    manifest=build_region_manifest(params,presentation,ref,size)
    parsed=RegionDerivativeManifest.model_validate_json(canonical_bytes(manifest))
    verify_region_encoded(parsed,params,body,target_sha256=presentation.profile_sha256,expected_alpha=alpha)
    with pytest.raises(ValueError,match='BYTES'):
        verify_region_encoded(parsed,params,body+b'!',target_sha256=presentation.profile_sha256,expected_alpha=alpha)
    with pytest.raises(ValueError,match='BINDING'):
        verify_region_encoded(parsed,params,body,target_sha256='f'*64,expected_alpha=alpha)
    parsed.geometry.output_width-=1
    with pytest.raises(ValueError,match='BINDING'):
        verify_region_encoded(parsed,params,body,target_sha256=presentation.profile_sha256,expected_alpha=alpha)


def test_result_cannot_adopt_jpeg_with_generated_exif(tmp_path):
    from photo_worker.operations.media.derivative import _write
    params,source=fixture()
    presentation=prepare_region_presentation(params,source,np.zeros((300,400,3),np.float32),None,ROOT,runtime_icc_lock_sha=ICC_LOCK)
    path=tmp_path/'legacy.jpg'
    _write(path,presentation.rgb,None,presentation.profile,88)
    body=path.read_bytes()
    ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
    manifest=build_region_manifest(params,presentation,ref,len(body))
    with pytest.raises(ValueError,match='ENCODING'):
        verify_region_encoded(manifest,params,body,target_sha256=presentation.profile_sha256,expected_alpha=False)
