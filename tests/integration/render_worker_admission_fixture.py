"""Synthetic rolled-back worker provisioning; no runtime qualification claim."""
from uuid import uuid4
import json
from copy import deepcopy
from hashlib import sha256
from psycopg.types.json import Jsonb
from contextlib import contextmanager
from types import SimpleNamespace
from photo_worker.runtime.database import TaskDatabase


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
