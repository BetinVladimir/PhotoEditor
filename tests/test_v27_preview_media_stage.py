from hashlib import sha256
import numpy as np
import pytest
from photo_worker.operations.semantic.preview_media_stage import stage_exposure_preview_media
from photo_worker.operations.semantic.preview_presentation import ExposureDisplay


class NoIO:
    @property
    def scratch(self):raise AssertionError('scratch IO before admission')


def inputs():
    profile=b'fixture';digest=sha256(profile).hexdigest()
    image=ExposureDisplay(np.zeros((4,4,3),dtype=np.float32),None,profile,digest)
    return dict(before=image,after=image,affected=np.zeros((4,4,4),dtype=np.uint8),
        protected=np.zeros((4,4,4),dtype=np.uint8),forbidden_ids=set(),srgb_sha=digest,
        max_bytes=128*1024**2,max_output_bytes=4*1024**2,
        verify_runtime=lambda:True,verify_admission=lambda:True,check_active=lambda:None)


@pytest.mark.parametrize('accepted',[False,None,1])
def test_media_requires_explicit_admission_before_scratch_io(accepted):
    args=inputs();args['verify_admission']=lambda:accepted
    with pytest.raises(ValueError,match='ADMISSION_DENIED'):
        stage_exposure_preview_media(NoIO(),**args)


def test_media_admits_total_four_file_budget_before_any_io():
    args=inputs();args['max_output_bytes']=1
    with pytest.raises(ValueError,match='BUDGET'):
        stage_exposure_preview_media(NoIO(),**args)
