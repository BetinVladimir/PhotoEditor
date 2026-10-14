from copy import deepcopy
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2 import semantic_transfer_authority as module
from photo_worker.v2.adoption import AdoptionInvalid
from test_semantic_transfer_contracts import transfer_documents
from test_image_denoise import Artifacts


@pytest.mark.parametrize('variant',['valid','scene_denied','catalog_changed','icc'])
def test_installed_transfer_authority_rechecks_after_owned_scene_callback(tmp_path,monkeypatch,variant):
    params,intent,calibration=transfer_documents();image=Artifacts(tmp_path).image
    events=[];calls=[]
    monkeypatch.setattr(module,'PostgresObjectEvidenceReader',lambda *args,**kwargs:object())
    def report(*args,**kwargs):return lambda:events.append('report')
    monkeypatch.setattr(module,'semantic_adoption_qualification_loader',report)
    def catalog(*args,**kwargs):
        calls.append(1);owned=deepcopy(image)
        if variant=='catalog_changed' and len(calls)==2:owned.pixels.sha256='0'*64
        return (owned,deepcopy(owned)),()
    monkeypatch.setattr(module,'load_semantic_adoption_catalog',catalog)
    def scene(p,i,c,images,selected,masks):
        events.append('scene');p.model_lock_sha='0'*64
        if variant=='scene_denied':raise AdoptionInvalid('SCENE_DENIED')
    verify=module.sql_semantic_adoption_authority(SimpleNamespace(autocommit=True),object(),'owned',
        working_icc_sha='0'*64 if variant=='icc' else image.color.icc_sha256,verify_scene=scene)
    original=deepcopy(params)
    def run():verify(uuid4(),uuid4(),SimpleNamespace(attempt=2,fence=3,result_id=uuid4()),params,intent,calibration,None,[])
    if variant=='valid':
        run();assert events==['report','scene','report'] and len(calls)==2
    else:
        with pytest.raises(AdoptionInvalid):run()
    assert params==original


def test_scene_verifier_is_mandatory():
    with pytest.raises(ValueError):module.sql_semantic_adoption_authority(SimpleNamespace(autocommit=True),object(),'owned',working_icc_sha='a'*64,verify_scene=None)
