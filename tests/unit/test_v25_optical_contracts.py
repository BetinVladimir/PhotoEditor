"""Executable optical contracts agree with normative schema fixtures."""
import json
from pathlib import Path
from uuid import uuid4
import pytest
from jsonschema import Draft202012Validator,FormatChecker
from photo_worker.operations.optical.contracts import OpticalLensParams,ImageReport,ImageResult,LensProfile

ROOT=Path(__file__).resolve().parents[2]
SCHEMAS=json.loads((ROOT/'docs/implementation-v2/contracts/schemas.json').read_text())


def ref():return {'artifact_id':str(uuid4()),'sha256':'a'*64}


@pytest.mark.parametrize('name,model,value',[
    ('OpticalLensParams',OpticalLensParams,{'schema_version':1,'base':{'asset_version_id':str(uuid4()),'source':ref(),'canvas_hash':'b'*64},'strength':.5,'profile':ref(),'focal_mm':50,'aperture':2}),
    ('ImageResult',ImageResult,{'schema_version':1,'working_image':ref(),'report':ref(),'changed_mask':ref(),'generated_mask':None}),
    ('ImageReport',ImageReport,{'schema_version':1,'status':'review','operation':'optical.lens','skipped_count':0,'reason':'uncovered_area','outside_mask_max_error':0,'clipping_fraction':0,'identity':None,'transform':None,'sources':[{'asset_version_id':str(uuid4()),'source':ref(),'canvas_hash':'b'*64}],'registrations':[]}),
    ('LensProfile',LensProfile,{'schema_version':1,'camera_model':'Camera','lens_model':'Lens','focal_mm':50,'aperture':2,'cx':.5,'cy':.5,'k':[0,0,0],'p':[0,0],'vignette':[0,0,0],'ca_red_scale':1,'ca_blue_scale':1,'qualification':ref()}),
])
def test_normative_schema_roundtrip_and_unknown_field_rejection(name,model,value):
    validator=Draft202012Validator({**SCHEMAS,'$ref':f'#/$defs/{name}'},format_checker=FormatChecker())
    validator.validate(value)
    parsed=model.model_validate_json(json.dumps(value))
    validator.validate(parsed.model_dump(mode='json'))
    invalid={**value,'unexpected':True}
    assert not validator.is_valid(invalid)
    with pytest.raises(ValueError):model.model_validate_json(json.dumps(invalid))
