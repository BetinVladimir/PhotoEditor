from types import SimpleNamespace
import numpy as np
import pytest
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.portrait.donor_result import stage_donor_result
from photo_worker.operations.portrait.donor_files import DonorPortraitFiles
from test_portrait_local_result import Artifacts
from test_portrait_donor_images import manifest_arguments


@pytest.mark.parametrize('warning',[None,'COLOR_MATCH_UNAVAILABLE'])
def test_donor_result_preserves_source_metadata_and_requires_review(tmp_path,warning):
    args=manifest_arguments('gamma24');params,raw=args[:2]
    source=WorkingImage.model_validate_json(raw)
    rgb,changed=tmp_path/'rgb.npy',tmp_path/'changed.npy'
    np.save(rgb,np.ones((100,100,3),np.float32))
    np.save(changed,np.zeros((100,100),np.float32))
    files=DonorPortraitFiles(rgb,changed,SimpleNamespace(validate=lambda:None))
    artifacts=Artifacts()
    result=stage_donor_result(artifacts,params,raw,files,color_warning=warning,check_active=lambda:None)
    image=artifacts.roles['working_image'][1]
    assert image.alpha==source.alpha and image.color==source.color
    assert image.canvas==source.canvas and image.transform_chain==source.transform_chain
    assert len(result.outputs)==6 and result.review_required
    report=artifacts.roles['report'][1]
    assert report.status=='review' and report.sources==[params.base,params.donor]
    assert report.reason==(warning or 'donor_review')
    assert artifacts.roles['document'][1].generated_mask is None
    assert result.document==artifacts.roles['document'][0]


def test_changed_source_manifest_rejects_before_staging():
    args=manifest_arguments();artifacts=Artifacts()
    files=DonorPortraitFiles(None,None,None)
    with pytest.raises(ValueError,match='DONOR_IMAGE_SHA'):
        stage_donor_result(artifacts,args[0],args[1]+b' ',files,
            color_warning=None,check_active=lambda:None)
    assert not artifacts.roles
