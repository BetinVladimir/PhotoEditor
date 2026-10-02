"""Bounded, staged ColorProgram optimization with independent holdout selection."""
from __future__ import annotations
from dataclasses import dataclass,field
from typing import Sequence,Callable
import numpy as np
import torch
from torch import nn
from torch.nn import functional as F
from skimage.color import deltaE_ciede2000
from photo_worker.contracts.style import GlobalColor,LUT,Disabled,LocalColor
from .color import KNOTS,apply_global,apply_local,rgb_to_lab_d50
from . import torch_color as tc

OPTIMIZER_VERSION='color_fit_adam_v1'

@dataclass(frozen=True)
class PairSamples:
    id: str
    train_before: np.ndarray
    train_after: np.ndarray
    holdout_before: np.ndarray
    holdout_after: np.ndarray
    train_person: np.ndarray | None = None
    holdout_person: np.ndarray | None = None

    def validate(self):
        for before,after in ((self.train_before,self.train_after),(self.holdout_before,self.holdout_after)):
            if before.ndim!=2 or before.shape[-1]!=3 or not 1<=len(before)<=65536 or before.shape!=after.shape or not np.isfinite(before).all() or not np.isfinite(after).all() or np.min(before)<0 or np.max(before)>1 or np.min(after)<0 or np.max(after)>1:
                raise ValueError('FIT_SAMPLES_INVALID')
        for image,person in ((self.train_before,self.train_person),(self.holdout_before,self.holdout_person)):
            if person is not None and (person.shape!=(len(image),) or not np.isfinite(person).all() or np.min(person)<0 or np.max(person)>1):
                raise ValueError('FIT_MASK_INVALID')

@dataclass(frozen=True)
class FitOptions:
    device: str='cpu'
    parameter_steps: int=400
    residual_steps: int=600
    local_steps: int=200
    evaluations_every: int=25
    patience: int=100
    initializations: int=3

    def validate(self):
        if self.device not in ('cpu','mps','cuda') or not 1<=self.initializations<=3 or min(self.parameter_steps,self.residual_steps,self.local_steps)<0 or self.evaluations_every<1 or self.patience<1:
            raise ValueError('FIT_OPTIONS_INVALID')
        if self.device=='mps' and not torch.backends.mps.is_available() or self.device=='cuda' and not torch.cuda.is_available():
            raise ValueError('BACKEND_UNAVAILABLE')

@dataclass
class FitResult:
    global_color: GlobalColor
    local: LocalColor | Disabled
    holdout_loss: float
    metrics: dict
    initialization_index: int
    optimizer_version: str=OPTIMIZER_VERSION
    history: list[dict]=field(default_factory=list)


def identity_color() -> GlobalColor:
    return GlobalColor(gains_rgb=[1.,1.,1.],master_curve=KNOTS.tolist(),rgb_curves=[KNOTS.tolist() for _ in range(3)],saturation=1.,residual_lut=LUT(kind='residual',size=17,encoding='constant',values=[0.,0.,0.]))


