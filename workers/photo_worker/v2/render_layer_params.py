"""Qualified typed Params loading, separate from operator execution."""
from dataclasses import dataclass
from typing import Callable
import re
from photo_worker.contracts.base import Contract, canonical_bytes, parse_json
from .render_inputs import FrozenRenderInputs, read_render_document
from .render_layers import RenderLayer


@dataclass(frozen=True)
class RenderLayerOperator:
    operation: str
    version: str
    runtime_sha256: str
    params_model: type[Contract]
    verify_runtime: Callable[[], None]
    validate_binding: Callable[[Contract, FrozenRenderInputs], None]


@dataclass(frozen=True)
class LoadedRenderLayer:
    layer: RenderLayer
    params: Contract
    runtime_sha256: str


def load_render_layer_params(artifacts, inputs: FrozenRenderInputs,
                             operators: tuple[RenderLayerOperator, ...], *,
                             max_document_bytes: int, check_active=lambda: None) -> tuple[LoadedRenderLayer, ...]:
    """Preflight the complete installed operator inventory before Params IO.

    The runtime provider supplies qualified handlers and source-specific typed
    binding validators. Resource admission is required before invoking this
    bounded loader. No unsupported layer is dropped, including disabled layers.
    """
    if type(max_document_bytes) is not int or not 1<=max_document_bytes<=33554432:
        raise ValueError('RENDER_PARAMS_BUDGET_INVALID')
    layers=inputs.layers.layers if inputs.layers is not None else []
    registry={}
    for operator in operators:
        key=(operator.operation,operator.version)
        if (key in registry or re.fullmatch('[0-9a-f]{64}',operator.runtime_sha256) is None
                or not isinstance(operator.params_model,type) or not issubclass(operator.params_model,Contract)):
            raise ValueError('RENDER_OPERATOR_INVENTORY_INVALID')
        registry[key]=operator
    used={}
    for layer in layers:
        key=(layer.operation,layer.operation_version)
        if key not in registry:raise ValueError('RENDER_OPERATOR_UNAVAILABLE')
        used[key]=registry[key]
    for operator in used.values():
        check_active();operator.verify_runtime();check_active()
    cache={};remaining=max_document_bytes;result=[]
    for layer in layers:
        check_active();operator=registry[(layer.operation,layer.operation_version)]
        key=(layer.params.artifact_id,layer.params.sha256,layer.operation,layer.operation_version)
        if key not in cache:
            raw=read_render_document(artifacts,layer.params,check_active)
            remaining-=len(raw)
            if remaining<0:raise ValueError('RENDER_PARAMS_DOCUMENT_BUDGET')
            if canonical_bytes(parse_json(raw))!=raw:raise ValueError('RENDER_PARAMS_NONCANONICAL')
            params=operator.params_model.model_validate_json(raw)
            operator.validate_binding(params,inputs);check_active();cache[key]=params
        result.append(LoadedRenderLayer(layer.model_copy(deep=True),cache[key].model_copy(deep=True),operator.runtime_sha256))
    return tuple(result)
