import numpy as np
import torch
from photo_worker.algorithms.fitting import PairSamples,FitOptions,fit_color,identity_color,reconstruction_metrics
from photo_worker.algorithms.color import apply_global

def test_exact_identity_has_no_hidden_correction():
    x=np.random.default_rng(12).random((64,3),dtype=np.float32)
    result=fit_color([PairSamples('one',x,x,x[::-1],x[::-1])])
    assert result.global_color==identity_color()
    assert result.holdout_loss==0 and not result.local.enabled

def test_gain_fit_improves_independent_holdout():
    torch.set_num_threads(2)
    rng=np.random.default_rng(18)
    before=rng.uniform(.12,.72,(512,3)).astype(np.float32)
    holdout=rng.uniform(.12,.72,(128,3)).astype(np.float32)
    desired=identity_color().model_copy(update={'gains_rgb':[1.15,.92,1.05]})
    pair=PairSamples('pair',before,apply_global(before,desired),holdout,apply_global(holdout,desired))
    baseline=reconstruction_metrics(identity_color(),[pair])
    result=fit_color([pair],FitOptions(parameter_steps=75,residual_steps=0,local_steps=0,initializations=1))
    assert result.metrics['median_delta_e']<baseline['median_delta_e']*.5
    assert result.global_color.residual_lut.encoding=='constant'
    assert result.optimizer_version=='color_fit_adam_v1'
    assert result.history[-1]['step']==75

def test_residual_and_local_stages_keep_finite_contracts():
    torch.set_num_threads(2)
    rng=np.random.default_rng(4)
    x=rng.uniform(.2,.7,(96,3)).astype(np.float32)
    mask=np.r_[np.full(48,.9),np.full(48,.1)].astype(np.float32)
    target=np.clip(x+np.array([.015,-.01,.005],np.float32),0,1)
    pair=PairSamples('small',x,target,x[::-1].copy(),target[::-1].copy(),mask,mask[::-1].copy())
    result=fit_color([pair],FitOptions(parameter_steps=2,residual_steps=2,local_steps=2,initializations=1,evaluations_every=1))
    assert {'parameters','residual','local'} <= {x['stage'] for x in result.history}
    assert np.isfinite(result.holdout_loss)
    assert all(np.isfinite(x['holdout_loss']) for x in result.history)


def test_cancelled_fit_stops_before_another_optimizer_step(monkeypatch):
    import pytest
    torch.set_num_threads(2)
    x=np.random.default_rng(4).uniform(.2,.7,(64,3)).astype(np.float32)
    pair=PairSamples('cancel',x,x+.01,x[::-1].copy(),x[::-1].copy()+.01)
    steps=0
    original=torch.optim.Adam.step
    def step(self,*args,**kwargs):
        nonlocal steps
        result=original(self,*args,**kwargs);steps+=1
        return result
    monkeypatch.setattr(torch.optim.Adam,'step',step)
    def active():
        if steps>=2:raise RuntimeError('lease lost')
    with pytest.raises(RuntimeError,match='lease lost'):
        fit_color([pair],FitOptions(parameter_steps=50,residual_steps=50,local_steps=0,initializations=3),check_active=active)
    assert steps==2
    # No global cancellation state contaminates a subsequent task.
    result=fit_color([pair],FitOptions(parameter_steps=1,residual_steps=0,local_steps=0,initializations=1))
    assert np.isfinite(result.holdout_loss)


def test_cancelled_identity_fit_does_not_return_result():
    import pytest
    x=np.ones((16,3),np.float32)*.5
    def active():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        fit_color([PairSamples('same',x,x,x,x)],check_active=active)


def test_cancellation_during_final_metrics_is_observed(monkeypatch):
    import pytest
    import photo_worker.algorithms.fitting as fitting
    cancelled=False
    original=fitting.apply_global
    def render(*args,**kwargs):
        nonlocal cancelled
        output=original(*args,**kwargs);cancelled=True
        return output
    monkeypatch.setattr(fitting,'apply_global',render)
    def active():
        if cancelled:raise RuntimeError('cancelled during metrics')
    x=np.ones((16,3),np.float32)*.5
    with pytest.raises(RuntimeError,match='during metrics'):
        fit_color([PairSamples('identity',x,x,x,x)],check_active=active)
