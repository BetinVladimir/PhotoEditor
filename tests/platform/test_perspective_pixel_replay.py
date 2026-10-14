from uuid import uuid4
import numpy as np
import pytest
from test_perspective_prepare import source
from photo_worker.algorithms.perspective import manual_perspective
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.operations.optical.perspective_prepare import prepare_source_perspective
from photo_worker.v2.perspective_adoption import verify_perspective_prepared_arrays


@pytest.mark.parametrize('change',[None,'rgb','alpha','mask','statistics'])
def test_physical_replay_detects_individual_output_substitution(tmp_path,change):
    image=source();rgb=np.arange(36,dtype=np.float32).reshape(3,4,3)/20
    geometry=manual_perspective([[0,0],[1,0],[1,1],[0,1]],4,3,max_crop_fraction=0,max_pixels=12)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1000000,min_free_bytes=0) as scratch:
        with prepare_source_perspective(image,rgb,None,geometry,scratch) as files:
            after=np.load(files.rgb);alpha=np.load(files.alpha);changed=np.load(files.changed_mask)
            clipping=files.clipping_fraction
            if change=='rgb':after[1,1,0]=np.nextafter(after[1,1,0],np.float32(np.inf))
            if change=='alpha':alpha[1,1]=.5
            if change=='mask':changed[1,1]=1
            if change=='statistics':clipping=.123
            if change is None:
                verify_perspective_prepared_arrays(files,after,alpha,changed,clipping_fraction=clipping,check_active=lambda:None)
            else:
                with pytest.raises(ValueError):
                    verify_perspective_prepared_arrays(files,after,alpha,changed,clipping_fraction=clipping,check_active=lambda:None)
