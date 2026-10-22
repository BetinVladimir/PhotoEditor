from typing import Literal,Annotated
from pydantic import Field, model_validator
from .base import Contract,ArtifactRef
from .images import MaskRef,TransformStep,TransformChain,WorkingImage

class TransformRegion(Contract):
    region_id: Annotated[str,Field(min_length=1,max_length=120)]
    mask: MaskRef
    purpose: Literal["region_adjustment","portrait_teeth","portrait_sclera","portrait_shine","portrait_under_eye","portrait_cloth","portrait_glasses","inpaint_target","generation_target","protected"] = Field(default="region_adjustment",exclude_if=lambda value:value=="region_adjustment")

class TransformedRegion(TransformRegion):
    png: ArtifactRef
    guided_radius: Annotated[int,Field(ge=1)]

class MaskTransformParams(Contract):
    mask:MaskRef
    target:ArtifactRef
    step:TransformStep
    dtype:Literal['float32','uint16']='float32'
    regions: Annotated[list[TransformRegion],Field(max_length=128)] = Field(default_factory=list,exclude_if=lambda value:not value)

    @model_validator(mode='after')
    def regional_sources(self):
        if self.regions and self.dtype!='float32':raise ValueError('Regional masks require float32')
        if len({r.region_id for r in self.regions})!=len(self.regions):raise ValueError('Duplicate transform region')
        if len({(r.mask.artifact.artifact_id,r.mask.artifact.sha256) for r in self.regions})>62:raise ValueError('Regional transform output budget')
        selectors={'inpaint_target':{'manual'},'generation_target':{'manual'},'region_adjustment':{'skin','sky','clothing','manual'},'portrait_teeth':{'teeth','manual'},'portrait_sclera':{'sclera','manual'},'portrait_shine':{'skin','manual'},'portrait_under_eye':{'skin','manual'},'portrait_cloth':{'clothing','manual'},'portrait_glasses':{'glasses','manual'}}
        for region in self.regions:
            if (region.mask.canvas!=self.mask.canvas or region.mask.transform_chain!=self.mask.transform_chain
                    or region.mask.protected!=(region.purpose=='protected')
                    or (region.purpose!='protected' and region.mask.selector not in selectors[region.purpose])
                    or (region.purpose=='region_adjustment' and region.mask.confidence<.75)):
                raise ValueError('Regional transform source binding mismatch')
        return self

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
    regions: Annotated[list[TransformedRegion],Field(max_length=128)] = Field(default_factory=list,exclude_if=lambda value:not value)


    @model_validator(mode='after')
    def regional_results(self):
        sources={r.region_id:r for r in self.params.regions}
        if len({r.region_id for r in self.regions})!=len(self.regions) or set(sources)!={r.region_id for r in self.regions}:
            raise ValueError('Transformed region inventory mismatch')
        for region in self.regions:
            source_region=sources[region.region_id];source=source_region.mask;mask=region.mask
            if (region.purpose!=source_region.purpose or mask.canvas!=self.mask.canvas or mask.transform_chain!=self.mask.transform_chain
                    or mask.selector!=source.selector or mask.object_id!=source.object_id
                    or mask.confidence!=source.confidence or mask.model_lock_id!=source.model_lock_id
                    or mask.protected!=source.protected):
                raise ValueError('Transformed regional provenance mismatch')
        return self
