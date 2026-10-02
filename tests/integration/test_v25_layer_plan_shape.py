"""Independent SQL validation of composition plans; no admission authority."""
from copy import deepcopy
from hashlib import sha256
from uuid import uuid4, uuid5, UUID
import json
from psycopg.types.json import Jsonb
from test_v25_layer_params_shape import params
from photo_worker.operations.layers.resources import layer_resources


def plan():
    workflow = uuid4()
    body = params()
    wire = json.dumps(body, separators=(',', ':'), sort_keys=True)
    resources = layer_resources(5, 5, mask_refs=1, mask_nodes=1, tile_size=512)
    node = dict(task_id=str(uuid5(workflow, 'layer.compose:0')), ordinal=0,
                params=dict(artifact_id=str(uuid4()), sha256=sha256(wire.encode()).hexdigest()),
                params_jcs=wire, input_digest='a'*64, width=5, height=5,
                profile=dict(backend='cpu', renderer_version='v2.2', resource_estimate=resources))
    config = dict(project_id=str(uuid4()), input_snapshot_id=str(uuid4()),
                  v25_composition=dict(auth_session_id=str(uuid4()), nodes=[node]))
    return workflow, config, resources['output_bytes']


def test_layer_plan_shape(db):
    workflow, config, output = plan()
    db.execute('SET LOCAL ROLE photo_scheduler')
    def check(value, size=output, identity=workflow):
        return db.execute('SELECT photo.v25_layer_plan_shape(%s,%s,%s)', (identity, Jsonb(value), size)).fetchone()[0]
    assert check(config) is True
    multiple=deepcopy(config)
    second=deepcopy(multiple['v25_composition']['nodes'][0])
    second['ordinal']=1;second['task_id']=str(uuid5(workflow,'layer.compose:1'))
    multiple['v25_composition']['nodes'].append(second)
    assert check(multiple,2*output) is True
    assert check(config, output+1) is False
    assert check(config, identity=uuid4()) is False
    assert check(config, identity=UUID(int=0)) is False
    for field, value in [('ordinal',1),('ordinal',True),('task_id',str(uuid4())),('width',0),
                         ('height',200_000_001),('width','5'),('params_jcs','{}'),
                         ('input_digest','A'*64),('extra',0)]:
        wrong=deepcopy(config);wrong['v25_composition']['nodes'][0][field]=value
        assert check(wrong) is False,(field,value)
    for field in config['v25_composition']['nodes'][0]:
        wrong=deepcopy(config);del wrong['v25_composition']['nodes'][0][field]
        assert check(wrong) is False,field
    wrong=deepcopy(config);wrong['v25_composition']['nodes'][0]['profile']['resource_estimate']['ram_bytes']+=1
    assert check(wrong) is False
    wrong=deepcopy(config);wrong['v25_composition']['nodes']*=2
    assert check(wrong,2*output) is False
    for value in [None,[],{},dict(config['v25_composition'],extra=True)]:
        wrong=deepcopy(config);wrong['v25_composition']=value
        assert check(wrong) is False
    wrong=deepcopy(config);wrong['v25_composition']['nodes']=[]
    assert check(wrong) is False
    for field in ['project_id','input_snapshot_id']:
        wrong=deepcopy(config);wrong[field]=str(UUID(int=0))
        assert check(wrong) is False
    for value in [None,[],{},dict(config,extra=True)]:assert check(value) is False
    assert db.execute("SELECT has_function_privilege('photo_api','photo.v25_layer_plan_shape(uuid,jsonb,bigint)','EXECUTE')").fetchone()[0] is False
