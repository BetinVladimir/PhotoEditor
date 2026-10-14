"""Private publication scope only; no object publication or Apply admission."""
from uuid import uuid4,uuid5
import pytest
import psycopg
from psycopg.types.json import Jsonb
from test_v27_semantic_final_producer_gate import fixture


def test_approved_params_scope_pins_proposed_target_and_live_parent(db):
    tid,task,result,_,_,candidate,scope=fixture(db,snapshot_authority=True)
    workflow,run=db.execute('SELECT w.id,w.run_id FROM photo.v2_workflows w JOIN photo.tasks t ON t.run_id=w.run_id WHERE t.id=%s',(task,)).fetchone()
    db.execute("UPDATE photo.runs SET status='waiting_review' WHERE id=%s",(run,))
    db.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,'v2-adopt',photo.v2_adoption_event_id(%s,%s),clock_timestamp())",(tid,task,result))
    revision=db.execute('SELECT revision FROM photo.runs WHERE id=%s',(run,)).fetchone()[0]
    intent=db.execute('INSERT INTO photo.semantic_intents(tenant_id,project_id,workflow_id,reference_version_id,snapshot_id,intent_artifact_id,intent_sha) VALUES(%s,%s,%s,%s,%s,%s,%s) RETURNING id',(tid,scope['project'],workflow,scope['version'],scope['snapshot'],scope['source'],'5'*64)).fetchone()[0]
    target=db.execute("INSERT INTO photo.semantic_targets(tenant_id,project_id,intent_id,asset_version_id,candidate_artifact_id,state) VALUES(%s,%s,%s,%s,%s,'proposed') RETURNING id",(tid,scope['project'],intent,scope['version'],candidate)).fetchone()[0]
    digest=db.execute('SELECT sha256 FROM photo.artifacts WHERE id=%s',(candidate,)).fetchone()[0]
    args=(tid,intent,scope['actor'],scope['session'],revision,target,1,scope['version'],candidate,digest)
    def lock(values=args):return db.execute('SELECT photo.lock_semantic_approved_params_publication(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',values).fetchone()[0]
    assert lock()==run
    for index in (0,1,2,3,4,5,6,7,8,9):
        changed=list(args);changed[index]=revision+1 if index==4 else 2 if index==6 else '0'*64 if index==9 else uuid4()
        with db.transaction(force_rollback=True):
            with pytest.raises((psycopg.errors.SerializationFailure,psycopg.errors.InsufficientPrivilege)):
                lock(tuple(changed))
    for query,values in (
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(candidate,)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(query,values)
            with pytest.raises((psycopg.errors.SerializationFailure,psycopg.errors.InsufficientPrivilege)):lock()
    signature='photo.lock_semantic_approved_params_publication(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()[0]

    owner,artifact=uuid4(),uuid4()
    sha='a'*64;size=123;bucket='fixture'
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT(tenant_id) DO NOTHING',(tid,))
    before=db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()
    def reserve(aid=artifact,digest=sha):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_api')
            row=db.execute('SELECT * FROM photo.reserve_semantic_approved_params_document('+','.join(['%s']*15)+')',(*args,owner,aid,digest,size,bucket)).fetchone()
            db.execute('RESET ROLE')
            return row
    first=reserve();assert first[0]=='created' and first[1]==artifact and first[3] is False
    resumed=reserve();assert resumed[0]=='resumed' and resumed[2]!=first[2]
    assert reserve(uuid4())[0]=='conflict'
    assert reserve(digest='b'*64)[0]=='conflict'
    def complete(token=resumed[2],version='v2'):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_api')
            row=db.execute('SELECT photo.complete_semantic_approved_params_document('+','.join(['%s']*17)+')',(*args,owner,artifact,token,sha,size,bucket,version)).fetchone()[0]
            db.execute('RESET ROLE')
            return row
    assert complete(first[2]) is False
    assert complete() is True
    assert complete() is True
    assert complete(version='other-version') is False
    assert reserve()[0]=='replayed'
    after=db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()
    assert after==(before[0]+size,before[1])
    pin=db.execute('SELECT sha256,bytes,output_role,object_version FROM photo.artifacts WHERE id=%s',(artifact,)).fetchone()
    assert pin==(sha,size,'task_params','v2')

    child=uuid4();transport_owner=uuid5(child,'semantic.edit:'+str(target))
    for purpose in ('v2_input_chunk_0','v2_input_manifest'):
        transport=uuid5(transport_owner,purpose)
        base=(*args,child,purpose,transport_owner,transport)
        usage=db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()
        def reserve_transport(values=base):
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_api')
                result=db.execute('SELECT * FROM photo.reserve_semantic_approved_transport_document('+','.join(['%s']*17)+')',(*values,sha,size,bucket)).fetchone()
                db.execute('RESET ROLE');return result
        initial=reserve_transport();assert initial[0]=='created' and initial[1]==transport
        retry=reserve_transport();assert retry[0]=='resumed' and retry[2]!=initial[2]
        def complete_transport(token=retry[2],version='transport-v1'):
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_api')
                result=db.execute('SELECT photo.complete_semantic_approved_transport_document('+','.join(['%s']*19)+')',(*base,token,sha,size,bucket,version)).fetchone()[0]
                db.execute('RESET ROLE');return result
        assert complete_transport(initial[2]) is False
        assert complete_transport() is True
        assert complete_transport() is True
        assert complete_transport(version='changed') is False
        assert reserve_transport()[0]=='replayed'
        assert db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()==(usage[0]+size,usage[1])
        assert db.execute('SELECT output_role FROM photo.artifacts WHERE id=%s',(transport,)).fetchone()[0]==purpose
        for index,value in ((10,uuid4()),(11,'task_params'),(11,'v2_input_chunk_1954'),(12,uuid4()),(13,uuid4())):
            changed=list(base);changed[index]=value
            with pytest.raises(psycopg.errors.InvalidParameterValue):
                with db.transaction(force_rollback=True):reserve_transport(tuple(changed))
    assert db.execute('SELECT count(*) FROM photo.runs WHERE id=%s',(child,)).fetchone()[0]==0
    assert db.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(child,)).fetchone()[0]==0
    manifest=dict(artifact_id=str(uuid5(transport_owner,'v2_input_manifest')),sha256=sha)
    chunks=[dict(artifact_id=str(uuid5(transport_owner,'v2_input_chunk_0')),sha256=sha)]
    def transport_ready(parent=run,owner=transport_owner,count=1,m=manifest,c=chunks):
        return db.execute('SELECT photo.semantic_approved_transport_ready(%s,%s,%s,%s,%s,%s)',(tid,parent,owner,count,Jsonb(m),Jsonb(c))).fetchone()[0]
    assert transport_ready() is True
    assert transport_ready(parent=uuid4()) is False
    assert transport_ready(owner=uuid4()) is False
    assert transport_ready(count=513) is False
    assert transport_ready(c=[]) is False
    assert transport_ready(m=chunks[0]) is False
    assert transport_ready(m={**manifest,'sha256':'b'*64}) is False
    nodes=[dict(id=str(transport_owner),params_artifact_id=str(artifact),params_sha=sha)]
    transport_rows=[dict(task_id=str(transport_owner),entry_count=1,entries_sha='a'*64,manifest=manifest,chunks=chunks,direct_roots=[dict(artifact_id=str(artifact),sha256=sha)])]
    def batch_ready(n=nodes,t=transport_rows):
        return db.execute('SELECT photo.semantic_approved_transport_batch_ready(%s,%s,%s,%s)',(tid,run,Jsonb(n),Jsonb(t))).fetchone()[0]
    assert batch_ready() is True
    assert batch_ready(n=[dict(id=str(transport_owner))]) is False
    assert batch_ready(n=[{**nodes[0],'params_artifact_id':str(uuid4())}]) is False
    assert batch_ready(n=[{**nodes[0],'params_sha':'b'*64}]) is False
    assert batch_ready(n=[{**nodes[0],'params_artifact_id':manifest['artifact_id']}]) is False
    assert batch_ready(t=[]) is False
    assert batch_ready(n=[dict(id=str(uuid4()))]) is False
    assert batch_ready(n=nodes+nodes,t=transport_rows+transport_rows) is False
    assert batch_ready(t=[{**transport_rows[0],'direct_roots':[]}]) is False
    assert batch_ready(t=[{**transport_rows[0],'direct_roots':[manifest]}]) is False
    assert batch_ready(t=[{**transport_rows[0],'direct_roots':[dict(artifact_id=str(artifact),sha256='b'*64)]}]) is False
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(manifest['artifact_id'],))
        assert transport_ready() is False
    signature='photo.semantic_approved_transport_ready(uuid,uuid,uuid,integer,jsonb,jsonb)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0]
    # Owner-seeded relational draft only, not a real review acceptance proof.
    params_id=uuid5(transport_owner,'Params')
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES(%s,%s,'fixture',%s,%s,123,'application/json','v1',clock_timestamp())",(params_id,tid,str(params_id),sha))
    config=dict(project_id=str(scope['project']),approved_semantic=dict(nodes=[dict(task_id=str(transport_owner),target_id=str(target),params_artifact_id=str(params_id),params_sha=sha)]))
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'apply','draft',%s,%s,%s,%s,'fail_fast')",(child,tid,scope['snapshot'],Jsonb(config),'b'*64,scope['actor']))
    batch_id=db.execute("INSERT INTO photo.v2_review_batches(tenant_id,project_id,workflow_id,decisions_artifact_id,actor_id,action,child_run_id) VALUES(%s,%s,%s,%s,%s,'apply',%s) RETURNING id",(tid,scope['project'],workflow,scope['source'],scope['actor'],child)).fetchone()[0]
    db.execute("UPDATE photo.semantic_targets SET state='queued',batch_id=%s,child_run_id=%s WHERE id=%s",(batch_id,child,target))
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,%s,%s,'semantic.local_exposure','1','cpu','blocked','[]',%s,%s,'{}')",(transport_owner,tid,child,'semantic.edit:'+str(target),params_id,'b'*64))
    assert db.execute('SELECT photo.semantic_apply_input_scope(%s,%s,%s)',(tid,scope['project'],transport_owner)).fetchone()[0] is True
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes
    entries=[dict(contract='Params',ref=dict(artifact_id=str(params_id),sha256=sha))]
    digest=sha256(canonical_bytes(dict(entries=entries,operation='semantic.local_exposure',task=str(transport_owner),version='1'))).hexdigest()
    entries_digest=sha256(canonical_bytes(entries)).hexdigest()
    db.execute('UPDATE photo.tasks SET input_digest=%s WHERE id=%s',(digest,transport_owner))
    registration_nodes=[dict(id=str(transport_owner),operation='semantic.local_exposure',operation_version='1',params_artifact_id=str(params_id),params_sha=sha,input_digest=digest)]
    from semantic_qualification_fixture import frozen_qualification
    from test_v27_approved_exposure_nodes import node_payload
    pin=frozen_qualification(db,tid,scope['project'],scope['actor'],operation='semantic.local_exposure',backend='cpu')
    profile=node_payload(pin)[2][0]['execution_profile']
    registration_nodes[0].update(node_key='semantic.edit:'+str(target),resource_class='cpu',execution_profile=profile)
    db.execute('UPDATE photo.tasks SET execution_profile=%s WHERE id=%s',(Jsonb(profile),transport_owner))
    seal_params=[dict(task_id=str(transport_owner),target_id=str(target),artifact_id=str(params_id),sha256=sha,body='{"kind":"local_exposure"}')]
    registration_transport=[dict(task_id=str(transport_owner),entry_count=1,entries_sha=entries_digest,manifest=manifest,chunks=chunks,direct_roots=[entries[0]['ref']])]
    registration_inventory=[dict(task_id=str(transport_owner),entries=entries)]
    registration_args=(tid,scope['project'],run,child,Jsonb(registration_nodes),Jsonb(registration_transport),Jsonb(registration_inventory))
    def registered_ready():
        return db.execute('SELECT photo.semantic_apply_registered_inventory_ready(%s,%s,%s,%s,%s,%s)',(tid,scope['project'],child,Jsonb(registration_nodes),Jsonb(registration_transport),Jsonb(registration_inventory))).fetchone()[0]
    def sealed_ready(n=registration_nodes,t=registration_transport):
        return db.execute('SELECT photo.semantic_apply_sealed_inventory_ready(%s,%s,%s,%s,%s,%s)',(tid,scope['project'],child,Jsonb(n),Jsonb(t),Jsonb(registration_inventory))).fetchone()[0]
    assert sealed_ready() is False
    assert registered_ready() is False
    with db.transaction(force_rollback=True):
        assert db.execute('SELECT photo.register_semantic_apply_inputs(%s,%s,%s,%s,%s,%s,%s)',registration_args).fetchone()[0]==3
        assert registered_ready() is True
        for contract,ordinal in (('WorkingImage',0),('Params',5)):
            with db.transaction(force_rollback=True):
                db.execute('DELETE FROM photo.v2_task_inputs WHERE task_id=%s AND ordinal=0',(transport_owner,))
                db.execute('INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,%s,%s)',(tid,scope['project'],transport_owner,params_id,sha,contract,ordinal))
                assert db.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE task_id=%s',(transport_owner,)).fetchone()[0]==3
                assert registered_ready() is False
        assert registered_ready() is True
        assert db.execute('SELECT contract,ordinal FROM photo.v2_task_inputs WHERE task_id=%s ORDER BY ordinal',(transport_owner,)).fetchall()==[('Params',0),('InputClosureChunk',1000000),('InputClosureManifest',1000001)]
        assert db.execute('SELECT state FROM photo.tasks WHERE id=%s',(transport_owner,)).fetchone()[0]=='blocked'
        seal_args=(tid,scope['project'],run,child,Jsonb(seal_params),Jsonb(registration_nodes),Jsonb(registration_transport),Jsonb(registration_inventory))
        for statement,values in (
            ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
            ("UPDATE photo.tasks SET execution_profile=jsonb_set(execution_profile,'{runtime_lock_sha}',to_jsonb(%s::text)) WHERE id=%s",('0'*64,transport_owner)),
            ('DELETE FROM photo.v2_task_inputs WHERE task_id=%s AND ordinal=0',(transport_owner,)),
        ):
            with db.transaction(force_rollback=True):
                db.execute(statement,values)
                with pytest.raises(psycopg.errors.CheckViolation):
                    with db.transaction():db.execute('SELECT photo.seal_semantic_apply_inputs(%s,%s,%s,%s,%s,%s,%s,%s)',seal_args)
                assert db.execute("SELECT execution_profile ? 'v2_input_manifest' FROM photo.tasks WHERE id=%s",(transport_owner,)).fetchone()[0] is False
        with db.transaction(force_rollback=True):
            assert db.execute('SELECT photo.seal_semantic_apply_inputs(%s,%s,%s,%s,%s,%s,%s,%s)',seal_args).fetchone()[0]==1
            inputs,sealed,state=db.execute('SELECT inputs,execution_profile,state FROM photo.tasks WHERE id=%s',(transport_owner,)).fetchone()
            assert inputs==[manifest,entries[0]['ref']]
            assert sealed['v2_input_manifest']==manifest and sealed['v2_entries_sha']==entries_digest
            assert state=='blocked'
            assert sealed_ready() is True
            from copy import deepcopy
            changed=deepcopy(registration_nodes);changed[0]['execution_profile']['backend']='cuda'
            assert sealed_ready(n=changed) is False
            changed=deepcopy(registration_transport);changed[0]['manifest']['sha256']='0'*64
            assert sealed_ready(t=changed) is False
            with db.transaction(force_rollback=True):
                db.execute('DELETE FROM photo.v2_task_inputs WHERE task_id=%s AND ordinal=0',(transport_owner,))
                assert sealed_ready() is False
            assert sealed_ready() is True

            assert db.execute('SELECT status,graph_sealed FROM photo.runs WHERE id=%s',(child,)).fetchone()==('draft',False)
            for statement,values in (
                ("UPDATE photo.tasks SET execution_profile=jsonb_set(execution_profile,'{backend}','\"cuda\"') WHERE id=%s",(transport_owner,)),
                ("UPDATE photo.tasks SET execution_profile=jsonb_set(execution_profile,'{resource_estimate,ram_bytes}','1') WHERE id=%s",(transport_owner,)),
                ("UPDATE photo.tasks SET execution_profile=jsonb_set(execution_profile,'{qualification,policy_id}',to_jsonb(%s::text)) WHERE id=%s",(str(uuid4()),transport_owner)),
                ("UPDATE photo.tasks SET node_key='changed' WHERE id=%s",(transport_owner,)),
                ("UPDATE photo.runs SET config_snapshot=jsonb_set(config_snapshot,'{project_id}',to_jsonb(%s::text)) WHERE id=%s",(str(uuid4()),child)),
                ("UPDATE photo.runs SET config_hash=%s WHERE id=%s",('c'*64,child)),
                ("UPDATE photo.runs SET failure_policy='continue_items' WHERE id=%s",(child,)),
            ):
                with pytest.raises(psycopg.errors.ObjectNotInPrerequisiteState):
                    with db.transaction():db.execute(statement,values)
            with db.transaction(force_rollback=True):
                db.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(transport_owner,))
                db.execute('UPDATE photo.runs SET cancel_requested=true,revision=revision+1 WHERE id=%s',(child,))
                assert db.execute('SELECT cancel_requested FROM photo.tasks WHERE id=%s',(transport_owner,)).fetchone()[0] is True
                assert sealed_ready() is False
            with pytest.raises(psycopg.errors.ObjectNotInPrerequisiteState):
                with db.transaction():db.execute("UPDATE photo.tasks SET inputs='[]' WHERE id=%s",(transport_owner,))
            with pytest.raises(psycopg.errors.CheckViolation):
                with db.transaction():db.execute('SELECT photo.seal_semantic_apply_inputs(%s,%s,%s,%s,%s,%s,%s,%s)',seal_args)
        for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway'):
            assert not db.execute("SELECT has_function_privilege(%s,'photo.seal_semantic_apply_inputs(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb)','EXECUTE')",(role,)).fetchone()[0]

        with db.transaction(force_rollback=True):
            db.execute('INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,%s,%s)',(tid,scope['project'],transport_owner,scope['source'],'5'*64,'WorkingImage',1))
            assert registered_ready() is False
        assert registered_ready() is True
        with pytest.raises(psycopg.errors.CheckViolation):
            with db.transaction():db.execute('SELECT photo.register_semantic_apply_inputs(%s,%s,%s,%s,%s,%s,%s)',registration_args)
    with db.transaction(force_rollback=True):
        legacy=uuid4()
        db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'apply','draft',%s,'{}',%s,%s,'continue_items')",(legacy,tid,scope['snapshot'],'a'*64,scope['actor']))
        db.execute("UPDATE photo.runs SET config_snapshot='{\"legacy\":true}',config_hash=%s WHERE id=%s",('b'*64,legacy))
        assert db.execute('SELECT config_snapshot FROM photo.runs WHERE id=%s',(legacy,)).fetchone()[0]==dict(legacy=True)
        with pytest.raises(psycopg.errors.ObjectNotInPrerequisiteState):
            with db.transaction():db.execute('UPDATE photo.runs SET config_snapshot=%s WHERE id=%s',(Jsonb(config),legacy))
    assert db.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE task_id=%s',(transport_owner,)).fetchone()[0]==0
    assert registered_ready() is False
    row=(tid,scope['project'],transport_owner,params_id,sha,'Params',0)
    db.execute('INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,%s,%s)',row)
    for query,values in (
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(child,)),
        ('UPDATE photo.projects SET archived_at=clock_timestamp() WHERE id=%s',(scope['project'],)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(query,values)
            assert db.execute('SELECT photo.semantic_apply_input_scope(%s,%s,%s)',(tid,scope['project'],transport_owner)).fetchone()[0] is False
    with pytest.raises(psycopg.errors.CheckViolation):
        with db.transaction(force_rollback=True):
            db.execute('INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,%s,%s)',(tid,uuid4(),transport_owner,artifact,sha,'WorkingImage',1))
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],))
        with pytest.raises(psycopg.errors.InsufficientPrivilege):reserve()

    for name,types in (
        ('reserve_semantic_approved_params_document','uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,uuid,uuid,text,bigint,text'),
        ('complete_semantic_approved_params_document','uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,uuid,uuid,uuid,text,bigint,text,text'),
        ('reserve_semantic_approved_transport_document','uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,uuid,text,uuid,uuid,text,bigint,text'),
        ('complete_semantic_approved_transport_document','uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,uuid,text,uuid,uuid,uuid,text,bigint,text,text'),
    ):
        for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway'):
            assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,'photo.'+name+'('+types+')')).fetchone()[0] is (role=='photo_api')
