"""Apply every migration and verify the narrow worker admission boundary."""
from hashlib import sha256
from uuid import uuid4
import psycopg


SIGNATURE = 'photo.admit_v25_lens_user_composition(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'


def test_worker_admission_grants_and_missing_lease_denial(database):
    with psycopg.connect(**database, autocommit=True) as db:
        for role, expected in [('photo_worker', True), ('photo_api', False), ('photo_scheduler', False)]:
            assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                (role, SIGNATURE, 'EXECUTE')).fetchone() == (expected,)
        assert db.execute('SELECT prosecdef,proconfig FROM pg_proc WHERE oid=%s::regprocedure',
                          (SIGNATURE,)).fetchone() == (True, ['search_path=pg_catalog, photo'])
        raw = '{}'
        args = (uuid4(), uuid4(), uuid4(), 1, 1, uuid4(), uuid4(),
                sha256(raw.encode()).hexdigest(), raw, 'e'*64)
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_worker')
            assert db.execute('SELECT photo.admit_v25_lens_user_composition(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                              args).fetchone() == (False,)
            altered = args[:7]+('0'*64,)+args[8:]
            assert db.execute('SELECT photo.admit_v25_lens_user_composition(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
                              altered).fetchone() == (False,)


def assert_live_composition_admission(db, tenant, shell, client, bucket, scratch_root, database):
    """Test relational running-state proof; synthetic provisioning is rolled back.

    Does not qualify a build, activate the public registry, or execute pixels.
    Inputs and adopted parent are published by the actual lens fixture.
    """
    run, task, params_id, params_sha, digest, _ = shell
    worker, node = uuid4(), 'layer-admission-'+str(uuid4())
    with db.transaction(force_rollback=True):
        db.execute("INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes) VALUES(%s,1000000000,0,1000000000)", (node,))
        db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions) VALUES(%s,%s,'[\"cpu\"]','[\"cpu\"]',%s,now(),'{}',session_user,%s,true,'{\"layer.compose\":[\"cpu\"]}','[\"v2.2\"]')", (tenant,worker,'e'*64,node))
        db.execute("UPDATE photo.runs SET status='running',graph_sealed=true WHERE tenant_id=%s AND id=%s", (tenant,run))
        db.execute("UPDATE photo.tasks SET state='running',attempt=1,fencing_token=1,worker_id=%s,lease_until=clock_timestamp()+interval '60 seconds',deadline=clock_timestamp()+interval '120 seconds' WHERE tenant_id=%s AND id=%s", (worker,tenant,task))
        db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity,tenant_id,task_id,fencing_token,lease_until) VALUES(%s,'cpu1','cpu','{}',%s,%s,1,clock_timestamp()+interval '60 seconds')", (node,tenant,task))
        config = db.execute('SELECT config_snapshot FROM photo.runs WHERE id=%s', (run,)).fetchone()[0]
        plan = next(n for n in config['v25_composition']['nodes'] if n['task_id']==str(task))
        args = (tenant,run,task,1,1,worker,params_id,params_sha,plan['params_jcs'],digest)
        query = 'SELECT photo.admit_v25_lens_user_composition(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)'
        def admitted(values=args):
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_worker')
                try:
                    return db.execute(query,values).fetchone()[0]
                finally:
                    db.execute('RESET ROLE')
        assert admitted() is True
        for index, wrong in [(0,uuid4()),(1,uuid4()),(2,uuid4()),(3,2),(4,2),(5,uuid4()),
                             (6,uuid4()),(7,'0'*64),(8,'{}'),(9,'0'*64)]:
            values=list(args);values[index]=wrong
            assert admitted(tuple(values)) is False, index
        db.execute("UPDATE photo.tasks SET lease_until=clock_timestamp()-interval '1 second' WHERE id=%s", (task,))
        assert admitted() is False
        db.execute("UPDATE photo.tasks SET lease_until=clock_timestamp()+interval '60 seconds' WHERE id=%s", (task,))
        db.execute('UPDATE photo.resource_slots SET fencing_token=2 WHERE task_id=%s', (task,))
        assert admitted() is False
        db.execute('UPDATE photo.resource_slots SET fencing_token=1 WHERE task_id=%s', (task,))
        db.execute("UPDATE photo.resource_slots SET lease_until=clock_timestamp()-interval '1 second' WHERE task_id=%s", (task,))
        assert admitted() is False
        db.execute("UPDATE photo.resource_slots SET lease_until=clock_timestamp()+interval '60 seconds' WHERE task_id=%s", (task,))
        db.execute("UPDATE photo.worker_registrations SET db_principal='unrelated_worker' WHERE worker_id=%s", (worker,))
        assert admitted() is False
        db.execute('UPDATE photo.worker_registrations SET db_principal=session_user WHERE worker_id=%s', (worker,))
        db.execute('UPDATE photo.worker_registrations SET enabled=false WHERE worker_id=%s', (worker,))
        assert admitted() is False
        db.execute('UPDATE photo.worker_registrations SET enabled=true WHERE worker_id=%s', (worker,))
        assert admitted() is True
        with db.transaction(force_rollback=True):
            # Relational lookup only: synthetic terminal metadata is rolled back.
            # No valid composition JSON, physical output or atomic adoption is inferred.
            from psycopg.types.json import Jsonb
            db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),'cpu',%s)",(tenant,task,worker,Jsonb(shell[5])))
            from photo_worker.contracts.base import canonical_bytes,ArtifactRef
            from photo_worker.contracts.images import WorkingImage,MaskRef
            from photo_worker.operations.layers.contracts import LayerComposeParams
            from photo_worker.operations.optical.contracts import ImageResult,ImageReport
            from io import BytesIO
            import numpy as np
            typed=LayerComposeParams.model_validate_json(plan['params_jcs'])
            region_asset=db.execute('SELECT asset_id FROM photo.asset_versions WHERE tenant_id=%s AND id=%s',(tenant,typed.base.asset_version_id)).fetchone()[0]
            region_actor=db.execute('SELECT created_by FROM photo.runs WHERE id=%s',(run,)).fetchone()[0]
            region_args=(tenant,region_asset,typed.base.asset_version_id,region_actor,config['v25_composition']['auth_session_id'])
            region_query='SELECT * FROM photo.v26_region_source(%s,%s,%s,%s,%s)'
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_api')
                try:
                    source_row=db.execute(region_query,region_args).fetchone()
                    assert source_row is not None and source_row[1:3]==(typed.base.source.artifact_id,typed.base.source.sha256)
                    for index in range(5):
                        invalid=list(region_args);invalid[index]=uuid4()
                        assert db.execute(region_query,tuple(invalid)).fetchone() is None
                finally:db.execute('RESET ROLE')
            source_location=db.execute('SELECT object_key,object_version FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(tenant,typed.base.source.artifact_id)).fetchone()
            response=client.get_object(bucket,source_location[0],version_id=source_location[1])
            try:source=WorkingImage.model_validate_json(response.read())
            finally:response.close();response.release_conn()
            def publish(raw,media,role):
                identifier=uuid4();digest=sha256(raw).hexdigest();key='composition-proof/'+str(identifier)
                version=client.put_object(bucket,key,BytesIO(raw),len(raw),content_type=media).version_id
                db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,clock_timestamp(),%s,%s,1,1)",(identifier,tenant,bucket,key,digest,len(raw),media,version,role,task))
                db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES(%s,%s,1,1,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,'ready',%s)",(tenant,task,worker,'f'*64,uuid4(),identifier,role,bucket,key,digest,len(raw),media,version))
                return ArtifactRef(artifact_id=identifier,sha256=digest)
            def document(value,role):return publish(canonical_bytes(value),'application/json',role)
            stream=BytesIO();np.save(stream,np.zeros((source.canvas.height,source.canvas.width),np.float32),allow_pickle=False)
            changed_pixels=publish(stream.getvalue(),'application/x-npy','mask_pixels')
            changed=document(MaskRef(artifact=changed_pixels,canvas=source.canvas,transform_chain=source.transform_chain,selector='changed',confidence=1.,model_lock_id=None,protected=False),'mask')
            report=document(ImageReport(schema_version=1,status='pass',operation='layer.compose',skipped_count=0,reason=None,outside_mask_max_error=0.,clipping_fraction=0.,identity=None,transform=None,sources=[typed.base],registrations=[]),'report')
            root=document(ImageResult(schema_version=1,working_image=typed.base.source,changed_mask=changed,generated_mask=None,report=report),'document')
            result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,review_required,result_digest) VALUES(%s,%s,1,1,%s,'{}',false,%s) RETURNING id",(tenant,task,Jsonb([root.model_dump(mode='json')]),'b'*64)).fetchone()[0]
            db.execute("UPDATE photo.tasks SET state='succeeded',lease_until=NULL,canonical_result_id=%s WHERE id=%s",(result,task))
            lookup='SELECT * FROM photo.v25_layer_adoption_params(%s,%s,%s,%s,%s)'
            policy_lookup='SELECT photo.v25_layer_adoption_alpha_policy(%s,%s,%s,%s,%s)'
            assert db.execute(policy_lookup,(tenant,task,1,1,result)).fetchone()==('require_float32',)
            def parameters(values):
                with db.transaction():
                    db.execute('SET LOCAL ROLE photo_scheduler')
                    try:return db.execute(lookup,values).fetchone()
                    finally:db.execute('RESET ROLE')
            lookup_args=(tenant,task,1,1,result)
            assert parameters(lookup_args)==(params_id,params_sha)
            proof_scratch=scratch_root/'composition-proof';proof_scratch.mkdir(exist_ok=True)
            from photo_worker.v2.adoption_store import PostgresObjectEvidenceReader
            from photo_worker.v2.adoption import ArtifactRef as EvidenceRef,prepare_v2_adoption
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_scheduler')
                try:
                    reader=PostgresObjectEvidenceReader(db,client,tenant_id=tenant,task_id=task,attempt=1,fence=1,allowed_bucket=bucket)
                    prepared=prepare_v2_adoption(task_id=task,operation='layer.compose',version='1',root=EvidenceRef(root.artifact_id,root.sha256),reader=reader,params_ref=EvidenceRef(params_id,params_sha),layer_scratch_root=proof_scratch,layer_scratch_bytes=100000000)
                    assert prepared.inventory
                    db.execute('RESET ROLE')
                    changes=[
                        ("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s",(config['v25_composition']['auth_session_id'],)),
                        ("UPDATE photo.runs SET cancel_requested=true WHERE id=%s",(run,)),
                        ("UPDATE photo.tasks SET cancel_requested=true WHERE id=%s",(task,)),
                        ("UPDATE photo.tasks SET fencing_token=fencing_token+1 WHERE id=%s",(task,)),
                        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(params_id,)),
                        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(typed.proposal_result.artifact_id,)),
                    ]
                    for mutation,arguments in changes:
                        with db.transaction(force_rollback=True):
                            db.execute(mutation,arguments)
                            db.execute('SET LOCAL ROLE photo_scheduler')
                            try:
                                assert db.execute('SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,%s,%s)',
                                    (tenant,task,result,Jsonb(list(prepared.inventory)),prepared.digest)).fetchone()==(False,)
                            finally:db.execute('RESET ROLE')
                            assert db.execute("SELECT count(*) FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(%s,%s)",(tenant,task,result)).fetchone()==(0,)
                            assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v2.task.adopted'",(tenant,run)).fetchone()==(0,)
                            assert db.execute('SELECT count(*) FROM photo.v2_artifact_links WHERE tenant_id=%s AND workflow_id=(SELECT id FROM photo.v2_workflows WHERE tenant_id=%s AND run_id=%s)',(tenant,tenant,run)).fetchone()==(0,)
                    db.execute('SET LOCAL ROLE photo_scheduler')
                    assert db.execute('SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,%s,%s)',
                        (tenant,task,result,Jsonb(list(prepared.inventory)),prepared.digest)).fetchone()==(True,)
                    assert db.execute('SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,%s,%s)',
                        (tenant,task,result,Jsonb(list(prepared.inventory)),prepared.digest)).fetchone()==(False,)
                finally:db.execute('RESET ROLE')
            # An independent SQL connection must not mutate authority rows before commit.
            import pytest
            with psycopg.connect(**database,autocommit=True,connect_timeout=5) as authority_writer:
                for statement,arguments in [
                    ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(config['v25_composition']['auth_session_id'],)),
                    ('UPDATE photo.users SET status=status WHERE id=%s',(config.get('created_by',None) or db.execute('SELECT created_by FROM photo.runs WHERE id=%s',(run,)).fetchone()[0],)),
                    ('UPDATE photo.memberships SET role=role WHERE tenant_id=%s AND user_id=(SELECT created_by FROM photo.runs WHERE id=%s)',(tenant,run)),
                    ('UPDATE photo.projects SET name=name WHERE tenant_id=%s AND id=(SELECT project_id FROM photo.v2_workflows WHERE tenant_id=%s AND run_id=%s)',(tenant,tenant,run)),
                ]:
                    with authority_writer.transaction(force_rollback=True):
                        authority_writer.execute("SET LOCAL lock_timeout='150ms'")
                        authority_writer.execute("SET LOCAL statement_timeout='1000ms'")
                        with pytest.raises(psycopg.errors.LockNotAvailable):
                            authority_writer.execute(statement,arguments)
            assert db.execute("SELECT count(*) FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(%s,%s)",(tenant,task,result)).fetchone()==(1,)
            assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v2.task.adopted'",(tenant,run)).fetchone()==(1,)
            workflow=db.execute('SELECT id FROM photo.v2_workflows WHERE tenant_id=%s AND run_id=%s',(tenant,run)).fetchone()[0]
            assert db.execute('SELECT count(*) FROM photo.v2_artifact_links WHERE tenant_id=%s AND workflow_id=%s',(tenant,workflow)).fetchone()[0]==len(prepared.inventory)
            assert not list(proof_scratch.iterdir())
            proof_scratch.rmdir()
            for index,value in [(0,uuid4()),(1,uuid4()),(2,2),(3,2),(4,uuid4())]:
                invalid=list(lookup_args);invalid[index]=value
                assert parameters(tuple(invalid)) is None
                assert db.execute(policy_lookup,tuple(invalid)).fetchone()==(None,)
            db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(params_id,))
            assert parameters(lookup_args) is None
            assert db.execute(policy_lookup,lookup_args).fetchone()==(None,)
        db.execute("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s", (config['v25_composition']['auth_session_id'],))
        assert admitted() is False


