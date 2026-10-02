"""SQL resource estimates agree with the actual worker; no graph admission."""
from copy import deepcopy
from uuid import uuid4
from psycopg.types.json import Jsonb
from photo_worker.algorithms.layer_masks import compile_mask
from photo_worker.operations.layers.resources import layer_resources
from test_v25_layer_params_shape import params


def test_v25_layer_sql_resources_and_profile_match_worker(db):
    db.execute('SET LOCAL ROLE photo_scheduler')
    for variant in ['leaf','shared','protected_shared','protected_distinct','wide_dag']:
        body=params();leaf=body['mask']
        if variant in {'shared','protected_shared','protected_distinct'}:
            body['mask']=dict(op='union',left=leaf,right=dict(op='invert',operand=deepcopy(leaf)))
        if variant=='protected_shared':body['protected_mask']=deepcopy(leaf['mask'])
        if variant=='protected_distinct':body['protected_mask']=dict(artifact_id=str(uuid4()),sha256='d'*64)
        if variant=='wide_dag':
            leaves=[dict(op='ref',mask=dict(artifact_id=str(uuid4()),sha256='a'*64),canvas_hash='b'*64) for _ in range(65)]
            while len(leaves)>1:
                leaves=[dict(op='union',left=leaves[i],right=leaves[i+1]) if i+1<len(leaves) else leaves[i] for i in range(0,len(leaves),2)]
            body['mask']=leaves[0]
        plan=compile_mask(body['mask'],body['base']['canvas_hash'])
        refs={(node.ref.artifact_id,node.ref.sha256) for node in plan.nodes if node.ref is not None}
        if body['protected_mask'] is not None:
            from uuid import UUID
            refs.add((UUID(body['protected_mask']['artifact_id']),body['protected_mask']['sha256']))
        for width,height in [(257,2),(513,513),(200_000_000,1)]:
            expected=layer_resources(width,height,mask_refs=len(refs),mask_nodes=len(plan.nodes),tile_size=512)
            actual=db.execute('SELECT photo.v25_layer_resources(%s,%s,%s)',(Jsonb(body),width,height)).fetchone()[0]
            assert actual==expected,(variant,width,height)
            profile=dict(backend='cpu',renderer_version='v2.2',resource_estimate=expected)
            args=(Jsonb(body),width,height,Jsonb(profile))
            assert db.execute('SELECT photo.v25_layer_profile_shape(%s,%s,%s,%s)',args).fetchone()[0] is True
            for field in expected:
                wrong=deepcopy(profile);wrong['resource_estimate'][field]+=1
                assert db.execute('SELECT photo.v25_layer_profile_shape(%s,%s,%s,%s)',(*args[:3],Jsonb(wrong))).fetchone()[0] is False
            for field,value in [('backend','gpu'),('renderer_version','v2.1'),('extra',True)]:
                wrong=deepcopy(profile);wrong[field]=value
                assert db.execute('SELECT photo.v25_layer_profile_shape(%s,%s,%s,%s)',(*args[:3],Jsonb(wrong))).fetchone()[0] is False


def test_v25_layer_sql_resource_invalid_bounds_and_permissions(db):
    body=params()
    for width,height in [(0,1),(1,0),(-1,1),(1,-1),(200_000_001,1),(1,200_000_001),(2**63-1,1),(1,2**63-1),(None,1)]:
        assert db.execute('SELECT photo.v25_layer_resources(%s,%s,%s)',(Jsonb(body),width,height)).fetchone()[0] is None
    body['mask']['canvas_hash']='c'*64
    assert db.execute('SELECT photo.v25_layer_resources(%s,1,1)',(Jsonb(body),)).fetchone()[0] is None
    assert db.execute("SELECT has_function_privilege('photo_api','photo.v25_layer_resources(jsonb,bigint,bigint)','EXECUTE')").fetchone()[0] is False
