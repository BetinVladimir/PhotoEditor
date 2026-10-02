from pathlib import Path
import copy
import json

import numpy as np
import pytest
from pydantic import ValidationError
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.style import StyleV2,LUT,Adaptation,Prototype,Disabled
from photo_worker.algorithms.color import (apply_global,apply_local,apply_series,apply_strength,identity_lut,trilinear,
    render_color_tiled,rgb_to_lab_d50,ColorDiagnostics,compile_absolute_lut)
from photo_worker.algorithms.adaptation import compute_stats20,fit_normalizer,neighbors,blend,ood_reasons,farthest_prototypes
from photo_worker.algorithms.tiles import plan_tiles,sample_map,Rect,guided_filter

ROOT=Path(__file__).resolve().parents[2]
@pytest.fixture
def raw_style():
    return json.loads((ROOT/'docs/implementation/contracts/examples/style-fixed.example.json').read_text())
@pytest.fixture
def style(raw_style):return StyleV2.model_validate(raw_style)


def test_identity_and_tiling(style):
    rgb=np.random.default_rng(42).random((91,137,3),dtype=np.float32)
    output=apply_global(rgb,style.global_)
    assert np.max(np.abs(output-rgb))<=2/65535
    assert np.array_equal(render_color_tiled(rgb,style.global_,17),output)


def test_trilinear_boundary_and_layout():
    grid=identity_lut(33)
    p=np.array([[0,0,0],[1,1,1],[1,.2,0],[.12,.73,.39]],np.float32)
    assert np.allclose(trilinear(grid,p),p,atol=2e-7)
    nonlinear=grid**2
    assert np.allclose(trilinear(nonlinear,grid),nonlinear,atol=1e-7)


def test_linear_strength_and_no_hidden_wb(style):
    rgb=np.array([[[.1,.3,.7],[.9,.5,.2]]],np.float32)
    out=apply_global(rgb,style.global_)
    assert np.allclose(out,rgb,atol=2e-7)
    assert np.array_equal(apply_strength(rgb,out,0),rgb)
    half=apply_strength(np.zeros_like(rgb),np.ones_like(rgb),.5)
    assert np.allclose(half,.5**(1/2.4))


def test_series_correction_is_linear_light_and_bounded():
    rgb=np.array([[[.25,.5,.75]]],np.float32)
    out=apply_series(rgb,.25,[1.05,.95,1.])
    expected=np.power(np.clip(np.power(rgb,2.4)*(2**.25)*np.array([1.05,.95,1.]),0,1),1/2.4)
    assert np.allclose(out,expected,atol=2e-7)
    with pytest.raises(ValueError):apply_series(rgb,.36,[1.,1.,1.])
    with pytest.raises(ValueError):apply_series(rgb,0,[1.06,1.,1.])


def test_clipping_is_diagnosed(style):
    style.global_.gains_rgb=[4,4,4]
    diag=ColorDiagnostics()
    apply_global(np.ones((2,2,3),np.float32),style.global_,diag)
    assert diag.clipping['gains']['above_one']==1


@pytest.mark.parametrize('mutation',[
    lambda x:x.update(schema_version='1.0.0'),
    lambda x:x['global'].update(master_curve=[0,.5,.2,.3,.5,.7,1]),
    lambda x:x['global']['residual_lut'].update(values=[0,0]),
    lambda x:x['global']['residual_lut'].update(values=[0,0,float('nan')]),
    lambda x:x.update(mode='adaptive'),
    lambda x:x['extensions'].update(unknown_effect={'enabled':True}),
    lambda x:x['quality'].update(transfer_confidence='validated'),
])
def test_invalid_style_rejected(raw_style,mutation):
    mutation(raw_style)
    with pytest.raises(ValidationError):StyleV2.model_validate(raw_style)


