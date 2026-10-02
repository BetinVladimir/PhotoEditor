import json
from pathlib import Path
import numpy as np
import pytest
from photo_worker.contracts.style import StyleV2,Adaptation,Prototype,Disabled,ModelReference
from photo_worker.algorithms.fitting import identity_color
from photo_worker.algorithms.style import compile_validated_lut,resolve_style
ROOT=Path(__file__).resolve().parents[2]

def profile():
    raw=json.loads((ROOT/'docs/implementation/contracts/examples/style-fixed.example.json').read_text())
    raw['quality']['status']='needs_review'
    return StyleV2.model_validate(raw)

def test_compiled_identity_meets_full_validation():
    result=compile_validated_lut(identity_color())
    assert result.adaptive_allowed and result.lut.size==33
    assert result.attempts[0]['sample_count']==100000
    assert result.attempts[0]['p95_delta_e']<.001

def test_adaptive_ood_and_explicit_group_selection():
    style=profile();lut=compile_validated_lut(identity_color()).lut
    adaptation=Adaptation(feature_version='stats20_v1',mean=[0.]*20,scale=[.05]*20,k=1,temperature=1,ood_threshold=1.,prototypes=[Prototype(id='pair-a',features=[0.]*20,absolute_lut=lut,local=Disabled())],coverage_bins=[True]*512)
    style=style.model_copy(update={'mode':'adaptive','adaptation':adaptation,'models':[ModelReference(id='lraspp_mobilenet_v3_large',weight_id='test')],'strength':0.})
    resolved=resolve_style(style,features=np.ones(20),linear_pixels=np.zeros((8,3)))
    assert resolved.evidence['mode']=='fixed_fallback' and resolved.needs_review
    with pytest.raises(ValueError,match='STYLE_OOD_SKIP'):
        resolve_style(style,features=np.ones(20),linear_pixels=np.zeros((8,3)),strict_ood=True)
    chosen=resolve_style(style,features=np.ones(20),linear_pixels=np.zeros((8,3)),group_prototype_id='pair-a')
    assert chosen.evidence['mode']=='group_fixed' and chosen.evidence['group_prototype_id']=='pair-a'
    pixels=np.full((8,3),.4,np.float32)
    assert np.array_equal(chosen.local_and_strength(pixels,np.zeros_like(pixels),np.ones(8)),pixels)

def test_examples_never_render():
    style=profile().model_copy(update={'quality':profile().quality.model_copy(update={'status':'example'})})
    with pytest.raises(ValueError,match='EXAMPLE_STYLE_FORBIDDEN'):resolve_style(style)


def test_counted_coverage_preserves_exact_ood_fraction_and_group_choice():
    from photo_worker.algorithms.adaptation import ood_reasons,ood_reasons_from_counts
    from photo_worker.algorithms.statistics import coverage_counts_bounded
    from photo_worker.algorithms.color import to_linear
    style=profile();lut=compile_validated_lut(identity_color()).lut
    coverage=[False]*512;coverage[0]=True
    adaptation=Adaptation(feature_version='stats20_v1',mean=[0.]*20,scale=[.05]*20,k=1,temperature=1,ood_threshold=1.,
        prototypes=[Prototype(id='pair-a',features=[0.]*20,absolute_lut=lut,local=Disabled())],coverage_bins=coverage)
    style=style.model_copy(update={'mode':'adaptive','adaptation':adaptation})
    rgb=np.zeros((10,10,3),np.float32);rgb[:3]=1.
    counts=coverage_counts_bounded(rgb,encoding='gamma24')
    expected=ood_reasons(np.zeros(20),to_linear(rgb),adaptation)
    assert expected==ood_reasons_from_counts(np.zeros(20),counts,adaptation)
    assert expected['uncovered_fraction']==.3
    result=resolve_style(style,features=np.zeros(20),coverage_counts=counts)
    assert result.evidence['mode']=='fixed_fallback' and result.needs_review
    chosen=resolve_style(style,features=np.zeros(20),coverage_counts=counts,group_prototype_id='pair-a')
    assert chosen.evidence['mode']=='group_fixed' and chosen.needs_review and not chosen.evidence['fallback']
    with pytest.raises(ValueError,match='AMBIGUOUS'):
        resolve_style(style,features=np.zeros(20),coverage_counts=counts,linear_pixels=rgb)
    for bad in (np.zeros(512),np.zeros(512,np.int64),np.full(512,200000000,np.int64),np.full(512,-1,np.int64)):
        with pytest.raises(ValueError,match='COUNTS'):ood_reasons_from_counts(np.zeros(20),bad,adaptation)


def test_coverage_counts_exclude_alpha_and_keep_frequency():
    from photo_worker.algorithms.statistics import coverage_counts_bounded
    rgb=np.zeros((20,30,3),np.float32);rgb[:5]=1
    alpha=np.full((20,30),65535,np.uint16);alpha[:4]=0
    counts=coverage_counts_bounded(rgb,alpha=alpha)
    assert counts[0]==450 and counts[-1]==30 and counts.sum()==480
    assert not counts.flags.writeable
