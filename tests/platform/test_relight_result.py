import json
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_hash,canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.relight import ImageRelightParams
from photo_worker.operations.portrait.relight_result import stage_relight_result


@pytest.mark.parametrize('alpha',[False,True])
def test_relight_staging_preserves_lineage_and_policy(tmp_path,alpha):
    root=Path(__file__).resolve().parents[2]
    source=WorkingImage.model_validate_json(json.dumps(json.loads((root/'backend/internal/review/testdata/working-images-python.json').read_text())[0]))
    if alpha:source=source.model_copy(update={'alpha':ArtifactRef(artifact_id=uuid4(),sha256='d'*64)})
    ref=lambda digest='a'*64:dict(artifact_id=str(uuid4()),sha256=digest)
    params=ImageRelightParams.model_validate_json(json.dumps({'schema_version':1,'base':{'asset_version_id':str(uuid4()),'source':ref(canonical_hash(source)),'canvas_hash':canonical_hash(source.canvas)},'strength':.7,'depth':ref(),'subject':ref(),'protected':ref(),'lights':[{'x':.5,'y':.5,'radius':.2,'ev':1.}],'calibration':ref()}))
    rgb=tmp_path/'rgb.npy';mask=tmp_path/'mask.npy'
    np.save(rgb,np.zeros((source.canvas.height,source.canvas.width,3),np.float32))
    np.save(mask,np.zeros((source.canvas.height,source.canvas.width),np.float32))
    files=SimpleNamespace(rgb=rgb,changed_mask=mask,review_required=True,reservation=SimpleNamespace(validate=lambda:None))
    class Artifacts:
        def __init__(self):self.roles={};self.written=0
        def stage(self,path,reservation,role,mime):
            assert mime=='application/x-npy'
            pin=ArtifactRef(artifact_id=uuid4(),sha256='b'*64);self.roles[role]=(pin,path);self.written+=path.stat().st_size;return pin
        def stage_contract(self,value,role):
            pin=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(value));self.roles[role]=(pin,value);self.written+=len(canonical_bytes(value));return pin
    artifacts=Artifacts();staged=stage_relight_result(artifacts,params,source,files)
    image=artifacts.roles['working_image'][1];result=artifacts.roles['document'][1];report=artifacts.roles['report'][1]
    assert image.canvas==source.canvas and image.transform_chain==source.transform_chain and image.color==source.color and image.alpha==source.alpha
    assert image.pixels==artifacts.roles['pixels'][0] and result.working_image==artifacts.roles['working_image'][0]
    assert result.report==artifacts.roles['report'][0] and result.changed_mask==artifacts.roles['mask'][0] and result.generated_mask is None
    assert report.operation=='image.relight' and report.status=='review' and report.sources==[params.base]
    assert staged.review_required and staged.document==artifacts.roles['document'][0] and staged.bytes_written==artifacts.written
    policies=json.loads((root/'docs/implementation-v2/contracts/operations.json').read_text())['operations']
    policy=next(p for p in policies if p['id']=='image.relight')['role_policies']
    assert set(artifacts.roles)=={'pixels','working_image','mask_pixels','mask','report','document'}
    assert all(role in artifacts.roles for role,rule in policy.items() if rule['required'])
    bad=params.model_copy(update={'base':params.base.model_copy(update={'canvas_hash':'f'*64})})
    rejected=Artifacts()
    with pytest.raises(ValueError,match='RELIGHT_SOURCE_MISMATCH'):stage_relight_result(rejected,bad,source,files)
    assert not rejected.roles
    changed_source=source.model_copy(update={'pixels':ArtifactRef(artifact_id=uuid4(),sha256='e'*64)})
    with pytest.raises(ValueError,match='RELIGHT_SOURCE_MISMATCH'):stage_relight_result(rejected,params,changed_source,files)
    assert not rejected.roles
    files.review_required=False
    with pytest.raises(ValueError,match='RELIGHT_REVIEW_MISMATCH'):stage_relight_result(rejected,params,source,files)
    assert not rejected.roles
