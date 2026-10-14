"""Relational staging metadata; complete object-byte qualification is separate."""
import json
from pathlib import Path
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.v2.render_resources import render_resources
from hashlib import sha256
from uuid import uuid4, uuid5
import pytest
import psycopg
from psycopg.types.json import Jsonb
from layer_original_fixture import seed_original_composition


@pytest.mark.parametrize('valid_task',[True,False])
@pytest.mark.parametrize('valid_workflow',[True,False])
@pytest.mark.parametrize('valid_budget',[True,False])
@pytest.mark.parametrize('valid_quota',[True,False])
def test_render_task_stage_registered_root_style_snapshot_and_replay(db,valid_task,valid_budget,valid_quota,valid_workflow,empty_layer=False,user_protection=False,raster_protection=False):
    images=json.loads((Path(__file__).resolve().parents[2]/'backend/internal/review/testdata/working-images-python.json').read_text())
    source_body=images[0];source_raw=canonical_bytes(source_body)
    case=seed_original_composition(db,source_object=dict(bucket='test',key=str(uuid4()),version='v1',bytes=len(source_raw),sha256=sha256(source_raw).hexdigest()),canvas_hash=canonical_hash(source_body['canvas']))
    tid,project,actor,session=(case[k] for k in ('Tenant','Project','Actor','Session'))
    binding=json.loads(case['Params'])['base'];version=binding['asset_version_id']
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)',(tid,))
    style_ref=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES(%s,'test',%s,'v1',%s,64,'application/json','document',clock_timestamp()) RETURNING id",(tid,str(uuid4()),'e'*64)).fetchone()[0]
    style=db.execute("INSERT INTO photo.styles(tenant_id,project_id,name,created_by) VALUES(%s,%s,'Render',%s) RETURNING id",(tid,project,actor)).fetchone()[0]
    db.execute("INSERT INTO photo.style_versions(tenant_id,style_id,version_no,config_json,config_hash,schema_version,renderer_version,approval_status,metrics,export_artifact_id) VALUES(%s,%s,1,'{}',%s,'2.0.0','photo_renderer_v2','accepted','{}',%s)",(tid,style,'e'*64,style_ref))
    layer_raw='';layer_ref=None
    if empty_layer:
        layer_raw=json.dumps(dict(schema_version=1,source=binding,layers=[]),sort_keys=True,separators=(',',':'))
        layer_sha=sha256(layer_raw.encode()).hexdigest();layer_document=uuid4()
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(tid,));db.execute('SET LOCAL ROLE photo_api')
        layer_args=(tid,version,layer_document,actor,session,layer_sha,len(layer_raw),'test')
        layer_upload=db.execute('SELECT * FROM photo.reserve_v2_draft_upload('+','.join(['%s']*8)+')',layer_args).fetchone()
        assert db.execute('SELECT photo.complete_v2_draft_upload('+','.join(['%s']*11)+')',(*layer_args[:5],layer_upload[1],layer_upload[2],layer_sha,len(layer_raw),'test','v1')).fetchone()==(True,)
        db.execute('RESET ROLE')
        layer_ref=dict(artifact_id=str(layer_upload[1]),sha256=layer_sha)
    protection_raw='';protection_ref=None;protection_pixels=None
    if user_protection:
        from v26_protection_fixture import publish_user_protection
        protection_raw,protection_ref,protection_pixels=publish_user_protection(db,case,binding,source_body)
    if raster_protection:
        from v26_protection_fixture import publish_legacy_raster_protection
        protection_raw,protection_ref,protection_pixels=publish_legacy_raster_protection(db,case,binding,source_body)
    body=json.dumps(dict(schema_version=1,recipe=dict(source=binding,base_style=dict(artifact_id=str(style_ref),sha256='e'*64),layer_stack=layer_ref,protected_mask=protection_ref),recipe_sha256='c'*64,scalars=dict(source=binding,model_lock_sha='d'*64,icc_lock_sha='f'*64),learning=None),sort_keys=True,separators=(',',':'))
    digest=sha256(body.encode()).hexdigest();document=uuid4()
    db.execute("SELECT set_config('app.tenant_id',%s,true)",(tid,));db.execute('SET LOCAL ROLE photo_api')
    args=(tid,version,document,actor,session,digest,len(body),'test')
    upload=db.execute('SELECT * FROM photo.reserve_v2_draft_upload('+','.join(['%s']*8)+')',args).fetchone()
    assert db.execute('SELECT photo.complete_v2_draft_upload('+','.join(['%s']*11)+')',(*args[:5],upload[1],upload[2],digest,len(body),'test','v1')).fetchone()==(True,)
    assert db.execute('SELECT photo.register_v26_render_recipe('+','.join(['%s']*10)+')',(*args[:5],upload[1],digest,binding['source']['artifact_id'],binding['source']['sha256'],body)).fetchone()==(True,)
    db.execute('RESET ROLE')
    run_id=uuid4();workflow=uuid5(run_id,'workflow') if valid_workflow else uuid4();task=uuid5(workflow,'render.compose:0') if valid_task else uuid4();profile=dict(backend='cuda',renderer_version='v2.2',render_runtime_sha='a'*64,model_lock_sha='d'*64,icc_lock_sha='f'*64,tile_size=512,alpha_mode='require_float32',resource_estimate=render_resources(source_body['canvas']['width'],source_body['canvas']['height'],mask_refs=int(user_protection or raster_protection),mask_nodes=int(user_protection or raster_protection),provider_peak=dict(ram_bytes=0,scratch_bytes=0,vram_bytes=0,output_bytes=0)))
    if not valid_budget:profile['resource_estimate']['ram_bytes']=1
    node=dict(task_id=str(task),params=dict(artifact_id=str(upload[1]),sha256=digest),params_jcs=body,source_jcs=source_raw.decode(),input_digest='b'*64,profile=profile,layer_jcs=layer_raw,protection_jcs=protection_raw)
    nodes=[node]
    if not valid_quota:
        extra=dict(node);extra['task_id']=str(uuid5(workflow,'render.compose:1'))
        extra['profile']=dict(profile,resource_estimate=dict(profile['resource_estimate'],output_bytes=1073741824))
        nodes.append(extra)
    config=dict(project_id=project,input_snapshot_id=case['Snapshot'],v26_render=dict(auth_session_id=session,nodes=nodes))
    run=db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'v2','draft',%s,%s,%s,%s,'fail_fast') RETURNING id",(run_id,tid,case['Snapshot'],Jsonb(config),'a'*64,actor)).fetchone()[0]
    db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'preview','v2.2',%s)",(workflow,tid,project,run,case['Snapshot'],'a'*64))
    db.execute("INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at) VALUES(%s,'user',%s,'run',%s,1073741824,1,clock_timestamp()+interval '1 hour')",(tid,actor,run))
    db.execute('SET LOCAL ROLE photo_scheduler')
    query='SELECT photo.stage_v26_render_recipe_task('+','.join(['%s']*8)+')'
    stage=(tid,run,task,0,upload[1],digest,'b'*64,Jsonb(profile))
    if not valid_task or not valid_budget or not valid_quota or not valid_workflow:
        assert db.execute(query,stage).fetchone()==(False,)
        db.execute('RESET ROLE')
        assert db.execute('SELECT count(*) FROM photo.tasks WHERE id=%s',(task,)).fetchone()==(0,)
        return
    db.execute('RESET ROLE')
    db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','4294967296') WHERE id=%s",(tid,))
    with db.transaction(force_rollback=True):
        batch_id=uuid4();child_id=uuid5(batch_id,'render.batch:0');child_workflow=uuid5(child_id,'workflow');child_task=uuid5(child_workflow,'render.compose:0')
        child_config=dict(config,v26_render=dict(auth_session_id=session,nodes=[dict(node,task_id=str(child_task))]))
        child_raw=canonical_bytes(child_config).decode();child_sha=sha256(child_raw.encode()).hexdigest();child_output=profile['resource_estimate']['output_bytes']
        parent_body=dict(schema_version=1,project_id=project,input_snapshot_id=case['Snapshot'],auth_session_id=session,policy_sha='a'*64,parts=[dict(run_id=str(child_id),first_ordinal=0,frame_count=1,config_sha=child_sha,output_bytes=child_output)])
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.begin_v26_render_batch('+','.join(['%s']*7)+')',(tid,project,case['Snapshot'],actor,session,batch_id,canonical_bytes(parent_body).decode())).fetchone()==(True,)
        part_args=(tid,batch_id,actor,session,0)
        part_query='SELECT photo.begin_v26_render_batch_part('+','.join(['%s']*6)+')'
        assert db.execute(part_query,(*part_args,child_raw+' ')).fetchone()==(False,)
        assert db.execute(part_query,(*part_args,child_raw)).fetchone()==(True,)
        assert db.execute(part_query,(*part_args,child_raw)).fetchone()==(True,)
        saved_child=db.execute('SELECT config,config_sha,reserved_bytes FROM photo.v26_render_draft_state(%s,%s,%s,%s)',(tid,child_id,actor,session)).fetchone()
        assert saved_child==(child_config,child_sha,child_output)
        assert db.execute('SELECT run_id,config,config_sha,reserved_bytes FROM photo.v26_render_batch_part_state(%s,%s,%s,%s,0)',(tid,batch_id,actor,session)).fetchone()==(child_id,child_config,child_sha,child_output)
        assert db.execute('SELECT * FROM photo.v26_render_batch_part_state(%s,%s,%s,%s,1)',(tid,batch_id,actor,session)).fetchall()==[]
        for parent_phase in ('queued','running','waiting_review'):
            with db.transaction(force_rollback=True):
                db.execute('RESET ROLE')
                db.execute('UPDATE photo.runs SET status=%s WHERE id=%s',(parent_phase,batch_id))
                db.execute('SET LOCAL ROLE photo_scheduler')
                assert db.execute('SELECT run_id FROM photo.v26_render_batch_part_state(%s,%s,%s,%s,0)',(tid,batch_id,actor,session)).fetchone()==(child_id,)
        for parent_phase in ('completed','completed_with_errors','failed','cancelled','cancelling'):
            with db.transaction(force_rollback=True):
                db.execute('RESET ROLE')
                db.execute('UPDATE photo.runs SET status=%s WHERE id=%s',(parent_phase,batch_id))
                db.execute('SET LOCAL ROLE photo_scheduler')
                assert db.execute('SELECT * FROM photo.v26_render_batch_part_state(%s,%s,%s,%s,0)',(tid,batch_id,actor,session)).fetchall()==[]

        assert db.execute('SELECT * FROM photo.v26_render_batch_parts(%s,%s,%s,%s)',(tid,batch_id,actor,session)).fetchone()==(0,child_id,0,1,'draft',1,0,0,0)
        assert db.execute('SELECT * FROM photo.v26_render_batch_counts(%s,%s)',(tid,batch_id)).fetchone()==(1,0,0,0,0)
        assert db.execute('SELECT photo.v26_render_batch_stage(%s,%s)',(tid,batch_id)).fetchone()==('preparing',)
        for state,expected_stage in [('queued','queued'),('running','running'),('waiting_review','waiting_review'),('failed','finalizing')]:
            with db.transaction(force_rollback=True):
                db.execute('RESET ROLE')
                db.execute('UPDATE photo.runs SET status=%s,graph_sealed=true WHERE id=%s',(state,child_id))
                db.execute('SET LOCAL ROLE photo_api')
                assert db.execute('SELECT photo.v26_render_batch_stage(%s,%s)',(tid,batch_id)).fetchone()==(expected_stage,)
                db.execute('RESET ROLE')
                db.execute('SET LOCAL ROLE photo_scheduler')
                assert db.execute('SELECT photo.sync_v26_render_batch_state()').fetchone()==(True,)
                assert db.execute('SELECT photo.sync_v26_render_batch_state()').fetchone()==(False,)
                db.execute('RESET ROLE')
                durable='running' if state=='failed' else state
                assert db.execute('SELECT status,revision,graph_sealed FROM photo.runs WHERE id=%s',(batch_id,)).fetchone()==(durable,2,False)
                assert db.execute('SELECT count(*) FROM photo.run_events WHERE run_id=%s AND type=%s',(batch_id,'run.'+durable)).fetchone()==(1,)


        with db.transaction(force_rollback=True):
            db.execute('RESET ROLE')
            db.execute("UPDATE photo.tasks SET state='skipped',terminal_reason='disabled' WHERE id=%s",(child_task,))
            db.execute('SET LOCAL ROLE photo_api')
            assert db.execute('SELECT * FROM photo.v26_render_batch_counts(%s,%s)',(tid,batch_id)).fetchone()==(1,0,0,1,0)

        assert db.execute('SELECT * FROM photo.v26_render_batch_counts(%s,%s)',(uuid4(),batch_id)).fetchall()==[]
        assert db.execute(part_query,(tid,batch_id,actor,uuid4(),0,child_raw)).fetchone()==(False,)

        assert db.execute('SELECT photo.finalize_v26_render_batch()').fetchone()==(False,)
        with db.transaction(force_rollback=True):
            db.execute('RESET ROLE')
            db.execute("UPDATE photo.tasks SET state='failed',terminal_reason='fixture_failure' WHERE id=%s",(child_task,))
            db.execute("UPDATE photo.runs SET status='failed',graph_sealed=true WHERE id=%s",(child_id,))
            db.execute('SET LOCAL ROLE photo_scheduler')
            assert db.execute('SELECT photo.finalize_v26_render_batch()').fetchone()==(True,)
            assert db.execute('SELECT photo.finalize_v26_render_batch()').fetchone()==(False,)
            db.execute('RESET ROLE')
            assert db.execute('SELECT status,graph_sealed,revision FROM photo.runs WHERE id=%s',(batch_id,)).fetchone()==('failed',True,2)
            assert db.execute("SELECT count(*) FROM photo.run_events WHERE run_id=%s AND type='run.failed'",(batch_id,)).fetchone()==(1,)
            with pytest.raises(psycopg.errors.CheckViolation,match='V26_BATCH_ALREADY_TERMINAL'),db.transaction():
                db.execute("UPDATE photo.runs SET status='draft' WHERE id=%s",(child_id,))
        with db.transaction(force_rollback=True):
            db.execute('RESET ROLE')
            db.execute("UPDATE photo.runs SET status='completed',graph_sealed=true WHERE id=%s",(child_id,))
            db.execute('SET LOCAL ROLE photo_scheduler')
            # A terminal child cannot hide an unfinished planned task.
            assert db.execute('SELECT photo.finalize_v26_render_batch()').fetchone()==(False,)
            db.execute('RESET ROLE')
            assert db.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(batch_id,)).fetchone()==('draft',1)
        with db.transaction(force_rollback=True):
            db.execute('RESET ROLE')
            db.execute("UPDATE photo.tasks SET state='skipped',terminal_reason='disabled' WHERE id=%s",(child_task,))
            db.execute("UPDATE photo.runs SET status='completed',graph_sealed=true WHERE id=%s",(child_id,))
            db.execute('SET LOCAL ROLE photo_scheduler')
            assert db.execute('SELECT photo.finalize_v26_render_batch()').fetchone()==(True,)
            db.execute('RESET ROLE')
            assert db.execute('SELECT status,graph_sealed,revision FROM photo.runs WHERE id=%s',(batch_id,)).fetchone()==('completed',True,2)
            assert db.execute("SELECT count(*) FROM photo.run_events WHERE run_id=%s AND type='run.completed'",(batch_id,)).fetchone()==(1,)
        db.execute('SET LOCAL ROLE photo_scheduler')
        with db.transaction(force_rollback=True):
            # An older terminal child with a missing planned task must not starve a valid batch.
            next_batch=uuid4();next_child=uuid5(next_batch,'render.batch:0')
            next_workflow=uuid5(next_child,'workflow');next_task=uuid5(next_workflow,'render.compose:0')
            next_config=dict(config,v26_render=dict(auth_session_id=session,nodes=[dict(node,task_id=str(next_task))]))
            next_raw=canonical_bytes(next_config).decode();next_sha=sha256(next_raw.encode()).hexdigest()
            next_parent=dict(parent_body,parts=[dict(run_id=str(next_child),first_ordinal=0,frame_count=1,config_sha=next_sha,output_bytes=child_output)])
            db.execute('SET LOCAL ROLE photo_scheduler')
            assert db.execute('SELECT photo.begin_v26_render_batch('+','.join(['%s']*7)+')',(tid,project,case['Snapshot'],actor,session,next_batch,canonical_bytes(next_parent).decode())).fetchone()==(True,)
            assert db.execute(part_query,(tid,next_batch,actor,session,0,next_raw)).fetchone()==(True,)
            db.execute('RESET ROLE')
            db.execute("UPDATE photo.runs SET created_at=clock_timestamp()-interval '2 days' WHERE id=%s",(batch_id,))
            db.execute('DELETE FROM photo.tasks WHERE id=%s',(child_task,))
            db.execute("UPDATE photo.tasks SET state='failed',terminal_reason='fixture_failure' WHERE id=%s",(next_task,))
            db.execute("UPDATE photo.runs SET status='failed',graph_sealed=true WHERE id IN(%s,%s)",(child_id,next_child))
            db.execute('SET LOCAL ROLE photo_scheduler')
            assert db.execute('SELECT photo.finalize_v26_render_batch()').fetchone()==(True,)
            assert db.execute('SELECT photo.finalize_v26_render_batch()').fetchone()==(False,)
            db.execute('RESET ROLE')
            assert db.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(batch_id,)).fetchone()==('draft',1)
            assert db.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(next_batch,)).fetchone()==('failed',2)
        db.execute('SET LOCAL ROLE photo_scheduler')
        cancel_query='SELECT photo.cancel_v26_render_batch(%s,%s,%s,%s)'
        assert db.execute(cancel_query,(tid,batch_id,actor,uuid4())).fetchone()==(False,)
        with db.transaction(force_rollback=True):
            db.execute('RESET ROLE')
            db.execute('UPDATE photo.runs SET revision=9007199254740991 WHERE id=%s',(batch_id,))
            db.execute('SET LOCAL ROLE photo_scheduler')
            assert db.execute(cancel_query,(tid,batch_id,actor,session)).fetchone()==(False,)
            db.execute('RESET ROLE')
            assert db.execute('SELECT status,cancel_requested FROM photo.runs WHERE id=%s',(child_id,)).fetchone()==('draft',False)
        assert db.execute("SELECT has_function_privilege('photo_worker','photo.cancel_v26_render_batch(uuid,uuid,uuid,uuid)','EXECUTE')").fetchone()==(False,)

        db.execute('RESET ROLE')
        db.execute('SET LOCAL ROLE photo_api')
        assert db.execute(cancel_query,(tid,batch_id,actor,session)).fetchone()==(True,)
        db.execute('RESET ROLE')
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute(cancel_query,(tid,batch_id,actor,session)).fetchone()==(True,)
        assert db.execute(cancel_query,(tid,batch_id,actor,session)).fetchone()==(True,)
        assert db.execute(part_query,(*part_args,child_raw)).fetchone()==(False,)
        assert db.execute('SELECT * FROM photo.v26_render_batch_part_state(%s,%s,%s,%s,0)',(tid,batch_id,actor,session)).fetchall()==[]
        assert db.execute('SELECT photo.finalize_run_cancellation()').fetchone()==(0,)
        assert db.execute('SELECT photo.cancel_idle_task()').fetchone()==(1,)
        assert db.execute('SELECT photo.finalize_run_cancellation()').fetchone()==(1,)
        assert db.execute('SELECT photo.finalize_run_cancellation()').fetchone()==(1,)
        assert db.execute(cancel_query,(tid,batch_id,actor,session)).fetchone()==(True,)
        db.execute('RESET ROLE')
        assert db.execute('SELECT status,cancel_requested FROM photo.runs WHERE id IN(%s,%s) ORDER BY id',(batch_id,child_id)).fetchall()==[('cancelled',True),('cancelled',True)]
        assert db.execute('SELECT count(*) FROM photo.audit_log WHERE tenant_id=%s AND resource_id=%s AND action=%s',(tid,batch_id,'run.cancel_requested')).fetchone()==(1,)
        assert db.execute('SELECT state,fencing_token FROM photo.tasks WHERE id=%s',(child_task,)).fetchone()==('cancelled',1)
        db.execute('SET LOCAL ROLE photo_api')
        assert db.execute('SELECT * FROM photo.v26_render_batch_counts(%s,%s)',(tid,batch_id)).fetchone()==(1,0,1,0,0)
        db.execute('RESET ROLE')

        db.execute('RESET ROLE')
    db.execute('SET LOCAL ROLE photo_scheduler')
    for reject_second in (True,False):
        db.execute('RESET ROLE')
        with db.transaction(force_rollback=True):
            new_run=uuid4();new_workflow=uuid5(new_run,'workflow');new_task=uuid5(new_workflow,'render.compose:0')
            new_nodes=[dict(node,task_id=str(new_task))]
            new_nodes.append(dict(node,task_id=str(uuid4() if reject_second else uuid5(new_workflow,'render.compose:1'))))
            new_config=dict(config,v26_render=dict(auth_session_id=session,nodes=new_nodes))
            before=db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()
            db.execute('SET LOCAL ROLE photo_scheduler')
            begin='SELECT photo.begin_v26_render_draft('+','.join(['%s']*10)+')'
            begin_args=(tid,project,case['Snapshot'],actor,session,new_run,new_workflow,json.dumps(new_config,sort_keys=True,separators=(',',':'),default=str),'a'*64,1073741824)
            assert db.execute(begin,begin_args).fetchone()==(not reject_second,)
            if not reject_second:
                assert db.execute(begin,begin_args).fetchone()==(True,)
                state_query='SELECT * FROM photo.v26_render_draft_state(%s,%s,%s,%s)'
                state=db.execute(state_query,(tid,new_run,actor,session)).fetchone()
                assert state[0]==new_workflow and state[3]==new_config
                assert state[4:]==(sha256(begin_args[7].encode()).hexdigest(),'a'*64,1073741824)
                assert db.execute(state_query,(tid,new_run,uuid4(),session)).fetchall()==[]
                assert db.execute(state_query,(tid,new_run,actor,uuid4())).fetchall()==[]
            db.execute('RESET ROLE')
            assert db.execute('SELECT count(*) FROM photo.runs WHERE id=%s',(new_run,)).fetchone()==(0 if reject_second else 1,)
            assert db.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s',(new_run,)).fetchone()==(0 if reject_second else 2,)
            after=db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tid,)).fetchone()
            assert after==(before[0]+(0 if reject_second else 1073741824),)
        db.execute('SET LOCAL ROLE photo_scheduler')
    for snapshot_pin in (None,str(uuid4())):
        db.execute('RESET ROLE')
        with db.transaction(force_rollback=True):
            bad_run=uuid4();bad_workflow=uuid5(bad_run,'workflow');bad_task=uuid5(bad_workflow,'render.compose:0')
            bad_node=dict(node,task_id=str(bad_task))
            bad_config=dict(config,v26_render=dict(auth_session_id=session,nodes=[bad_node]))
            if snapshot_pin is None:bad_config.pop('input_snapshot_id')
            else:bad_config['input_snapshot_id']=snapshot_pin
            db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'v2','draft',%s,%s,%s,%s,'fail_fast')",(bad_run,tid,case['Snapshot'],Jsonb(bad_config),'a'*64,actor))
            db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'preview','v2.2',%s)",(bad_workflow,tid,project,bad_run,case['Snapshot'],'a'*64))
            db.execute("INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at) VALUES(%s,'user',%s,'run',%s,1073741824,1,clock_timestamp()+interval '1 hour')",(tid,actor,bad_run))
            db.execute('SET LOCAL ROLE photo_scheduler')
            bad_stage=list(stage);bad_stage[1]=bad_run;bad_stage[2]=bad_task
            assert db.execute(query,bad_stage).fetchone()==(False,)
            db.execute('RESET ROLE')
            assert db.execute('SELECT count(*) FROM photo.tasks WHERE id=%s',(bad_task,)).fetchone()==(0,)
        db.execute('SET LOCAL ROLE photo_scheduler')
    for field,value in (('tile_size',True),('tile_size',128),('alpha_mode','invalid'),('resource_estimate',dict(output_bytes=1024))):
        bad=dict(profile);bad[field]=value
        changed=list(stage);changed[-1]=Jsonb(bad)
        assert db.execute(query,changed).fetchone()==(False,)
    assert db.execute(query,stage).fetchone()==(True,)
    assert db.execute(query,stage).fetchone()==(True,)
    db.execute('RESET ROLE')
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT sealed,manifest,entries_sha,entry_count,chunks FROM photo.v26_render_input_state(%s,%s,%s)',(tid,run,task)).fetchone()==(False,None,None,None,[])
    db.execute('RESET ROLE')
    assert db.execute('SELECT photo.v26_render_activation_ready(%s,%s)',(tid,run)).fetchone()==(False,)
    assert db.execute("SELECT photo.v2_input_document_scope(%s,%s,%s,'v2_input_manifest')",(tid,run,task)).fetchone()==(True,)
    db.execute('SET LOCAL ROLE photo_scheduler')
    from psycopg.errors import CheckViolation
    with pytest.raises(CheckViolation,match='V26_RENDER_INPUT_ROOT_INVALID'):
        with db.transaction():
            db.execute('SELECT photo.seal_v2_task_inputs(%s,%s,%s,%s,%s,2,%s,%s)',(tid,run,task,Jsonb(dict(artifact_id=str(uuid4()),sha256='a'*64)),Jsonb([dict(artifact_id=str(uuid4()),sha256='a'*64)]),'a'*64,Jsonb([node['params']])))
    for source_contract,error in (('StyleV2','V26_RENDER_INPUT_ROOT_INVALID'),('WorkingImage','V2_INPUT_MANIFEST_UNPUBLISHED')):
        with pytest.raises(CheckViolation,match=error):
            with db.transaction():
                rows=[dict(ref=node['params'],contract='FrozenRenderRecipe'),dict(ref=binding['source'],contract=source_contract)]
                rows.sort(key=lambda entry:entry['ref']['artifact_id'])
                assert db.execute('SELECT photo.register_v2_task_input_batch(%s,%s,%s,0,%s)',(tid,run,task,Jsonb(rows))).fetchone()==(2,)
                db.execute('SELECT photo.seal_v2_task_inputs(%s,%s,%s,%s,%s,2,%s,%s)',(tid,run,task,Jsonb(dict(artifact_id=str(uuid4()),sha256='a'*64)),Jsonb([dict(artifact_id=str(uuid4()),sha256='a'*64)]),'a'*64,Jsonb([node['params']])))
    entries=[dict(ref=node['params'],contract='FrozenRenderRecipe'),dict(ref=binding['source'],contract='WorkingImage'),dict(ref=dict(artifact_id=str(style_ref),sha256='e'*64),contract='StyleV2')]
    if layer_ref is not None:entries.append(dict(ref=layer_ref,contract='LayerStack'))
    if protection_ref is not None:entries.extend([dict(ref=protection_ref,contract='MaskRef'),dict(ref=protection_pixels,contract='NpyArray')])
    entries.sort(key=lambda entry:entry['ref']['artifact_id'])
    entry_count=len(entries)
    entries_sha=sha256(canonical_bytes(entries)).hexdigest()
    def publish_transport(purpose,value):
        raw=canonical_bytes(value);transport_sha=sha256(raw).hexdigest()
        ticket=db.execute('SELECT outcome,artifact_id,token,ready FROM photo.reserve_v2_input_document(%s,%s,%s,%s,%s,%s,%s)',(tid,run,task,purpose,transport_sha,len(raw),'test')).fetchone()
        assert ticket[0]=='created'
        assert db.execute('SELECT photo.complete_v2_input_document(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',(tid,run,task,purpose,ticket[1],ticket[2],transport_sha,len(raw),'test','v1')).fetchone()==(True,)
        return dict(artifact_id=str(ticket[1]),sha256=transport_sha)
    chunk=publish_transport('v2_input_chunk_0',dict(schema_version=1,entries=entries))
    manifest=publish_transport('v2_input_manifest',dict(schema_version=1,task_id=str(task),entry_count=entry_count,entries_sha=entries_sha,chunks=[chunk]))
    assert db.execute('SELECT photo.register_v2_task_input_batch(%s,%s,%s,0,%s)',(tid,run,task,Jsonb(entries))).fetchone()==(entry_count,)
    assert db.execute('SELECT photo.seal_v2_task_inputs(%s,%s,%s,%s,%s,%s,%s,%s)',(tid,run,task,Jsonb(manifest),Jsonb([chunk]),entry_count,entries_sha,Jsonb([node['params']]))).fetchone()==(True,)
    assert db.execute('SELECT sealed,manifest,entries_sha,entry_count,chunks FROM photo.v26_render_input_state(%s,%s,%s)',(tid,run,task)).fetchone()==(True,manifest,entries_sha,str(entry_count),[chunk])
    assert db.execute(query,stage).fetchone()==(True,)
    db.execute('RESET ROLE')
    from photo_worker.v2.contract_lock import RENDER_PREVIOUS_SCHEMAS_SHA256
    # Migration00403 pins this baseline; additive generated schemas do not
    # authorize changing an admitted SQL profile or operation activation.
    assert db.execute("SELECT execution_profile->>'contracts_sha' FROM photo.tasks WHERE id=%s",(task,)).fetchone()==(RENDER_PREVIOUS_SCHEMAS_SHA256,)
    assert db.execute('SELECT photo.v2_input_closure_ready(%s)',(task,)).fetchone()==(True,)
    assert db.execute('SELECT photo.v26_render_activation_ready(%s,%s)',(tid,run)).fetchone()==(True,)
    assert db.execute("SELECT resource_class,routing_key,v2_enabled FROM photo.operation_registry WHERE operation='render.compose' AND version='1'").fetchone()==('gpu','gpu.render_compose',False)
    assert db.execute("SELECT count(*) FROM photo.operation_output_policies WHERE operation='render.compose' AND version='1'").fetchone()==(7,)
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.seal_v2_graph(%s,%s)',(tid,run)).fetchone()==(False,)
    db.execute('RESET ROLE')
    assert db.execute('SELECT status,graph_sealed FROM photo.runs WHERE id=%s',(run,)).fetchone()==('draft',False)
    with db.transaction(force_rollback=True):
        db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) SELECT %s,tenant_id,run_id,'render.compose:99',operation,operation_version,resource_class,'blocked','[]'::jsonb,params_artifact_id,input_digest,execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha' FROM photo.tasks WHERE id=%s",(uuid4(),task))
        assert db.execute('SELECT photo.v26_render_activation_ready(%s,%s)',(tid,run)).fetchone()==(False,)
    for ref,wrong_contract in ((node['params'],'FrozenRecipe'),(binding['source'],'StyleV2')):
        with db.transaction(force_rollback=True):
            saved=db.execute('DELETE FROM photo.v2_task_inputs WHERE task_id=%s AND artifact_id=%s RETURNING tenant_id,project_id,task_id,artifact_id,sha256,ordinal',(task,ref['artifact_id'])).fetchone()
            db.execute('INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,ordinal,contract) VALUES(%s,%s,%s,%s,%s,%s,%s)',(*saved,wrong_contract))
            assert db.execute('SELECT photo.v2_input_closure_ready(%s)',(task,)).fetchone()==(False,)
            assert db.execute('SELECT photo.v26_render_activation_ready(%s,%s)',(tid,run)).fetchone()==(False,)
        assert db.execute('SELECT photo.v2_input_closure_ready(%s)',(task,)).fetchone()==(True,)
    db.execute('SET LOCAL ROLE photo_scheduler')
    changed=list(stage);changed[6]='f'*64
    assert db.execute(query,changed).fetchone()==(False,)
    db.execute('RESET ROLE')
    assert db.execute('SELECT operation,resource_class,state,attempt FROM photo.tasks WHERE id=%s',(task,)).fetchone()==('render.compose','gpu','blocked',0)
    from render_worker_admission_fixture import assert_render_worker_admission
    assert_render_worker_admission(db,tid,run,task,upload[1],digest,body,'b'*64)
    db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(session,))
    assert db.execute('SELECT photo.v26_render_activation_ready(%s,%s)',(tid,run)).fetchone()==(False,)
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute(query,stage).fetchone()==(False,)
    db.execute('RESET ROLE')
    assert db.execute("SELECT photo.v2_input_document_scope(%s,%s,%s,'v2_input_manifest')",(tid,run,task)).fetchone()==(False,)
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT * FROM photo.v26_render_input_state(%s,%s,%s)',(tid,run,task)).fetchall()==[]
    assert db.execute('SELECT photo.seal_v2_graph(%s,%s)',(tid,run)).fetchone()==(False,)
    assert db.execute('SELECT * FROM photo.v26_render_draft_state(%s,%s,%s,%s)',(tid,run,actor,session)).fetchall()==[]


def test_render_empty_layer_stack_live_admission(db):
    test_render_task_stage_registered_root_style_snapshot_and_replay(db,True,True,True,True,empty_layer=True)



def test_render_user_protection_live_admission(db):
    test_render_task_stage_registered_root_style_snapshot_and_replay(db,True,True,True,True,user_protection=True)


def test_render_raster_protection_live_admission(db):
    test_render_task_stage_registered_root_style_snapshot_and_replay(db,True,True,True,True,raster_protection=True)
