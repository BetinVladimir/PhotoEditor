import pytest
import json
from pathlib import Path
from photo_worker.operations.portrait.donor_resources import donor_portrait_resources,admit_donor_portrait
from photo_worker.runtime.executor import OperationFailure


def test_combined_scratch_accounts_masks_both_images_and_output():
    dims=(100,200,300,400,40,50);r=donor_portrait_resources(*dims)
    assert r['scratch_bytes']==r['mask_scratch_bytes']+r['material_scratch_bytes']+r['output_scratch_bytes']+64*1024**2
    assert r['material_scratch_bytes']==20*(20_000+120_000)+24576
    assert admit_donor_portrait({'backend':'cpu','renderer_version':'v2.2','resource_estimate':r},*dims)==r


@pytest.mark.parametrize('key',['ram_bytes','scratch_bytes','mask_scratch_bytes','material_scratch_bytes','output_scratch_bytes','output_bytes'])
def test_underfunded_component_rejected(key):
    dims=(100,100,100,100,40,40);r=donor_portrait_resources(*dims);r[key]-=1
    with pytest.raises(OperationFailure):
        admit_donor_portrait({'backend':'cpu','renderer_version':'v2.2','resource_estimate':r},*dims)


@pytest.mark.parametrize('dims',[(True,100,100,100,40,40),(100,100,100,100,101,40),(10000,30000,100,100,40,40)])
def test_unsafe_dimensions_rejected(dims):
    with pytest.raises(ValueError,match='DONOR_DIMENSIONS_INVALID'):donor_portrait_resources(*dims)


def test_backend_worker_shared_resource_fixture():
    path=Path(__file__).resolve().parents[2]/'backend/internal/v2portrait/testdata/donor-resource-parity.json'
    for case in json.loads(path.read_text()):
        assert donor_portrait_resources(*case['dimensions'])==case['expected']
