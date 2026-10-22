from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,parse_json
import numpy as np
import pytest
from photo_worker.operations.semantic.preview_media_prepare import prepare_exposure_preview_media
from photo_worker.operations.semantic.preview_exposure import PreviewExposureArrays
from test_v27_exposure_preview_binding import fixture
from test_v27_exposure_source import source_fixture


def test_media_prepare_rejects_budget_before_runtime_or_icc_io(tmp_path):
    params,candidate,intent=fixture();edit,image,raw=source_fixture();params['source']=edit['source']
    changed=parse_json(candidate);changed['target']=edit['source'];candidate=canonical_bytes(changed)
    params['candidate']['sha256']=sha256(candidate).hexdigest()
    def forbidden(*args):raise AssertionError('runtime/ICC reached before budget admission')
    arrays=PreviewExposureArrays(np.zeros((1,2,3),np.float32),np.zeros((1,2),np.float32),
        np.zeros((1,2),np.float32),1024,0)
    with pytest.raises(ValueError,match='SEMANTIC_PREVIEW_MEDIA_INVALID'):
        prepare_exposure_preview_media(params,candidate,intent,raw,arrays,arrays.original,None,
            icc_root=tmp_path,icc_lock_sha='a'*64,srgb_sha='b'*64,max_bytes=1,
            verify_admission=forbidden,verify_runtime=forbidden,verify_color=forbidden,check_active=lambda:None)
