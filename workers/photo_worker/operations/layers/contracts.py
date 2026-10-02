"""Frozen same-geometry adjustment composition inputs and inherited lineage."""
from typing import Any,Literal
from pydantic import Field,model_validator
from photo_worker.contracts.base import ArtifactRef,Contract,Unit
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.algorithms.layer_masks import compile_mask

class LayerComposeParams(Contract):
    schema_version:Literal[1]
    base:VersionBinding
    # Parent ImageResults retain generated-mask/report lineage. A null source
    # result is allowed only for an original snapshot WorkingImage, proven by SQL.
    source_result:ArtifactRef|None=Field(...)
    proposal_result:ArtifactRef
    mask:dict[str,Any]
    protected_mask:ArtifactRef|None=Field(...)
    opacity:Unit
    blend:Literal['normal','multiply','screen']

    @model_validator(mode='after')
    def source_bound_mask(self):
        compile_mask(self.mask,self.base.canvas_hash)
        return self
