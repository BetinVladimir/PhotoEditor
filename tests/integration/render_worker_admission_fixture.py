"""Synthetic rolled-back worker provisioning; no runtime qualification claim."""
from uuid import uuid4
import pytest
import json
from copy import deepcopy
from hashlib import sha256
from psycopg.types.json import Jsonb
from contextlib import contextmanager
from types import SimpleNamespace
from photo_worker.runtime.database import TaskDatabase
from photo_worker.contracts.base import canonical_bytes, canonical_hash


def assert_render_worker_admission(db,tenant,run,task,params,sha,raw,digest):
    worker,node=uuid4(),'render-admission-'+str(uuid4())
    with db.transaction(force_rollback=True):
        db.execute("INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes) VALUES(%s,4000000000,12000000000,2000000000)",(node,))
        db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions) VALUES(%s,%s,'[\"gpu\"]','[\"cuda\"]',%s,now(),'{}',session_user,%s,true,'{\"render.compose\":[\"cuda\"]}','[\"v2.2\"]')",(tenant,worker,'d'*64,node))
        db.execute("UPDATE photo.runs SET status='running',graph_sealed=true WHERE id=%s",(run,))
        db.execute("UPDATE photo.tasks SET state='running',attempt=1,fencing_token=1,worker_id=%s,lease_until=clock_timestamp()+interval '60 seconds',deadline=clock_timestamp()+interval '120 seconds' WHERE id=%s",(worker,task))
        db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity,tenant_id,task_id,fencing_token,lease_until) VALUES(%s,'gpu1','gpu','{}',%s,%s,1,clock_timestamp()+interval '60 seconds')",(node,tenant,task))
        args=(tenant,run,task,1,1,worker,params,sha,raw,digest)
        def admitted(values=args):
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_worker')
                try:return db.execute('SELECT photo.admit_v26_render_recipe('+','.join(['%s']*10)+')',values).fetchone()[0]
                finally:db.execute('RESET ROLE')
        assert admitted() is False  # A lease/registration is not qualification.
        policy,report=provision_controlled_render_qualification(db,tenant,run)
        assert admitted() is True
        from render_adoption_inputs_fixture import assert_render_adoption_inputs
        assert_render_adoption_inputs(db,tenant,run,task,worker,policy,params,sha)
        def adoption_inputs_ready():
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_fn_owner')
                try:return db.execute('SELECT photo.v26_render_adoption_inputs_ready(%s,%s)',(tenant,task)).fetchone()[0]
                finally:db.execute('RESET ROLE')
        assert adoption_inputs_ready() is True
        with db.transaction(force_rollback=True):
            db.execute("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=(SELECT (config_snapshot->'v26_render'->>'auth_session_id')::uuid FROM photo.runs WHERE id=%s)",(run,))
            assert adoption_inputs_ready() is False
        assert adoption_inputs_ready() is True
        # Boundary reachability only: a deferred, rolled-back result identity
        # and deliberately invalid inventory are not an accepted Render output.
        with db.transaction(force_rollback=True):
            result=uuid4()
            db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s,lease_until=NULL,worker_id=NULL WHERE id=%s",(result,task))
            def adopt_invalid_inventory():
                with db.transaction():
                    db.execute('SET LOCAL ROLE photo_scheduler')
                    value=db.execute("SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,'[]'::jsonb,%s)",(tenant,task,result,'a'*64)).fetchone()[0]
                    db.execute('RESET ROLE')
                    return value
            def adoption_counts():
                return tuple(db.execute('SELECT count(*) FROM photo.'+table+' WHERE tenant_id=%s',(tenant,)).fetchone()[0]
                    for table in ('v2_artifact_links','inbox','run_events','outbox'))
            baseline=adoption_counts()
            with pytest.raises(Exception) as invalid:
                adopt_invalid_inventory()
            assert getattr(invalid.value,'sqlstate',None)=='22023'
            assert adoption_counts()==baseline
            for statement,mutation_args in (
                ("UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s",(policy,)),
                ("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=(SELECT (config_snapshot->'v26_render'->>'auth_session_id')::uuid FROM photo.runs WHERE id=%s)",(run,)),
            ):
                with db.transaction(force_rollback=True):
                    db.execute(statement,mutation_args)
                    assert adopt_invalid_inventory() is False
                    assert adoption_counts()==baseline
        for statement,mutation_args in (
            ("UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s",(policy,)),
            ("UPDATE photo.v2_qualification_policies SET license_cleared=false,admission_enabled=false WHERE id=%s",(policy,)),
            ("UPDATE photo.operation_registry SET v2_enabled=false WHERE operation='render.compose' AND version='1'",()),
            ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(report,)),
            ("UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s",(report,)),
        ):
            with db.transaction(force_rollback=True):
                db.execute(statement,mutation_args)
                assert admitted() is False
            assert admitted() is True
        # Reuse the fixture transaction so the real adapter sees uncommitted
        # provisioning, while preserving its _query and SQL parameter path.
        @contextmanager
        def fixture_connection():
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_worker')
                db.execute("SELECT set_config('app.tenant_id','',true)")
                try:yield db
                finally:db.execute('RESET ROLE')
        transport=TaskDatabase('unused');transport._connect=fixture_connection
        command=SimpleNamespace(tenant_id=tenant,run_id=run,task_id=task,attempt=1,
            params=SimpleNamespace(artifact_id=params,sha256=sha))
        lease=SimpleNamespace(command=command,fence=1,worker_id=worker,input_digest=digest)
        assert transport.admit_render_recipe(lease,raw.encode()) is True
        expected_pin=db.execute('SELECT v26_qualification_pin FROM photo.runs WHERE id=%s',(run,)).fetchone()[0]
        assert transport.render_qualification(lease,raw.encode())==expected_pin
        cache_context=transport.render_cache_context(lease,raw.encode())
        assert cache_context[:2]==(params,sha) and cache_context[4]==expected_pin
        assert cache_context[2]==db.execute('SELECT execution_profile FROM photo.tasks WHERE id=%s',(task,)).fetchone()[0]
        expected_parents=db.execute("SELECT artifact_id,sha256 FROM photo.v2_task_inputs WHERE task_id=%s AND contract='ImageResult' ORDER BY ordinal,artifact_id",(task,)).fetchall()
        assert cache_context[3]==[{'artifact_id':str(identifier),'sha256':checksum} for identifier,checksum in expected_parents]
        assert transport.lookup_render_cache(lease,raw.encode(),'0'*64,'{}') is None
        wrong_generation=SimpleNamespace(command=command,fence=2,worker_id=worker,input_digest=digest)
        assert transport.render_qualification(wrong_generation,raw.encode()) is None
        assert transport.render_cache_context(wrong_generation,raw.encode()) is None
        assert transport.lookup_render_cache(wrong_generation,raw.encode(),'0'*64,'{}') is None
        assert transport.render_cache_artifacts(lease,raw.encode(),'0'*64,'{}')==[]
        assert transport.render_cache_artifacts(wrong_generation,raw.encode(),'0'*64,'{}')==[]
        with db.transaction(force_rollback=True):
            db.execute("UPDATE photo.worker_registrations SET certificate_sha256=%s WHERE worker_id=%s",('a'*64,worker))
            db.execute('SET LOCAL ROLE photo_gateway')
            assert db.execute('SELECT * FROM photo.authorize_gateway_task(%s,1,1,%s,%s)',(task,worker,'a'*64)).fetchone() is not None
            assert db.execute('SELECT * FROM photo.gateway_input(%s,1,1,%s,%s,%s)',(task,worker,'a'*64,uuid4())).fetchone() is None
            db.execute('RESET ROLE')
        required_contracts=['StyleV2']
        if json.loads(raw)['recipe'].get('layer_stack') is not None:required_contracts.append('LayerStack')
        if json.loads(raw)['recipe'].get('protected_mask') is not None:required_contracts.extend(['MaskRef','NpyArray'])
        for contract in required_contracts:
            with db.transaction(force_rollback=True):
                saved=db.execute("DELETE FROM photo.v2_task_inputs WHERE task_id=%s AND contract=%s RETURNING tenant_id,project_id,task_id,artifact_id,sha256,ordinal",(task,contract)).fetchone()
                db.execute("INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,ordinal,contract) VALUES(%s,%s,%s,%s,%s,%s,'CorrectionPlan')",saved)
                assert transport.admit_render_recipe(lease,raw.encode()) is False
            assert transport.admit_render_recipe(lease,raw.encode()) is True
        # Dedicated predicate test for correction/override metadata scope;
        # the immutable running recipe and full closure are not rewritten.
        with db.transaction(force_rollback=True):
            project,actor,session=db.execute("SELECT w.project_id,r.created_by,(r.config_snapshot->'v26_render'->>'auth_session_id')::uuid FROM photo.runs r JOIN photo.v2_workflows w ON w.run_id=r.id AND w.tenant_id=r.tenant_id WHERE r.id=%s",(run,)).fetchone()
            evidence=json.loads(raw);version=evidence['scalars']['source']['asset_version_id']
            refs=[]
            next_ordinal=db.execute('SELECT coalesce(max(ordinal)+1,0) FROM photo.v2_task_inputs WHERE task_id=%s AND ordinal<1000000',(task,)).fetchone()[0]
            for ordinal,kind in enumerate(('CorrectionPlan','Override','LayerStack'),next_ordinal):
                document=uuid4();payload=json.dumps(dict(schema_version=1,source=evidence['scalars']['source'],layers=[]),sort_keys=True,separators=(',',':')) if kind=='LayerStack' else '{}'
                pin=sha256(payload.encode()).hexdigest()
                db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
                db.execute('SET LOCAL ROLE photo_api')
                upload_args=(tenant,version,document,actor,session,pin,len(payload),'test')
                ticket=db.execute('SELECT * FROM photo.reserve_v2_draft_upload('+','.join(['%s']*8)+')',upload_args).fetchone()
                assert db.execute('SELECT photo.complete_v2_draft_upload('+','.join(['%s']*11)+')',(*upload_args[:5],ticket[1],ticket[2],pin,len(payload),'test','v1')).fetchone()==(True,)
                db.execute('RESET ROLE')
                ref=dict(artifact_id=str(ticket[1]),sha256=pin);refs.append(ref)
                db.execute('INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,ordinal,contract) VALUES(%s,%s,%s,%s,%s,%s,%s)',(tenant,project,task,ticket[1],pin,ordinal,kind))
            evidence['recipe'].update(session_corrections=refs[0],overrides=[refs[1]])
            layer_body=deepcopy(evidence);layer_body['recipe']['layer_stack']=refs[2]
            layer_query='SELECT photo.v26_render_layer_stack_scope(%s,%s,%s,%s,%s,%s,%s)'
            def layer_ready(layer_raw=payload):return db.execute(layer_query,(tenant,project,actor,session,Jsonb(layer_body),Jsonb(dict(layer_jcs=layer_raw)),task)).fetchone()[0]
            assert layer_ready() is True
            assert layer_ready(payload+' ') is False
            assert layer_ready('{}') is False

            for invalid in (dict(schema_version='1',source=evidence['scalars']['source'],layers=[]),dict(schema_version=1,source=evidence['scalars']['source'],layers=[],extra=True),dict(schema_version=1,source=evidence['scalars']['source'],layers={})):
                malformed=json.dumps(invalid,sort_keys=True,separators=(',',':'));bad_pin=sha256(malformed.encode()).hexdigest();bad_document=uuid4()
                db.execute('SET LOCAL ROLE photo_api')
                upload_args=(tenant,version,bad_document,actor,session,bad_pin,len(malformed),'test')
                bad_ticket=db.execute('SELECT * FROM photo.reserve_v2_draft_upload('+','.join(['%s']*8)+')',upload_args).fetchone()
                assert db.execute('SELECT photo.complete_v2_draft_upload('+','.join(['%s']*11)+')',(*upload_args[:5],bad_ticket[1],bad_ticket[2],bad_pin,len(malformed),'test','v1')).fetchone()==(True,)
                db.execute('RESET ROLE')
                bad_body=deepcopy(layer_body);bad_body['recipe']['layer_stack']=dict(artifact_id=str(bad_ticket[1]),sha256=bad_pin)
                assert db.execute(layer_query,(tenant,project,actor,session,Jsonb(bad_body),Jsonb(dict(layer_jcs=malformed)),None)).fetchone()==(False,)
            query='SELECT photo.v26_render_direct_inputs_ready(%s,%s,%s,%s,%s,%s)'
            def direct(value=evidence):return db.execute(query,(tenant,task,project,actor,session,Jsonb(value))).fetchone()[0]
            assert direct() is True
            boundary=deepcopy(evidence);boundary['recipe']['overrides']=[refs[1]]*128
            assert direct(boundary) is True
            boundary['recipe']['overrides'].append(refs[1])
            assert direct(boundary) is False
            for field in ('session_corrections','overrides'):
                changed=deepcopy(evidence)
                target=changed['recipe'][field] if field=='session_corrections' else changed['recipe'][field][0]
                target['sha256']='0'*64
                assert direct(changed) is False
            changed=deepcopy(evidence);changed['recipe']['overrides']={}
            assert direct(changed) is False
            db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(session,))
            assert direct() is False
        command.attempt=2
        assert transport.admit_render_recipe(lease,raw.encode()) is False
        command.attempt=1
        lease.fence=2
        assert transport.admit_render_recipe(lease,raw.encode()) is False
        lease.fence=1

        for index,wrong in ((0,uuid4()),(1,uuid4()),(2,uuid4()),(3,2),(4,2),(5,uuid4()),(6,uuid4()),(7,'0'*64),(8,'{}'),(9,'0'*64)):
            changed=list(args);changed[index]=wrong
            assert admitted(tuple(changed)) is False,index
        db.execute('UPDATE photo.resource_slots SET fencing_token=2 WHERE task_id=%s',(task,))
        assert admitted() is False
        assert transport.admit_render_recipe(lease,raw.encode()) is False
        db.execute('UPDATE photo.resource_slots SET fencing_token=1 WHERE task_id=%s',(task,))
        db.execute("UPDATE photo.tasks SET lease_until=clock_timestamp()-interval '1 second' WHERE id=%s",(task,))
        assert admitted() is False
        assert transport.admit_render_recipe(lease,raw.encode()) is False
        db.execute("UPDATE photo.tasks SET lease_until=clock_timestamp()+interval '60 seconds' WHERE id=%s",(task,))
        db.execute("UPDATE photo.resource_slots SET lease_until=clock_timestamp()-interval '1 second' WHERE task_id=%s",(task,))
        assert admitted() is False
        assert transport.admit_render_recipe(lease,raw.encode()) is False
        db.execute("UPDATE photo.resource_slots SET lease_until=clock_timestamp()+interval '60 seconds' WHERE task_id=%s",(task,))
        db.execute("UPDATE photo.worker_registrations SET db_principal='unrelated_worker' WHERE worker_id=%s",(worker,))
        assert admitted() is False
        assert transport.admit_render_recipe(lease,raw.encode()) is False
        db.execute('UPDATE photo.worker_registrations SET db_principal=session_user WHERE worker_id=%s',(worker,))
        db.execute("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=(SELECT (config_snapshot->'v26_render'->>'auth_session_id')::uuid FROM photo.runs WHERE id=%s)",(run,))
        assert admitted() is False
        assert transport.admit_render_recipe(lease,raw.encode()) is False


