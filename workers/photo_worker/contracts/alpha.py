from typing import Annotated,Literal
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,Unit
from .images import MaskRef


class MattingParams(Contract):
    image:ArtifactRef
    alpha:MaskRef
    trimap_low:Literal[.1]=.1
    trimap_high:Literal[.9]=.9
    band_fraction:Literal[.005]=.005
    max_roi:Literal[1024]=1024

    @model_validator(mode='after')
    def alpha_role(self):
        if self.alpha.selector not in ('alpha','foreground') or self.alpha.protected:raise ValueError('Expected foreground alpha')
        return self


class MattingReport(Contract):
    image:ArtifactRef
    input_alpha:ArtifactRef
    output_alpha:ArtifactRef
    uncertain_pixels:Annotated[int,Field(ge=0,le=200000000)]
    roi:Annotated[list[int],Field(min_length=4,max_length=4)]
    band_radius:Annotated[int,Field(ge=2,le=2**31-1)]
    confidence:Unit
    reason:Literal['refined','no_boundary']
    algorithm:Literal['pymatting_cf_v1']='pymatting_cf_v1'
