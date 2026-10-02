"""Differentiable implementation of the frozen ColorProgramV1 math."""
from __future__ import annotations
import torch
from .color import KNOTS,LUMA,RGB_TO_XYZ,BRADFORD,D50


def _constant(value, reference):
    return torch.as_tensor(value,dtype=reference.dtype,device=reference.device)


def power_nonnegative(value, exponent):
    # Avoid inf * 0 gradients at black while preserving exact zero output.
    return torch.where(value>0,value.clamp_min(1e-12).pow(exponent),torch.zeros_like(value))


def to_linear(value):
    return power_nonnegative(value.clamp(0,1),2.4)


def to_encoded(value):
    return power_nonnegative(value.clamp(0,1),1/2.4)


def trilinear(grid,points):
    size=grid.shape[0]
    q=points.clamp(0,1)*(size-1)
    base=torch.floor(q).to(torch.long).clamp(max=size-2)
    weights=q-base.to(q.dtype)
    output=torch.zeros_like(points)
    for r in (0,1):
        for g in (0,1):
            for b in (0,1):
                weight=(weights[...,0] if r else 1-weights[...,0])*(weights[...,1] if g else 1-weights[...,1])*(weights[...,2] if b else 1-weights[...,2])
                output=output+grid[base[...,0]+r,base[...,1]+g,base[...,2]+b]*weight[...,None]
    return output


def curve(value,knots_y):
    knots=_constant(KNOTS,value)
    index=torch.bucketize(value.contiguous(),knots,right=True).sub(1).clamp(0,5)
    weight=(value-knots[index])/(knots[index+1]-knots[index])
    return knots_y[index]*(1-weight)+knots_y[index+1]*weight


def apply_global(rgb,gains,master,channels,saturation,residual):
    encoded=to_encoded(to_linear(rgb)*gains)
    curved=curve(encoded,master)
    curved=torch.stack([curve(curved[...,c],channels[c]) for c in range(3)],dim=-1)
    linear=to_linear(curved)
    y=(linear*_constant(LUMA,linear)).sum(dim=-1,keepdim=True)
    encoded=to_encoded(linear+(saturation-1)*(linear-y))
    return (encoded+trilinear(residual,encoded)).clamp(0,1)


def apply_local(rgb,person,ev,saturation):
    linear=to_linear(rgb)
    def region(index):
        exposed=linear*torch.pow(_constant(2.,linear),ev[index])
        y=(exposed*_constant(LUMA,linear)).sum(dim=-1,keepdim=True)
        return exposed+(saturation[index]-1)*(exposed-y)
    p=person.clamp(0,1)[...,None]
    return to_encoded(region(0)*p+region(1)*(1-p))


def rgb_to_lab_d50(rgb):
    xyz=to_linear(rgb)@_constant(RGB_TO_XYZ,rgb).T@_constant(BRADFORD,rgb).T
    t=xyz/_constant(D50,rgb)
    delta=6/29
    f=torch.where(t>delta**3,power_nonnegative(t,1/3),t/(3*delta**2)+4/29)
    return torch.stack([116*f[...,1]-16,500*(f[...,0]-f[...,1]),200*(f[...,1]-f[...,2])],dim=-1)