class ColorParameters(nn.Module):
    def __init__(self,gains,device):
        super().__init__()
        tensor=lambda v:torch.tensor(v,dtype=torch.float32,device=device)
        initial=(tensor(gains)-.25)/3.75
        self.gains_raw=nn.Parameter(torch.logit(initial))
        self.saturation_raw=nn.Parameter(tensor(0.))
        # Eight positive intervals: black margin, six knot gaps, white margin.
        # The constrained straight-through endpoint projection preserves exact
        # identity endpoints without an infinite sigmoid/softplus parameter.
        intervals=np.r_[1e-6,np.diff(KNOTS),1e-6]
        self.curves_raw=nn.Parameter(torch.log(torch.expm1(tensor(np.tile(intervals,(4,1))))))
        self.residual_raw=nn.Parameter(torch.zeros((17,17,17,3),dtype=torch.float32,device=device))
        self.local_ev_raw=nn.Parameter(tensor([0.,0.]))
        self.local_sat_raw=nn.Parameter(tensor([0.,0.]))

    def values(self):
        gains=.25+3.75*torch.sigmoid(self.gains_raw)
        increments=F.softplus(self.curves_raw)
        curves=increments.cumsum(-1)[...,:7]/increments.sum(-1,keepdim=True)
        projected=curves.clone()
        projected[:,0]=torch.where(projected[:,0]<2e-6,torch.zeros_like(projected[:,0]),projected[:,0])
        projected[:,-1]=torch.where(projected[:,-1]>1-2e-6,torch.ones_like(projected[:,-1]),projected[:,-1])
        curves=curves+(projected-curves).detach()
        return gains,curves,2*torch.sigmoid(self.saturation_raw),.25*torch.tanh(self.residual_raw)

    def forward(self,rgb,person=None,local=False):
        gains,curves,saturation,residual=self.values()
        output=tc.apply_global(rgb,gains,curves[0],curves[1:],saturation,residual)
        if local:
            output=tc.apply_local(output,person,torch.tanh(self.local_ev_raw),.7+.6*torch.sigmoid(self.local_sat_raw))
        return output

    def config(self):
        gains,curves,saturation,residual=self.values()
        array=lambda v:v.detach().cpu().numpy()
        grid=array(residual)
        lut=LUT(kind='residual',size=17,encoding='constant',values=[0.,0.,0.]) if not np.any(grid) else LUT(kind='residual',size=17,encoding='dense',values=grid.reshape(-1).tolist())
        return GlobalColor(gains_rgb=array(gains).tolist(),master_curve=array(curves[0]).tolist(),rgb_curves=array(curves[1:]).tolist(),saturation=float(saturation.detach().cpu()),residual_lut=lut)

    def local_config(self):
        ev=torch.tanh(self.local_ev_raw).detach().cpu().tolist()
        sat=(.7+.6*torch.sigmoid(self.local_sat_raw)).detach().cpu().tolist()
        return LocalColor(enabled=True,person_ev=ev[0],background_ev=ev[1],person_saturation=sat[0],background_saturation=sat[1],mask_policy_id='person_guided_v1')

    def regularization(self,one_pair:bool,local:bool):
        gains,curves,saturation,residual=self.values()
        knots=torch.tensor(KNOTS,dtype=gains.dtype,device=gains.device)
        blocks=[((gains-1)/3.75).square().mean(),(curves-knots).square().mean(),((saturation-1)/2).square(),(residual/.5).square().mean()]
        if local:
            blocks += [(torch.tanh(self.local_ev_raw)/2).square().mean(),((.7+.6*torch.sigmoid(self.local_sat_raw)-1)/.6).square().mean()]
        identity=torch.stack(blocks).mean()
        smooth=torch.stack([torch.diff(residual,n=2,dim=axis).square().mean() for axis in range(3)]).mean()
        x=torch.linspace(0,1,17,dtype=gains.dtype,device=gains.device)
        grid=torch.stack(torch.meshgrid(x,x,x,indexing='ij'),-1)
        output=tc.apply_global(grid,gains,curves[0],curves[1:],saturation,residual)
        fold=torch.stack([F.relu(-torch.diff(output[...,axis],dim=axis)).mean() for axis in range(3)]).mean()
        return (.1 if one_pair else .01)*identity+(1. if one_pair else .1)*smooth+.1*fold


def _data_loss(predicted,target):
    return F.huber_loss(predicted,target,delta=.02)+.2*(tc.rgb_to_lab_d50(predicted)-tc.rgb_to_lab_d50(target)).abs().mean()/100


def _tensor(array,options):
    return torch.as_tensor(np.asarray(array,dtype=np.float32),device=options.device)


def _holdout(model,pairs,options,local=False,check_active:Callable[[],None]=lambda:None):
    values=[]
    with torch.no_grad():
        for pair in pairs:
            check_active()
            person=_tensor(pair.holdout_person,options) if local else None
            output=model(_tensor(pair.holdout_before,options),person,local)
            values.append(float(_data_loss(output,_tensor(pair.holdout_after,options)).cpu()))
    check_active()
    return float(np.mean(values))


def _stage(model,pairs,options,phase,steps,lr,names,history,local=False,check_active:Callable[[],None]=lambda:None):
    parameters=[]
    for name,p in model.named_parameters():
        p.requires_grad_(name in names)
        if p.requires_grad:parameters.append(p)
    check_active()
    optimizer=torch.optim.Adam(parameters,lr=lr)
    best=_holdout(model,pairs,options,local,check_active)
    progress_best=best;last_improvement=0
    checkpoint={k:v.detach().clone() for k,v in model.state_dict().items()}
    for step in range(1,steps+1):
        check_active()
        optimizer.zero_grad(set_to_none=True)
        # Backward per pair bounds accelerator memory and assigns equal pair
        # weights regardless of image resolution or sample count.
        for pair in pairs:
            check_active()
            person=_tensor(pair.train_person,options) if local else None
            predicted=model(_tensor(pair.train_before,options),person,local)
            loss=_data_loss(predicted,_tensor(pair.train_after,options))/len(pairs)
            if not torch.isfinite(loss).item():raise ValueError('FIT_NONFINITE')
            loss.backward()
        penalty=model.regularization(len(pairs)==1,local)
        penalty.backward()
        if any(p.grad is not None and not torch.isfinite(p.grad).all().item() for p in parameters):raise ValueError('FIT_NONFINITE_GRADIENT')
        check_active()
        optimizer.step()
        if step%options.evaluations_every==0 or step==steps:
            score=_holdout(model,pairs,options,local,check_active)
            if not np.isfinite(score):raise ValueError('FIT_NONFINITE')
            history.append({'stage':phase,'step':step,'holdout_loss':score})
            if score<best:
                best=score;checkpoint={k:v.detach().clone() for k,v in model.state_dict().items()}
            if score<=progress_best*(1-.001):
                last_improvement=step;progress_best=score
            if step-last_improvement>=options.patience:break
    check_active()
    model.load_state_dict(checkpoint)
    return best