def test_pinned_profile_preserves_resource_checks(database):
    from psycopg.types.json import Jsonb
    from test_v25_layer_params_shape import params
    from photo_worker.operations.layers.resources import layer_resources
    body=params()
    profile={'backend':'cpu','renderer_version':'v2.2',
             'resource_estimate':layer_resources(5,5,mask_refs=1,mask_nodes=1),
             'layer_runtime_sha256':'a'*64}
    with psycopg.connect(**database,autocommit=True) as db:
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            def valid(value):
                return db.execute('SELECT photo.v25_layer_profile_shape(%s,5,5,%s)',
                                  (Jsonb(body),Jsonb(value))).fetchone()[0]
            assert valid(profile) is True
            assert valid({**profile,'layer_alpha_mode':'require_float32'}) is True
            assert valid({**profile,'layer_alpha_mode':'normalize_uint16'}) is False
            normalized={**profile,'layer_alpha_mode':'normalize_uint16',
                        'resource_estimate':layer_resources(5,5,mask_refs=1,mask_nodes=1,alpha_mode='normalize_uint16')}
            assert valid(normalized) is True
            for field in ('ram_bytes','scratch_bytes'):
                insufficient={**normalized,'resource_estimate':{**normalized['resource_estimate'],field:normalized['resource_estimate'][field]-1}}
                assert valid(insufficient) is False
            assert valid({**profile,'layer_alpha_mode':'automatic'}) is False
            assert valid({**profile,'layer_runtime_sha256':'A'*64}) is False
            invalid={**profile,'resource_estimate':{**profile['resource_estimate'],'ram_bytes':1}}
            assert valid(invalid) is False


