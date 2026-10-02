from typing import Annotated,Literal
import math
from pydantic import Field,model_validator
from .base import Contract,ArtifactRef,Unit,SHA256,canonical_hash
from .style import GlobalColor,LocalModule,Beauty,Defects,Geometry,Disabled,Extensions,ModelReference
from .override import OverrideV1,JobOverrideV1

class ResolveParams(Contract):
    style:ArtifactRef|None=None
    identity:bool=Field(default=False,exclude_if=lambda value:not value)
    image:ArtifactRef
    stats:ArtifactRef|None=None
    strict_ood:bool=False
    group_prototype_id:Annotated[str,Field(min_length=1,max_length=120)]|None=None
    override:OverrideV1|None=Field(default=None,exclude_if=lambda value:value is None)
    job_override:JobOverrideV1|None=Field(default=None,exclude_if=lambda value:value is None)

    @model_validator(mode='after')
    def style_source(self):
        if (self.style is None)==(not self.identity):
            raise ValueError('Select exactly one style source')
        if self.identity and self.override is None and self.job_override is None:
            raise ValueError('Identity resolution requires explicit modules')
        return self

class AbsoluteLUTRef(Contract):
    artifact:ArtifactRef
    size:Literal[33,65]
    dtype:Literal['float32']='float32'
    order:Literal['RGB_C']='RGB_C'
    profile_id:Literal['work_rec2020_gamma24_v1']='work_rec2020_gamma24_v1'

class PrototypeWeight(Contract):
    prototype_id:Annotated[str,Field(min_length=1,max_length=120)]
    distance:Annotated[float,Field(ge=0)]
    weight:Unit

class ResolvedModules(Contract):
    beauty:Annotated[Disabled|Beauty,Field(discriminator='enabled')]
    defects:Annotated[Disabled|Defects,Field(discriminator='enabled')]
    geometry:Annotated[Disabled|Geometry,Field(discriminator='enabled')]
    extensions:Extensions
    models:list[ModelReference]

class ResolvedStyleBody(Contract):
    params:ResolveParams
    mode:Literal['fixed','adaptive','fixed_fallback','group_fixed','skip']
    fixed:GlobalColor|None
    absolute_lut:AbsoluteLUTRef|None
    local:LocalModule
    strength:Unit
    modules:ResolvedModules
    neighbors:Annotated[list[PrototypeWeight],Field(max_length=4)]
    distance:Annotated[float,Field(ge=0)]|None
    uncovered_fraction:Unit|None
    reasons:Annotated[list[Literal['ood_distance','ood_color_coverage','local_mask_unreliable']],Field(max_length=3)]
    needs_review:bool

    @model_validator(mode='after')
    def coherent(self):
        if self.mode=='skip':
            if self.fixed is not None or self.absolute_lut is not None or self.local.enabled or not self.params.strict_ood:
                raise ValueError('Invalid skipped style')
        elif self.mode in ('fixed','fixed_fallback'):
            if self.fixed is None or self.absolute_lut is not None:raise ValueError('Missing fixed color')
        elif self.fixed is not None or self.absolute_lut is None:raise ValueError('Missing absolute LUT')
        if (self.mode=='adaptive')!=bool(self.neighbors):raise ValueError('Invalid neighbors')
        if self.neighbors and (not math.isclose(sum(n.weight for n in self.neighbors),1.,abs_tol=1e-12)
            or len({n.prototype_id for n in self.neighbors})!=len(self.neighbors)):
            raise ValueError('Invalid prototype weights')
        if (self.mode=='group_fixed')!=(self.params.group_prototype_id is not None):raise ValueError('Invalid group prototype binding')
        if self.mode=='fixed':
            if self.distance is not None or self.uncovered_fraction is not None:raise ValueError('Unexpected OOD metrics')
        elif self.distance is None or self.uncovered_fraction is None:raise ValueError('Missing OOD metrics')
        if self.mode in ('fixed_fallback','skip') and not any(r.startswith('ood_') for r in self.reasons):raise ValueError('Missing OOD reason')
        if self.needs_review!=bool(self.reasons) or len(set(self.reasons))!=len(self.reasons):raise ValueError('Invalid review evidence')
        return self


class ResolvedStyle(ResolvedStyleBody):
    hash:SHA256

    @model_validator(mode='after')
    def verify_hash(self):
        if self.hash!=canonical_hash(self.model_dump(mode='json',by_alias=True,exclude={'hash'})):
            raise ValueError('Resolved style hash mismatch')
        return self

    @classmethod
    def create(cls,**values):
        body=ResolvedStyleBody(**values)
        return cls(**body.model_dump(by_alias=True),hash=canonical_hash(body))
