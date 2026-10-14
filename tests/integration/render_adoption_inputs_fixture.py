"""Rolled-back SQL metadata provisioning; no image result/byte acceptance claim."""
from hashlib import sha256
from types import SimpleNamespace
from uuid import UUID, uuid4
from psycopg.types.json import Jsonb
from photo_worker.v2.render_adoption_inputs import load_render_adoption_inputs


def assert_render_adoption_inputs(db, tenant, run, task, worker, policy, params, digest):
    with db.transaction(force_rollback=True):
        artifact,result=uuid4(),uuid4();body=b'{}';checksum=sha256(body).hexdigest()
        bucket='render-adoption-metadata-fixture';key=str(artifact);version='fixture-version-1'
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,2,'application/json',%s,clock_timestamp(),'document',%s,1,1)",
            (artifact,tenant,bucket,key,checksum,version,task))
        db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES(%s,%s,1,1,%s,%s,%s,%s,'document',%s,%s,%s,2,'application/json','ready',%s)",
            (tenant,task,worker,'a'*64,uuid4(),artifact,bucket,key,checksum,version))
        db.execute("INSERT INTO photo.task_results(id,tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,%s,1,1,%s,'{}',%s)",
            (result,tenant,task,Jsonb([{'artifact_id':str(artifact),'sha256':checksum}]),'a'*64))
        db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s,lease_until=NULL,worker_id=NULL WHERE id=%s",(result,task))
        arguments=(tenant,task,1,1,result)
        def read(values=arguments):
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_scheduler')
                row=db.execute('SELECT * FROM photo.v26_render_adoption_inputs(%s,%s,%s,%s,%s)',values).fetchone()
                db.execute('RESET ROLE');return row
        row=read()
        assert row is not None and row[:2]==(params,digest)
        assert row[2]==db.execute('SELECT execution_profile FROM photo.tasks WHERE id=%s',(task,)).fetchone()[0]
        expected=db.execute("SELECT artifact_id,sha256 FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s AND contract='ImageResult' ORDER BY ordinal,artifact_id",(tenant,task)).fetchall()
        assert row[3]==[{'artifact_id':str(identifier),'sha256':checksum} for identifier,checksum in expected]
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            pin=db.execute('SELECT photo.v26_render_adoption_qualification(%s,%s,1,1,%s)',(tenant,task,result)).fetchone()[0]
            db.execute('RESET ROLE')
        assert pin==db.execute('SELECT v26_qualification_pin FROM photo.runs WHERE id=%s',(run,)).fetchone()[0]
        context=SimpleNamespace(operation='render.compose',version='1',attempt=1,fence=1,result_id=result)
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            decoded=load_render_adoption_inputs(db,tenant,task,context)
            db.execute('RESET ROLE')
        assert decoded.recipe.artifact_id==params and decoded.recipe.sha256==digest
        assert decoded.execution_profile()==row[2]
        from photo_worker.v2.render_qualification_pin import load_render_adoption_qualification
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            decoded_pin=load_render_adoption_qualification(db,tenant,task,context)
            db.execute('RESET ROLE')
        assert decoded_pin.device_profile_sha==pin['device_profile_sha']
        assert decoded_pin.calibration_sha==pin['calibration_sha']
        # SQL metadata acceptance only; typed image and object-store proofs are separate.
        from photo_worker.contracts.base import canonical_bytes, canonical_hash
        from photo_worker.v2.render_cache_identity import render_cache_context
        from photo_worker.v2.render_qualification_pin import RenderQualificationPin
        inventory=[{'artifact_id':str(artifact),'sha256':checksum,
                    'purpose':sha256(('tasks/'+str(task)+'/$').encode()).hexdigest(),'ordinal':0}]
        cache_body=canonical_bytes(render_cache_context(UUID(str(tenant)),decoded.recipe,decoded.execution_profile(),decoded.parent_roots,RenderQualificationPin.from_value(pin))).decode()
        cache_key=sha256(cache_body.encode()).hexdigest()
        publish_args=(*arguments,cache_key,cache_body,Jsonb(inventory))
        def publish(values=publish_args):
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_scheduler')
                try:return db.execute('SELECT photo.publish_v26_render_cache('+','.join(['%s']*8)+')',values).fetchone()[0]
                finally:db.execute('RESET ROLE')
        assert publish() is False
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            assert db.execute('SELECT photo.adopt_v2_plan(%s,%s,%s,%s,%s,%s,%s)',(*arguments,Jsonb(inventory),canonical_hash(inventory))).fetchone()==(True,)
            db.execute('RESET ROLE')
        with db.transaction(force_rollback=True):
            db.execute('SET LOCAL ROLE photo_fn_owner')
            db.execute("INSERT INTO photo.result_cache(tenant_id,cache_key,result_id,valid_until,render_context,render_inventory,render_context_jcs) VALUES(%s,%s,%s,clock_timestamp()-interval '1 second',%s,%s,%s)",
                (tenant,cache_key,result,Jsonb(__import__('json').loads(cache_body)),Jsonb(inventory),cache_body))
            db.execute('RESET ROLE')
            import pytest
            with pytest.raises(Exception) as identity_change:
                with db.transaction():
                    db.execute('SET LOCAL ROLE photo_fn_owner')
                    db.execute("UPDATE photo.result_cache SET cache_key=%s WHERE tenant_id=%s AND cache_key=%s",('0'*64,tenant,cache_key))
            assert identity_change.value.sqlstate=='42501'
            with pytest.raises(Exception) as unbounded:
                with db.transaction():
                    db.execute('SET LOCAL ROLE photo_fn_owner')
                    db.execute("UPDATE photo.result_cache SET valid_until=clock_timestamp()+interval '48 hours' WHERE tenant_id=%s AND cache_key=%s",(tenant,cache_key))
            assert unbounded.value.sqlstate=='42501'
            assert publish() is True
            assert publish() is False
            assert db.execute('SELECT valid_until>clock_timestamp() FROM photo.result_cache WHERE tenant_id=%s AND cache_key=%s',(tenant,cache_key)).fetchone()==(True,)
        assert publish() is True
        assert publish() is False
        cached=db.execute('SELECT result_id,render_context,valid_until>clock_timestamp() FROM photo.result_cache WHERE tenant_id=%s AND cache_key=%s',(tenant,cache_key)).fetchone()
        assert cached==(result,__import__('json').loads(cache_body),True)
        for index,value in ((0,uuid4()),(1,uuid4()),(2,2),(3,2),(4,uuid4()),(5,'0'*64),(6,'{}'),(7,Jsonb([]))):
            altered=list(publish_args);altered[index]=value
            assert publish(tuple(altered)) is False
        lookup_args=(tenant,task,1,1,cache_key,cache_body)
        def lookup(values=lookup_args):
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_scheduler')
                try:return db.execute('SELECT * FROM photo.lookup_v26_render_cache('+','.join(['%s']*6)+')',values).fetchone()
                finally:db.execute('RESET ROLE')
        assert lookup()==(result,{'artifact_id':str(artifact),'sha256':checksum},inventory)
        from render_cache_cross_task_fixture import assert_cross_task_render_cache
        assert_cross_task_render_cache(db,tenant,run,task,worker,cache_key,cache_body,artifact,result,policy)
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            manifest=db.execute('SELECT * FROM photo.v26_render_cache_artifacts('+','.join(['%s']*6)+')',lookup_args).fetchall()
            db.execute('RESET ROLE')
        assert manifest==[(artifact,checksum,2,'application/json','document',bucket,key,version,0)]
        from dataclasses import replace
        from photo_worker.v2.render_cache_lookup import lookup_render_cache
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            candidate=lookup_render_cache(db,UUID(str(tenant)),task,1,1,
                replace(decoded,qualification_pin=RenderQualificationPin.from_value(pin)))
            db.execute('RESET ROLE')
        assert candidate.result_id==result and candidate.root.artifact_id==artifact
        assert candidate.inventory_jcs==canonical_bytes(inventory)
        for index,value in ((0,uuid4()),(1,uuid4()),(2,2),(3,2),(4,'0'*64),(5,'{}')):
            altered=list(lookup_args);altered[index]=value
            assert lookup(tuple(altered)) is None
        for statement,values in (
            ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(artifact,)),
            ("UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s",(policy,)),
            ("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=(SELECT (config_snapshot->'v26_render'->>'auth_session_id')::uuid FROM photo.runs WHERE id=%s)",(run,)),
            ("DELETE FROM photo.v2_artifact_links WHERE tenant_id=%s AND artifact_id=%s",(tenant,artifact)),
            ("UPDATE photo.tasks SET cancel_requested=true WHERE id=%s",(task,)),
        ):
            with db.transaction(force_rollback=True):
                db.execute(statement,values)
                assert lookup() is None
            assert lookup() is not None
        import pytest
        with pytest.raises(Exception) as immutable:
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_fn_owner')
                db.execute('UPDATE photo.result_cache SET render_context=NULL WHERE tenant_id=%s AND cache_key=%s',(tenant,cache_key))
        assert immutable.value.sqlstate=='42501'
        for index,value in ((0,uuid4()),(1,uuid4()),(2,2),(3,2),(4,uuid4())):
            changed=list(arguments);changed[index]=value
            assert read(tuple(changed)) is None
        for statement,values in (
            ("UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s",(policy,)),
            ("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=(SELECT (config_snapshot->'v26_render'->>'auth_session_id')::uuid FROM photo.runs WHERE id=%s)",(run,)),
            ("UPDATE photo.tasks SET cancel_requested=true WHERE id=%s",(task,)),
        ):
            with db.transaction(force_rollback=True):
                db.execute(statement,values)
                assert read() is None
            assert read() is not None