def test_layer_adoption_parameter_read_is_scheduler_only_and_generation_bound(database):
    signature='photo.v25_layer_adoption_params(uuid,uuid,integer,bigint,uuid)'
    with psycopg.connect(**database,autocommit=True) as db:
        for role,expected in [('photo_scheduler',True),('photo_worker',False),('photo_api',False)]:
            assert db.execute('SELECT has_function_privilege(%s,%s,%s)',(role,signature,'EXECUTE')).fetchone()==(expected,)
        policy='photo.v25_layer_adoption_alpha_policy(uuid,uuid,integer,bigint,uuid)'
        for role,expected in [('photo_scheduler',True),('photo_worker',False),('photo_api',False)]:
            assert db.execute('SELECT has_function_privilege(%s,%s,%s)',(role,policy,'EXECUTE')).fetchone()==(expected,)
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            assert db.execute('SELECT photo.v25_layer_adoption_alpha_policy(%s,%s,%s,%s,%s)',
                              (uuid4(),uuid4(),1,1,uuid4())).fetchone()==(None,)
            assert db.execute('SELECT * FROM photo.v25_layer_adoption_params(%s,%s,%s,%s,%s)',
                              (uuid4(),uuid4(),1,1,uuid4())).fetchone() is None


def test_composition_adoption_core_cannot_bypass_authority(database):
    signature='photo.adopt_v2_plan_pre_layer_authority(uuid,uuid,integer,bigint,uuid,jsonb,text)'
    with psycopg.connect(**database,autocommit=True) as db:
        for role in ('photo_scheduler','photo_api','photo_worker'):
            assert db.execute('SELECT has_function_privilege(%s,%s,%s)',(role,signature,'EXECUTE')).fetchone()==(False,)
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            assert db.execute("SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,'[]','{}')",
                              (uuid4(),uuid4(),uuid4())).fetchone()==(False,)


def test_layer_catalog_is_registered_without_activation(database):
    with psycopg.connect(**database,autocommit=True) as db:
        assert db.execute("SELECT resource_class,routing_key,v2_enabled FROM photo.operation_registry WHERE operation='layer.compose' AND version='1'").fetchone()==('cpu','cpu.layer_compose',False)
        assert db.execute("SELECT count(*) FROM photo.operation_output_policies WHERE operation='layer.compose' AND version='1'").fetchone()[0]>0
