import pytest
from photo_worker.operations.optical.negative_resources import negative_resources


def test_negative_resource_profile_accounts_alpha_and_retained_documents():
    plain=negative_resources(6000,4000,has_alpha=False)
    alpha=negative_resources(6000,4000,has_alpha=True)
    assert alpha['ram_bytes']-plain['ram_bytes']==4*24000000
    assert alpha['material_scratch_bytes']-plain['material_scratch_bytes']==4*24000000
    assert plain['ram_bytes']>512*1024*1024
    assert alpha['scratch_bytes']==alpha['material_scratch_bytes']+alpha['output_scratch_bytes']
    assert alpha['vram_bytes']==0


@pytest.mark.parametrize('dims,alpha',[((True,1),False),((0,1),False),((200000001,1),False),((1,1),1)])
def test_negative_resource_invalid_shape(dims,alpha):
    with pytest.raises(ValueError,match='NEGATIVE_DIMENSIONS_INVALID'):
        negative_resources(*dims,has_alpha=alpha)


@pytest.mark.parametrize('mode',['ok','ram','sum','pixels','bool','gpu'])
def test_negative_admission_profile(mode):
    from photo_worker.operations.optical.negative_resources import admit_negative
    from photo_worker.runtime.executor import OperationFailure
    estimate=negative_resources(100,100,has_alpha=True)
    profile=dict(renderer_version='v2.2',backend='cpu',resource_estimate=estimate)
    if mode=='ram':estimate['ram_bytes']-=1
    if mode=='sum':estimate['material_scratch_bytes']+=1
    if mode=='pixels':estimate['pixel_count']+=1
    if mode=='bool':estimate['output_bytes']=True
    if mode=='gpu':profile['backend']='cuda'
    if mode=='ok':
        got=admit_negative(profile,100,100,has_alpha=True)
        estimate['ram_bytes']=0
        assert got['ram_bytes']>0
    else:
        with pytest.raises(OperationFailure):admit_negative(profile,100,100,has_alpha=True)



def test_negative_backend_worker_resource_fixture_parity():
    import json
    from pathlib import Path
    path=Path(__file__).resolve().parents[2]/'backend/internal/v2optical/testdata/negative-resource-parity.json'
    for case in json.loads(path.read_text()):
        assert negative_resources(case['Width'],case['Height'],has_alpha=case['Alpha'])==case['Expected']
