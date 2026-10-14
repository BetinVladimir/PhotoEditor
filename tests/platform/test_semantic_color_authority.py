from copy import deepcopy
import pytest
from photo_worker.operations.semantic.color_authority import semantic_working_color_authority
from photo_worker.v2.adoption import AdoptionInvalid
from test_image_denoise import Artifacts


@pytest.mark.parametrize('variant',['valid','icc','dtype','profile'])
def test_semantic_color_requires_exact_trusted_working_profile(tmp_path,variant):
    image=deepcopy(Artifacts(tmp_path).image)
    verify=semantic_working_color_authority(image.color.icc_sha256)
    if variant=='icc':image.color.icc_sha256='0'*64
    if variant=='dtype':image.dtype='uint16'
    if variant=='profile':
        from photo_worker.contracts.images import ColorSpace
        image.color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256=image.color.icc_sha256)
    if variant=='valid':verify(image)
    else:
        with pytest.raises(AdoptionInvalid,match='WORKING_COLOR_INVALID'):verify(image)


@pytest.mark.parametrize('fingerprint',[None,'A'*64,'0'*63,'z'*64])
def test_color_authority_has_no_unpinned_default(fingerprint):
    with pytest.raises(ValueError):semantic_working_color_authority(fingerprint)