def provision_controlled_render_qualification(db,tenant,run):
    """Rollback-only metadata report and identity pin, never a GPU benchmark."""
    project,actor,model,runtime,backend=db.execute("SELECT w.project_id,r.created_by,r.config_snapshot->'v26_render'->'nodes'->0->'profile'->>'model_lock_sha',r.config_snapshot->'v26_render'->'nodes'->0->'profile'->>'render_runtime_sha',r.config_snapshot->'v26_render'->'nodes'->0->'profile'->>'backend' FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id WHERE r.tenant_id=%s AND r.id=%s",(tenant,run)).fetchone()
    device,dataset,fixture,proof='c'*64,'d'*64,'e'*64,'f'*64
    db.execute('INSERT INTO photo.v2_qualification_operators(operator_id,enabled) VALUES(%s,true)',(actor,))
    policy=db.execute("INSERT INTO photo.v2_qualification_policies(operation,operation_version,model_lock_sha,runtime_lock_sha,backend,device_profile_sha,dataset_sha,issuer_id,required_checks,test_fixture_shas,evidence_shas,license_cleared,enabled,admission_enabled) VALUES('render.compose','1',%s,%s,%s,%s,%s,%s,ARRAY['quality'],ARRAY[%s],ARRAY[%s],true,true,true) RETURNING id",(model,runtime,backend,device,dataset,actor,fixture,proof)).fetchone()[0]
    body=dict(schema_version=1,operation='render.compose',version='1',model_lock_sha=model,runtime_lock_sha=runtime,device_profile_sha=device,dataset_sha=dataset,input_fixture_shas=[fixture],checks=[dict(id='quality',passed=True,evidence_sha=proof)],metrics=dict(p50_ms=10,p95_ms=20,peak_vram_bytes=0,peak_rss_bytes=0,scratch_bytes=0),status='passed',issuer_id=str(actor),created_at='2026-09-01T00:00:00Z')
    raw=canonical_bytes(body);digest=canonical_hash(body)
    report=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at) VALUES(%s,'qualification-metadata-fixture',%s,'fixture-v1',%s,%s,'application/json','ready',clock_timestamp()) RETURNING id",(tenant,str(uuid4()),digest,len(raw))).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_maintenance')
    db.execute('SELECT photo.import_v2_qualification(%s,%s,%s,%s,%s,%s,%s)',(tenant,project,report,digest,actor,backend,raw.decode())).fetchone()
    db.execute('RESET ROLE')
    db.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='render.compose' AND version='1'")
    pin=dict(model_lock_sha=model,runtime_lock_sha=runtime,backend=backend,device_profile_sha=device,dataset_sha=dataset,calibration_sha='e'*64)
    db.execute('SET LOCAL ROLE photo_fn_owner')
    db.execute('UPDATE photo.runs SET v26_qualification_pin=%s WHERE tenant_id=%s AND id=%s',(Jsonb(pin),tenant,run))
    db.execute('RESET ROLE')
    return policy,report
