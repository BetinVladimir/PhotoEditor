import json
from pathlib import Path
import numpy as np
import pytest
import torch
from photo_worker.contracts.style import StyleV2
from photo_worker.algorithms import torch_color as tc
from photo_worker.algorithms.color import apply_global,expand_lut,rgb_to_lab_d50
from photo_worker.algorithms.sampling import split_blocks,sample_pixels
ROOT=Path(__file__).resolve().parents[2]

@pytest.mark.parametrize('device',['cpu','mps'])
def test_reference_and_finite_autograd(device):
    if device=='mps' and not torch.backends.mps.is_available():pytest.skip('MPS unavailable')
    style=StyleV2.model_validate(json.loads((ROOT/'docs/implementation/contracts/examples/style-fixed.example.json').read_text()))
    config=style.global_.model_copy(update={'gains_rgb':[1.1,.95,.9],'saturation':1.2})
    pixels=np.random.default_rng(42).random((32,32,3),dtype=np.float32)
    pixels[0,0]=0;pixels[0,1]=1
    convert=lambda x:torch.tensor(x,dtype=torch.float32,device=device)
    rgb=convert(pixels).requires_grad_()
    params=[convert(config.gains_rgb).requires_grad_(),convert(config.master_curve).requires_grad_(),convert(config.rgb_curves).requires_grad_(),convert(config.saturation).requires_grad_(),convert(expand_lut(config.residual_lut)).requires_grad_()]
    output=tc.apply_global(rgb,*params)
    reference=apply_global(pixels,config)
    assert np.max(np.abs(output.detach().cpu().numpy()-reference))<=2/65535
    lab=tc.rgb_to_lab_d50(output)
    reference_lab=rgb_to_lab_d50(output.detach().cpu().numpy())
    assert np.max(np.abs(lab.detach().cpu().numpy()-reference_lab))<.001
    loss=(output.mean()+lab.square().mean()/10000)
    loss.backward()
    assert all(p.grad is not None and torch.isfinite(p.grad).all().item() for p in [rgb,*params])

def test_block_holdout_and_sampling():
    valid=np.ones((128,192),dtype=bool);valid[:16,:16]=False
    split=split_blocks(valid,19)
    repeat=split_blocks(valid,19)
    assert split.digest==repeat.digest
    assert not np.any(split.train&split.holdout)
    assert np.array_equal(split.train|split.holdout,valid)
    assert .17<split.holdout.sum()/valid.sum()<.23
    pixels=np.random.default_rng(5).random((128,192,3),dtype=np.float32)
    train=sample_pixels(pixels,split.train,9,2000)
    holdout=sample_pixels(pixels,split.holdout,10,1000)
    assert len(set(train.indices))==2000
    assert len(np.intersect1d(train.indices,holdout.indices))==0
    assert train.digest==sample_pixels(pixels,split.train,9,2000).digest

def test_insufficient_spatial_support():
    valid=np.zeros((128,128),bool);valid[:8,:8]=True
    with pytest.raises(ValueError,match='INSUFFICIENT_SPATIAL_COVERAGE'):split_blocks(valid,1)
