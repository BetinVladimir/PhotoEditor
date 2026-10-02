from typing import Annotated,Literal
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,Unit
from .images import MaskRef


class Seam(Contract):
    orientation:Literal['vertical','horizontal']
    position:Unit


class QualityParams(Contract):
    target:ArtifactRef
    target_kind:Literal['working','encoded']
    stage:Literal['master','export']
    threshold_profile_id:Literal['master_v1','export_v1']
    baseline:ArtifactRef|None=None
    protected:Annotated[list[MaskRef],Field(max_length=32)]=[]
    seams:Annotated[list[Seam],Field(max_length=256)]=[]

    @model_validator(mode='after')
    def mode(self):
        if (self.target_kind=='working')!=(self.stage=='master' and self.threshold_profile_id=='master_v1'):raise ValueError('Quality stage mismatch')
        if self.target_kind=='encoded' and (self.baseline is not None or self.protected):raise ValueError('Encoded quality cannot bind pixel masks')
        return self


class ReferenceMetrics(Contract):
    mean_absolute_error:Annotated[float,Field(ge=0,le=1)]
    psnr_db:Annotated[float,Field(ge=0,le=200)]
    protected_max_delta:Annotated[float,Field(ge=0,le=1)]|None


class TechnicalMetrics(Contract):
    below_zero_fraction:Unit
    above_one_fraction:Unit
    clipped_black_fraction:Unit
    clipped_white_fraction:Unit
    sharpness_laplacian:Annotated[float,Field(ge=0,le=1000000)]
    seam_p99_delta:Annotated[float,Field(ge=0,le=1)]
    finite:bool


class QualityReport(Contract):
    target:ArtifactRef
    target_kind:Literal['working','encoded']
    stage:Literal['master','export']
    threshold_profile_id:Literal['master_v1','export_v1']
    status:Literal['pass','review','fail']
    technical:TechnicalMetrics
    reference:ReferenceMetrics|None
    reasons:Annotated[list[Literal['nonfinite','excessive_black_clipping','excessive_white_clipping','protected_pixels_changed','seam_detected','low_sharpness','icc_mismatch','orientation_mismatch']],Field(max_length=8)]