def reconstruction_metrics(config,pairs,local=None,*,check_active:Callable[[],None]=lambda:None):
    per_pair=[]
    for pair in pairs:
        check_active()
        output=apply_global(pair.holdout_before,config)
        if local is not None and local.enabled:
            output=apply_local(output,pair.holdout_person,local)
        delta=deltaE_ciede2000(rgb_to_lab_d50(output),rgb_to_lab_d50(pair.holdout_after))
        per_pair.append({'pair_id':pair.id,'median_delta_e':float(np.median(delta)),'p95_delta_e':float(np.percentile(delta,95))})
    check_active()
    # Equal contribution of pair summaries, not a resolution-weighted pool.
    return {'median_delta_e':float(np.mean([p['median_delta_e'] for p in per_pair])),
            'p95_delta_e':float(np.mean([p['p95_delta_e'] for p in per_pair])),'pairs':per_pair}


def _local_reliable(pairs):
    for pair in pairs:
        for person in (pair.train_person,pair.holdout_person):
            if person is None:return False
            foreground=person>=.5
            if foreground.mean()<.01 or float(person[foreground].mean())<.75:return False
    return True


def fit_color(pairs:Sequence[PairSamples],options:FitOptions=FitOptions(),*,check_active:Callable[[],None]=lambda:None) -> FitResult:
    check_active()
    options.validate()
    if not pairs or len({p.id for p in pairs})!=len(pairs):raise ValueError('FIT_PAIRS_INVALID')
    pairs=sorted(pairs,key=lambda p:p.id)
    for pair in pairs:
        check_active()
        pair.validate()
    if all(np.array_equal(p.train_before,p.train_after) and np.array_equal(p.holdout_before,p.holdout_after) for p in pairs):
        config=identity_color()
        return FitResult(config,Disabled(enabled=False),0.,reconstruction_metrics(config,pairs,check_active=check_active),0,history=[{'stage':'identity','step':0,'holdout_loss':0.}])
    candidates=[]
    for index,gains in enumerate(([1.,1.,1.],[.95,1.,1.05],[1.05,1.,.95])[:options.initializations]):
        check_active()
        model=ColorParameters(gains,options.device);history=[]
        _stage(model,pairs,options,'parameters',options.parameter_steps,.01,{'gains_raw','curves_raw','saturation_raw'},history,check_active=check_active)
        param_state={k:v.detach().clone() for k,v in model.state_dict().items()}
        param_metrics=reconstruction_metrics(model.config(),pairs,check_active=check_active)
        _stage(model,pairs,options,'residual',options.residual_steps,.003,{'residual_raw'},history,check_active=check_active)
        residual_metrics=reconstruction_metrics(model.config(),pairs,check_active=check_active)
        if param_metrics['median_delta_e']-residual_metrics['median_delta_e']<.2 or residual_metrics['p95_delta_e']-param_metrics['p95_delta_e']>.5:
            model.load_state_dict(param_state)
        global_loss=_holdout(model,pairs,options,check_active=check_active)
        local=Disabled(enabled=False)
        if options.local_steps and _local_reliable(pairs):
            local_loss=_stage(model,pairs,options,'local',options.local_steps,.01,{'local_ev_raw','local_sat_raw'},history,True,check_active)
            if local_loss<=global_loss*.95 and local_loss<global_loss:
                local=model.local_config();global_loss=local_loss
        config=model.config()
        candidates.append(FitResult(config,local,global_loss,reconstruction_metrics(config,pairs,local,check_active=check_active),index,history=history))
    check_active()
    return min(candidates,key=lambda c:(c.holdout_loss,c.initialization_index))
