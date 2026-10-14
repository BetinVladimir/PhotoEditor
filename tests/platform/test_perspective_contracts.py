import json
from pathlib import Path
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.optical_perspective import OpticalPerspectiveParams,LineSegments
from photo_worker.operations.optical.perspective_resources import perspective_resources,admit_perspective
from photo_worker.runtime.executor import OperationFailure


def intent():
    return dict(schema_version=1,base=dict(asset_version_id=str(uuid4()),canvas_hash='b'*64,
        source=dict(artifact_id=str(uuid4()),sha256='a'*64)),strength=1,mode='manual',
        corners=[dict(x=x,y=y) for x,y in [(0,0),(1,0),(1,1),(0,1)]],lines=None,max_crop_fraction=.5)


def test_contract_matches_normative_required_fields():
    schemas=json.loads((Path(__file__).resolve().parents[2]/'docs/implementation-v2/contracts/schemas.json').read_text())
    for name,contract in [('OpticalPerspectiveParams',OpticalPerspectiveParams),('LineSegments',LineSegments)]:
        assert set(contract.model_fields)==set(schemas['$defs'][name]['required'])
    raw=canonical_bytes(intent())
    assert canonical_bytes(OpticalPerspectiveParams.model_validate_json(raw))==raw


@pytest.mark.parametrize('field,value',[('corners',None),('mode','auto'),('strength',True),('strength',1.1),('max_crop_fraction',-1),('extra',1)])
def test_invalid_intent_refused(field,value):
    data=intent();data[field]=value
    with pytest.raises(ValueError):OpticalPerspectiveParams.model_validate_json(canonical_bytes(data))


def test_line_families_scale_normalized_endpoints():
    data=dict(schema_version=1,lines=[dict(a=dict(x=0,y=y),b=dict(x=1,y=y),family='horizontal') for y in [.2,.8]]+
        [dict(a=dict(x=x,y=0),b=dict(x=x,y=1),family='vertical') for x in [.2,.8]])
    lines=LineSegments.model_validate_json(canonical_bytes(data))
    horizontal,vertical=lines.pixel_families(100,50)
    assert horizontal[0]==[[0,10],[100,10]] and vertical[0]==[[20,0],[20,50]]
    data['lines'][0]['family']='vertical'
    with pytest.raises(ValueError,match='FAMILY_MISSING'):LineSegments.model_validate_json(canonical_bytes(data))


def test_resource_estimate_and_cpu_admission():
    required=perspective_resources(100,80,90,70)
    profile=dict(backend='cpu',renderer_version='v2.2',resource_estimate=required.copy())
    assert admit_perspective(profile,100,80,90,70)==required
    for name in ['ram_bytes','scratch_bytes','output_bytes']:
        bad={**profile,'resource_estimate':{**required,name:required[name]-1}}
        with pytest.raises(OperationFailure):admit_perspective(bad,100,80,90,70)
    for name,value in [('pixel_count',1),('vram_bytes',1),('ram_bytes',True),('scratch_bytes',2**53)]:
        bad={**profile,'resource_estimate':{**required,name:value}}
        with pytest.raises(OperationFailure):admit_perspective(bad,100,80,90,70)
