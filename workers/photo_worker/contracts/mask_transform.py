from typing import Literal,Annotated
from pydantic import Field
from .base import Contract,ArtifactRef
from .images import MaskRef,TransformStep,TransformChain,WorkingImage

class MaskTransformParams(Contract):
    mask:MaskRef
    target:ArtifactRef
    step:TransformStep
    dtype:Literal['float32','uint16']='float32'

    def validate_target(self,target:WorkingImage):
        if max(self.mask.canvas.width*self.mask.canvas.height,target.canvas.width*target.canvas.height)>200000000:
            raise ValueError("Mask transform pixel limit")
        if self.mask.canvas!=self.step.from_canvas or target.canvas!=self.step.to_canvas:
            raise ValueError('Mask transform canvas mismatch')
        expected=TransformChain.create([*self.mask.transform_chain.steps,self.step])
        if target.transform_chain!=expected:raise ValueError('Mask transform chain mismatch')
        if self.dtype=='float32' and (target.dtype!='float32' or target.color.profile_id!='work_rec2020_gamma24_v1'):
            raise ValueError('Probability guide must use working RGB')
        return expected

class TransformedMask(Contract):
    params:MaskTransformParams
    mask:MaskRef
    png:ArtifactRef
    dtype:Literal['float32','uint16']
    guided_radius:Annotated[int,Field(ge=1)]|None
    algorithm:Literal['mask_transform_v1']='mask_transform_v1'
