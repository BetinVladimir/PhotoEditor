import numpy as np
import pytest
from photo_worker.algorithms.relight import build_light_field
from photo_worker.contracts.relight import RelightLight


def light(**kwargs):
    return RelightLight(x=.5,y=.5,radius=.2,ev=1.,**kwargs)


def test_center_golden_zero_ev_and_depth_discontinuity():
    depth=np.zeros((3,3),np.float32);invalid=np.zeros_like(depth,dtype=bool)
    field=build_light_field(depth,invalid,[light()],.1)
    assert field[1,1]==1 and np.exp2(field[1,1])==2
    zero=light().model_copy(update={'ev':0.})
    np.testing.assert_array_equal(build_light_field(depth,invalid,[zero],.1),np.zeros_like(depth))
    depth[1,2]=1
    assert build_light_field(depth,invalid,[light()],.1)[1,2]<1e-20


def test_invalid_anchor_nearest_tie_row_col_and_invalid_zero():
    depth=np.zeros((3,3),np.float32);depth[0,1]=.2;depth[1,0]=.8
    invalid=np.ones((3,3),bool);invalid[0,1]=False;invalid[1,0]=False
    field=build_light_field(depth,invalid,[light()],.01)
    assert field[0,1]>0 and field[1,0]==0
    np.testing.assert_array_equal(field[invalid],np.zeros(invalid.sum(),np.float32))


def test_bilinear_anchor_and_clamp_multiple_lights():
    depth=np.array([[0,1],[0,1]],np.float32);invalid=np.zeros_like(depth,dtype=bool)
    center=light()
    field=build_light_field(depth,invalid,[center],1.)
    expected=np.exp(-.5/(2*.2**2))*np.exp(-.25/2)
    np.testing.assert_allclose(field,expected,rtol=1e-6)
    uniform=np.zeros((1,1),np.float32)
    at_origin=center.model_copy(update={'x':0.,'y':0.})
    assert build_light_field(uniform,np.zeros((1,1),bool),[at_origin]*4,.1)[0,0]==1


def test_no_valid_depth_and_valid_nan_refused():
    depth=np.full((2,2),np.nan,np.float32)
    with pytest.raises(ValueError,match='DEPTH_UNAVAILABLE'):build_light_field(depth,np.ones((2,2),bool),[light()],.1)
    with pytest.raises(ValueError,match='RELIGHT_DEPTH_INVALID'):build_light_field(depth,np.zeros((2,2),bool),[light()],.1)


def test_scratch_output_blocks_and_cancellation(tmp_path):
    depth=np.zeros((257,513),np.float32);invalid=np.zeros(depth.shape,bool)
    out=np.lib.format.open_memmap(tmp_path/'field.npy',mode='w+',dtype=np.float32,shape=depth.shape)
    try:
        assert build_light_field(depth,invalid,[light()],.1,out=out) is out
        assert np.isfinite(out).all()
        with pytest.raises(ValueError,match='RELIGHT_OUTPUT_INVALID'):build_light_field(depth,invalid,[light()],.1,out=depth)
        def cancelled():raise RuntimeError('cancelled')
        with pytest.raises(RuntimeError,match='cancelled'):build_light_field(depth,invalid,[light()],.1,out=out,check_active=cancelled)
    finally:out._mmap.close()


def test_hdr_proposal_and_protected_composition():
    from photo_worker.algorithms.relight import relight_proposal
    from photo_worker.algorithms.portrait_local import compose_local
    source=np.full((3,3,3),2.,np.float32)
    proposal=relight_proposal(source,np.ones((3,3),np.float32))
    np.testing.assert_array_equal(proposal,np.full_like(source,4))
    target=np.ones((3,3),np.float32);target[0]=0
    protected=np.zeros((3,3),np.float32);protected[:,0]=1
    result=compose_local(source,proposal,target,protected,1.)
    np.testing.assert_array_equal(result[(target==0)|(protected==1)],source[(target==0)|(protected==1)])
    np.testing.assert_array_equal(relight_proposal(source,np.zeros((3,3),np.float32)),source)
    with pytest.raises(ValueError,match='RELIGHT_OVERFLOW'):
        relight_proposal(np.full_like(source,np.finfo(np.float32).max),np.ones((3,3),np.float32))


def test_relight_contract_matches_documented_schema():
    import json
    from uuid import uuid4
    from pathlib import Path
    import jsonschema
    from photo_worker.contracts.relight import ImageRelightParams
    ref=lambda:{'artifact_id':str(uuid4()),'sha256':'a'*64}
    body={'schema_version':1,'base':{'asset_version_id':str(uuid4()),'source':ref(),'canvas_hash':'b'*64},'strength':.7,'depth':ref(),'subject':ref(),'protected':ref(),'lights':[{'x':.5,'y':.5,'radius':.2,'ev':1.}],'calibration':ref()}
    schema=json.loads((Path(__file__).resolve().parents[2]/'docs/implementation-v2/contracts/schemas.json').read_text())
    jsonschema.Draft202012Validator({'$defs':schema['$defs'],'$ref':'#/$defs/ImageRelightParams'}).validate(body)
    parsed=ImageRelightParams.model_validate_json(json.dumps(body))
    assert parsed.lights[0].ev==1
    for key,value in [('radius',0),('x',3),('ev',float('nan'))]:
        bad=dict(body);bad['lights']=[dict(body['lights'][0],**{key:value})]
        with pytest.raises(ValueError):ImageRelightParams.model_validate_json(json.dumps(bad))
