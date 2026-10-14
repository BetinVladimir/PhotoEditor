from copy import deepcopy
from uuid import uuid4,uuid5
from psycopg.types.json import Jsonb
from test_v27_exposure_qualified_admission import qualified_adopted_fixture


def node_payload(pin,child=None):
    child,target=child or uuid4(),uuid4()
    task=uuid5(child,'semantic.edit:'+str(target));params=uuid5(task,'Params')
    pixels=480
    estimate=dict(ram_bytes=204*pixels+67108864+1,vram_bytes=0,
        scratch_bytes=20*pixels+3*16384+16777216,output_bytes=12*pixels+16384+3*1048576,pixel_count=pixels)
    profile=dict(backend='cpu',renderer_version='v2.2',resource_estimate=estimate,
        qualification=pin,**{key:pin[key] for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')})
    rows=[dict(task_id=str(task),target_id=str(target),artifact_id=str(params),sha256='a'*64,body='{"kind":"local_exposure"}')]
    nodes=[dict(id=str(task),node_key='semantic.edit:'+str(target),operation='semantic.local_exposure',operation_version='1',resource_class='cpu',
        params_artifact_id=str(params),params_sha='a'*64,input_digest='b'*64,execution_profile=profile)]
    return child,rows,nodes


def test_private_approved_nodes_bind_identity_resources_and_live_policy(db):
    fixture=qualified_adopted_fixture(db)
    tenant,project,pin=fixture['tenant'],fixture['scope']['project'],fixture['pin']
    child,rows,nodes=node_payload(pin)
    def ready(n=nodes,p=rows,c=child):
        return db.execute('SELECT photo.semantic_approved_exposure_nodes_ready(%s,%s,%s,%s,%s)',(tenant,project,c,Jsonb(p),Jsonb(n))).fetchone()[0]
    assert ready() is True

    for role in ('photo_api','photo_scheduler','photo_worker'):
        assert db.execute("SELECT has_function_privilege(%s,'photo.semantic_approved_exposure_nodes_ready(uuid,uuid,uuid,jsonb,jsonb)','EXECUTE')",(role,)).fetchone()[0] is False
    for field,value in (('id',str(uuid4())),('node_key','semantic.local_exposure:0'),('params_sha','c'*64),('params_artifact_id',str(uuid4())),('operation','semantic.transfer'),('resource_class','gpu'),('input_digest','bad')):
        n=deepcopy(nodes);n[0][field]=value;assert ready(n) is False
    for field in ('ram_bytes','scratch_bytes','output_bytes'):
        n=deepcopy(nodes);n[0]['execution_profile']['resource_estimate'][field]-=1;assert ready(n) is False
    for field,value in (('vram_bytes',1),('pixel_count',200000001),('ram_bytes',None),('output_bytes',1.5)):
        n=deepcopy(nodes);n[0]['execution_profile']['resource_estimate'][field]=value;assert ready(n) is False
    n=deepcopy(nodes);n[0]['execution_profile']['runtime_lock_sha']='c'*64;assert ready(n) is False
    n=deepcopy(nodes);n[0]['extra']=1;assert ready(n) is False
    p=deepcopy(rows);p[0]['body']='{"kind":"remove_object"}';assert ready(p=p) is False
    assert ready(c=uuid4()) is False
    assert ready([]) is False
    assert ready(nodes+nodes,rows+rows) is False
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],))
        assert ready() is False
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(pin['report']['artifact_id'],))
        assert ready() is False
    assert ready() is True


def test_two_node_batch_locks_qualification_until_both_readers_commit(database):
    import psycopg
    import pytest
    with psycopg.connect(**database) as seed:
        fixture=qualified_adopted_fixture(seed)
        tenant,project=fixture['tenant'],fixture['scope']['project']
        # One active admission policy per operation/version is a DB invariant.
        pins=[fixture['pin'],fixture['pin']]
        child,rows,nodes=node_payload(pins[0])
        _,other_rows,other_nodes=node_payload(pins[1],child)
        rows+=other_rows;nodes+=other_nodes
    with psycopg.connect(**database) as first,psycopg.connect(**database) as second,psycopg.connect(**database) as writer:
        query='SELECT photo.semantic_approved_exposure_nodes_ready(%s,%s,%s,%s,%s)'
        args=(tenant,project,child,Jsonb(rows),Jsonb(nodes))
        first.execute("SET LOCAL statement_timeout='3s'")
        assert first.execute(query,args).fetchone()[0] is True
        second.execute("SET LOCAL statement_timeout='3s'")
        assert second.execute(query,(tenant,project,child,Jsonb(rows[::-1]),Jsonb(nodes[::-1]))).fetchone()[0] is True
        for pin in pins[:1]:
            for statement,value in (
                ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',pin['policy_id']),
                ("UPDATE photo.model_qualifications SET status='failed' WHERE id=%s",pin['qualification_id']),
                ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',pin['report']['artifact_id']),
                ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',pin['calibration']['artifact_id']),
            ):
                with pytest.raises(psycopg.errors.LockNotAvailable):
                    with writer.transaction():
                        writer.execute("SET LOCAL lock_timeout='100ms'")
                        writer.execute(statement,(value,))
        first.commit()
        # The reversed-input reader independently retains the same authority.
        with pytest.raises(psycopg.errors.LockNotAvailable):
            with writer.transaction():
                writer.execute("SET LOCAL lock_timeout='100ms'")
                writer.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pins[1]['policy_id'],))
        second.commit()
        with writer.transaction():
            writer.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pins[1]['policy_id'],))
        assert first.execute(query,args).fetchone()[0] is False
