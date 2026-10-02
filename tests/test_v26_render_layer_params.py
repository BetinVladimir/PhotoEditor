from contextlib import contextmanager
from types import SimpleNamespace
from io import BytesIO
from uuid import uuid4
import pytest
from photo_worker.contracts.base import Contract, canonical_bytes, canonical_hash
from photo_worker.v2.render_layers import RenderLayer
from photo_worker.v2.render_layer_params import RenderLayerOperator,load_render_layer_params

class Params(Contract):
    schema_version:int
    canvas_hash:str


def fixture():
    params=dict(schema_version=1,canvas_hash='a'*64);body=canonical_bytes(params)
    layer=dict(id=str(uuid4()),enabled=True,opacity=1,blend='normal',operation='test.adjust',operation_version='1',params=dict(artifact_id=str(uuid4()),sha256=canonical_hash(params)),mask=dict(op='ref',mask=dict(artifact_id=str(uuid4()),sha256='a'*64),canvas_hash='a'*64))
    typed=RenderLayer.model_validate_json(canonical_bytes(layer));events=[]
    class Artifacts:
        @contextmanager
        def stream(self,ref):
            events.append('read')
            with BytesIO(body) as stream:yield stream
    def binding(params,inputs):
        events.append('binding')
        if params.canvas_hash!=inputs.canvas_hash:raise ValueError('SOURCE_CHANGED')
    operator=RenderLayerOperator('test.adjust','1','b'*64,Params,lambda:events.append('runtime'),binding)
    inputs=SimpleNamespace(layers=SimpleNamespace(layers=[typed]),canvas_hash='a'*64)
    return Artifacts(),inputs,operator,events,len(body)


def test_runtime_preflight_precedes_io_and_shared_params_are_read_once():
    artifacts,inputs,operator,events,size=fixture()
    inputs.layers.layers*=2
    result=load_render_layer_params(artifacts,inputs,(operator,),max_document_bytes=size)
    assert events==['runtime','read','binding'] and len(result)==2
    assert result[0].params == result[1].params
    result[0].params.canvas_hash='changed'
    assert result[1].params.canvas_hash=='a'*64


def test_unsupported_operator_is_not_skipped_or_downloaded():
    artifacts,inputs,_,events,size=fixture();inputs.layers.layers[0].enabled=False
    with pytest.raises(ValueError,match='UNAVAILABLE'):load_render_layer_params(artifacts,inputs,(),max_document_bytes=size)
    assert events==[]


def test_wrong_source_and_aggregate_document_budget_fail():
    artifacts,inputs,operator,events,size=fixture();inputs.canvas_hash='c'*64
    with pytest.raises(ValueError,match='SOURCE_CHANGED'):load_render_layer_params(artifacts,inputs,(operator,),max_document_bytes=size)
    artifacts,inputs,operator,events,size=fixture()
    with pytest.raises(ValueError,match='DOCUMENT_BUDGET'):load_render_layer_params(artifacts,inputs,(operator,),max_document_bytes=size-1)
