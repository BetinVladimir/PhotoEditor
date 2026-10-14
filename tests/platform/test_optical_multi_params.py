from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.optical_multi import OpticalPanoramaParams,OpticalFocusStackParams
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.algorithms.multi_registration import RegisteredPair,assemble_registrations
from photo_worker.operations.optical.focus_prepare import focus_resources


def frames(count):
    return [VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),canvas_hash='b'*64) for _ in range(count)]


def params(count):
    return dict(schema_version=1,frames=frames(count),reference_ordinal=count-1,max_pixels=10000,
                calibration=ArtifactRef(artifact_id=uuid4(),sha256='c'*64))


def test_focus_thirty_frames_are_supported_through_direct_registration():
    value=OpticalFocusStackParams(**params(30),motion_policy='review')
    assert value.reference_ordinal==29
    pairs=[RegisteredPair(i,29,np.eye(3),20,20) for i in range(29)]
    matrices,review=assemble_registrations(pairs,[(100,100)]*30,29,direct=True)
    assert len(matrices)==30 and not review
    assert focus_resources((100,100),30)['vram_bytes']==0


def test_panorama_still_caps_twenty_and_focus_caps_thirty():
    with pytest.raises(ValueError):OpticalPanoramaParams(**params(21),projection='planar')
    with pytest.raises(ValueError):OpticalFocusStackParams(**params(31),motion_policy='reject')


def test_duplicate_source_version_and_out_of_range_reference_rejected():
    value=params(2);value['frames'][1]=value['frames'][0]
    with pytest.raises(ValueError,match='MULTI_SOURCE_DUPLICATE'):OpticalFocusStackParams(**value,motion_policy='review')
    value=params(2);value['reference_ordinal']=2
    with pytest.raises(ValueError,match='MULTI_REFERENCE_INVALID'):OpticalFocusStackParams(**value,motion_policy='review')
