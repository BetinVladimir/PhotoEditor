"""Strict source-bound layer-stack evidence for full render recipe inputs."""
from typing import Annotated, Any, Literal
from uuid import UUID
from pydantic import Field, model_validator
from photo_worker.contracts.base import ArtifactRef, Contract, Unit, canonical_bytes, canonical_hash, parse_json
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.algorithms.layer_masks import compile_mask


class RenderLayer(Contract):
    id: UUID
    enabled: bool
    opacity: Unit
    blend: Literal['normal', 'multiply', 'screen']
    operation: Annotated[str, Field(min_length=1, max_length=80)]
    operation_version: Annotated[str, Field(min_length=1, max_length=20)]
    params: ArtifactRef
    mask: dict[str, Any]


class RenderLayerStack(Contract):
    schema_version: Literal[1]
    source: VersionBinding
    layers: Annotated[list[RenderLayer], Field(max_length=32)]

    @model_validator(mode='after')
    def geometry_and_budget(self):
        if self.source.asset_version_id.int == 0 or self.source.source.artifact_id.int == 0:
            raise ValueError('RENDER_LAYER_SOURCE_INVALID')
        ids=set();photometric=False;nodes=0
        def count(value):
            return 1+sum(count(value[k]) for k in ('operand','left','right') if k in value)
        for layer in self.layers:
            if (layer.id.int == 0 or layer.id in ids or layer.params.artifact_id.int == 0
                    or len(layer.operation.encode()) > 80 or len(layer.operation_version.encode()) > 20):
                raise ValueError('RENDER_LAYER_ID_INVALID')
            ids.add(layer.id)
            plan=compile_mask(layer.mask,self.source.canvas_hash)
            if any(n.ref is not None and n.ref.artifact_id.int == 0 for n in plan.nodes):
                raise ValueError('RENDER_LAYER_MASK_INVALID')
            nodes+=count(layer.mask)
            if nodes>256:raise ValueError('RENDER_LAYER_MASK_BUDGET')
            if layer.enabled and layer.opacity>0:
                geometry=layer.operation in ('optical.lens','optical.perspective','optical.panorama','optical.focus_stack')
                if geometry and photometric:raise ValueError('RENDER_LAYER_GEOMETRY_ORDER')
                if not geometry:photometric=True
        return self


def decode_render_layer_stack(raw: bytes, reference: ArtifactRef, binding: VersionBinding) -> RenderLayerStack:
    value=parse_json(raw)
    if canonical_bytes(value)!=raw or canonical_hash(value)!=reference.sha256:
        raise ValueError('RENDER_LAYER_DOCUMENT_CHANGED')
    stack=RenderLayerStack.model_validate_json(raw)
    if stack.source!=binding or canonical_bytes(stack)!=raw:
        raise ValueError('RENDER_LAYER_SOURCE_CHANGED')
    return stack
