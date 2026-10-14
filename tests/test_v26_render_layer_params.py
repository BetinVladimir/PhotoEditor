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


@pytest.mark.parametrize('field,value',[
    ('operation',None),('operation',''),('operation','я'*41),
    ('operation','\ud800'),('operation','test\x00adjust'),
    ('version',[]),('version',''),('version','я'*11),
    ('runtime_sha256',None),('runtime_sha256','B'*64),
    ('verify_runtime',None),('validate_binding',False),('params_model',dict),
])
def test_invalid_installed_handler_refuses_before_callbacks_and_document_io(field,value):
    from dataclasses import replace
    artifacts,inputs,operator,events,size=fixture()
    invalid=replace(operator,**{field:value})
    with pytest.raises(ValueError,match='INVENTORY_INVALID'):
        load_render_layer_params(artifacts,inputs,(invalid,),max_document_bytes=size)
    assert events==[]


@pytest.mark.parametrize('kind',['mutable','untyped','duplicate'])
def test_inventory_shape_and_duplicate_handlers_refuse_before_io(kind):
    artifacts,inputs,operator,events,size=fixture()
    inventory=[operator] if kind=='mutable' else (object(),) if kind=='untyped' else (operator,operator)
    with pytest.raises(ValueError,match='INVENTORY_INVALID'):
        load_render_layer_params(artifacts,inputs,inventory,max_document_bytes=size)
    assert events==[]


def test_binding_callback_cannot_replace_params_or_mutate_caller_source():
    from dataclasses import replace
    artifacts,inputs,operator,events,size=fixture()
    original=inputs.layers.layers[0].params
    def binding(params,owned):
        owned.canvas_hash='c'*64
        owned.layers.layers.clear()
        params.canvas_hash='c'*64
    operator=replace(operator,validate_binding=binding)
    with pytest.raises(ValueError,match='BINDING_CHANGED'):
        load_render_layer_params(artifacts,inputs,(operator,),max_document_bytes=size)
    assert inputs.canvas_hash=='a'*64 and len(inputs.layers.layers)==1
    assert inputs.layers.layers[0].params==original


def test_runtime_callback_cannot_change_snapshotted_layer_selection():
    from dataclasses import replace
    artifacts,inputs,operator,events,size=fixture()
    original=inputs.layers.layers[0].id
    def verify():
        inputs.layers.layers.clear()
    operator=replace(operator,verify_runtime=verify)
    result=load_render_layer_params(artifacts,inputs,(operator,),max_document_bytes=size)
    assert len(result)==1 and result[0].layer.id==original
    assert events==['read','binding']