def test_example_gate_and_canonical_roundtrip(style):
    data=style.canonical()
    with pytest.raises(ValueError):StyleV2.from_bytes(data)
    restored=StyleV2.from_bytes(data,allow_example=True)
    assert restored.config_hash==style.config_hash
    assert restored.canonical()==data


@pytest.mark.parametrize('raw',[b'{"x":1,"x":2}',b'{"a":NaN}',b'{"n":1e999}',b'{"x":"\\ud800"}'])
def test_unsafe_json(raw):
    with pytest.raises(ValueError):parse_json(raw)


def test_jcs_reference_numbers():
    assert canonical_bytes({'b':-0.0,'a':1e-7,'c':1e20})==b'{"a":1e-7,"b":0,"c":100000000000000000000}'


def test_lab_black_and_white():
    lab=rgb_to_lab_d50(np.array([[0,0,0],[1,1,1]],float))
    assert np.allclose(lab[0],0,atol=1e-8)
    assert np.allclose(lab[1],[100,0,0],atol=.03)


def test_stats_vector_and_alpha_exclusion():
    rgb=np.full((8,8,3),.5);mask=np.ones((8,8))
    out=compute_stats20(rgb,mask)
    assert out.values.shape==(20,)
    assert np.allclose(out.values[:8],.5)
    assert out.values[15]==1
    assert out.coverage_bins.sum()==1
    valid=np.zeros((8,8),bool);valid[0,0]=True
    assert np.allclose(compute_stats20(rgb,mask,valid).values,out.values)
    with pytest.raises(ValueError):compute_stats20(rgb,mask,np.zeros((8,8),bool))


def make_adaptation(style):
    lut=compile_absolute_lut(style.global_)
    return Adaptation(feature_version='stats20_v1',mean=[0]*20,scale=[.05]*20,k=2,temperature=1,ood_threshold=1,
        prototypes=[Prototype(id='b',features=[0]*20,absolute_lut=lut,local=Disabled()),
                    Prototype(id='a',features=[0]*20,absolute_lut=lut,local=Disabled())],coverage_bins=[True]*512)


def test_neighbor_tie_and_ood(style):
    adaptation=make_adaptation(style)
    selected=neighbors(np.zeros(20),adaptation)
    assert [(n.prototype_id,n.weight) for n in selected]==[('a',1)]
    grid,local=blend(adaptation,selected)
    assert np.allclose(grid,identity_lut(33),atol=2e-7)
    assert local.person_ev==0 and local.background_saturation==1
    assert ood_reasons(np.ones(20),np.zeros((8,3)),adaptation)['fallback']


def test_farthest_selection_independent_of_input_order():
    ids=['b','c','a'];features=np.asarray([[1]*20,[2]*20,[0]*20],float)
    selected=farthest_prototypes(ids,features,2)
    assert [ids[i] for i in selected]==['a','c']
    perm=[2,0,1];other=[ids[i] for i in perm]
    assert [other[i] for i in farthest_prototypes(other,features[perm],2)]==['a','c']


def test_tiling_covers_once_and_map_coordinates():
    count=np.zeros((27,39),int);source=np.arange(63,dtype=np.float32).reshape(7,9)
    full=sample_map(source,Rect(0,0,39,27),39,27)
    output=np.empty_like(full)
    for tile in plan_tiles(39,27,8,3):
        count[tile.core.slices]+=1
        output[tile.core.slices]=sample_map(source,tile.core,39,27)
    assert (count==1).all()
    assert np.array_equal(full,output)


def test_guided_filter_tiled_halo():
    rng=np.random.default_rng(8);guide=rng.random((80,90));signal=rng.random((80,90,3))
    full=guided_filter(guide,signal,3,.001);tiled=np.empty_like(full)
    for tile in plan_tiles(90,80,17,6):
        value=guided_filter(guide[tile.read.slices],signal[tile.read.slices],3,.001)
        tiled[tile.core.slices]=value[tile.core_in_read.slices]
    assert np.max(np.abs(full-tiled))<2/65535
