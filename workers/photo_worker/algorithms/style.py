from __future__ import annotations
from dataclasses import dataclass
import numpy as np
from skimage.color import deltaE_ciede2000
from photo_worker.contracts.style import StyleV2,GlobalColor,LUT,LocalColor,Disabled
from .adaptation import neighbors,blend,ood_reasons,ood_reasons_from_counts
from .color import apply_global,apply_local,apply_strength,compile_absolute_lut,expand_lut,trilinear,rgb_to_lab_d50

@dataclass(frozen=True)
class CompiledLUT:
    lut: LUT | None
    attempts: tuple[dict,...]
    adaptive_allowed: bool


def compile_validated_lut(config:GlobalColor) -> CompiledLUT:
    # This validation corpus is fixed by renderer version, not derived from fit
    # pixels or selected to favor a particular profile.
    samples=np.random.default_rng(42).random((100000,3),dtype=np.float32)
    exact=apply_global(samples,config)
    reference_lab=rgb_to_lab_d50(exact)
    attempts=[]
    for size in (33,65):
        lut=compile_absolute_lut(config,size)
        approximate=trilinear(expand_lut(lut),samples)
        delta=deltaE_ciede2000(reference_lab,rgb_to_lab_d50(approximate))
        measured={'size':size,'sample_count':len(samples),'seed':42,
                  'median_delta_e':float(np.median(delta)),'p95_delta_e':float(np.percentile(delta,95)),
                  'max_delta_e':float(delta.max())}
        attempts.append(measured)
        if measured['p95_delta_e']<=.3:
            return CompiledLUT(lut,tuple(attempts),True)
    return CompiledLUT(None,tuple(attempts),False)

@dataclass(frozen=True)
class ResolvedColor:
    fixed: GlobalColor | None
    absolute_lut: np.ndarray | None
    local: LocalColor | Disabled
    strength: float
    needs_review: bool
    evidence: dict

    def global_pass(self,rgb):
        if self.fixed is not None:return apply_global(rgb,self.fixed)
        return trilinear(self.absolute_lut,rgb)

    def local_and_strength(self,base_aligned,global_output,person):
        return apply_strength(base_aligned,apply_local(global_output,person,self.local),self.strength)


def resolve_style(style:StyleV2,*,features:np.ndarray|None=None,linear_pixels:np.ndarray|None=None,
                  person_reliable:bool=True,strict_ood:bool=False,group_prototype_id:str|None=None,coverage_counts:np.ndarray|None=None) -> ResolvedColor:
    if style.quality.status=='example':raise ValueError('EXAMPLE_STYLE_FORBIDDEN')
    if linear_pixels is not None and coverage_counts is not None:raise ValueError('RESOLVE_COVERAGE_AMBIGUOUS')
    fixed,absolute,local=style.global_,None,style.local
    evidence={'mode':'fixed','neighbors':[],'reasons':[]}
    if style.mode=='adaptive':
        if features is None or linear_pixels is None and coverage_counts is None:raise ValueError('ADAPTIVE_FEATURES_REQUIRED')
        evidence.update(ood_reasons(features,linear_pixels,style.adaptation) if coverage_counts is None else ood_reasons_from_counts(features,coverage_counts,style.adaptation))
        if group_prototype_id is not None:
            candidates=[p for p in style.adaptation.prototypes if p.id==group_prototype_id]
            if len(candidates)!=1:raise ValueError('GROUP_PROTOTYPE_INVALID')
            selected=candidates[0]
            fixed=None;absolute=expand_lut(selected.absolute_lut);local=selected.local
            evidence['mode']='group_fixed';evidence['group_prototype_id']=selected.id
            # Explicit group choice never changes with input order or per-frame KNN.
            evidence['fallback']=False
        elif evidence['fallback']:
            if strict_ood:raise ValueError('STYLE_OOD_SKIP')
            evidence['mode']='fixed_fallback'
        else:
            selected=neighbors(features,style.adaptation)
            absolute,local=blend(style.adaptation,selected);fixed=None
            evidence['mode']='adaptive';evidence['neighbors']=[n.__dict__ for n in selected]
    elif group_prototype_id is not None:raise ValueError('GROUP_PROTOTYPE_INVALID')
    if local.enabled and not person_reliable:
        local=Disabled(enabled=False)
        evidence['reasons'].append('local_mask_unreliable')
    return ResolvedColor(fixed,absolute,local,style.strength,bool(evidence['reasons']),evidence)
