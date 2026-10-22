"""Culling graph registration and narrow ACLs. Authored; not executed."""
from psycopg.types.json import Jsonb
from test_apply_v4_plan import _build


def test_culling_stages_register_as_bounded_item_group_aggregate_review(db):
    tenant,run,token,_=_build(db)
    plan=[dict(phase='resolve',operation='style.resolve',output_role='resolved_style',cardinality='item')]
    for phase,op,role,card in [('cull_embedding','features.scene','scene_embedding','item'),('cull_features','collection.features','scene_features','item'),('cull_pack','collection.features_pack','scene_feature_pack','group'),('cull_group','collection.group','scene_groups','aggregate'),('cull_rank','collection.cull','cull_recommendations','aggregate'),('cull_review','','','review'),('color','image.color','working_image','item'),('local','image.local','working_image','item'),('quality','quality.check','quality_report','item')]:
        plan.append(dict(phase=phase,operation=op,output_role=role,cardinality=card))
    assert db.execute('SELECT photo.register_apply_v4_plan(%s,%s,%s,%s,%s)',(tenant,run,token,'e'*64,Jsonb(plan))).fetchone()[0]
    assert db.execute("SELECT cardinality FROM photo.apply_v4_stages WHERE tenant_id=%s AND run_id=%s AND phase='cull_pack'",(tenant,run)).fetchone()==('group',)
    for op,role in [('collection.features','scene_features'),('collection.features_pack','scene_feature_pack')]:
        assert db.execute('SELECT resource_class,backends FROM photo.operation_registry WHERE operation=%s AND version=%s',(op,'1')).fetchone()==('cpu',['cpu'])
        assert db.execute('SELECT max_count FROM photo.operation_output_policies WHERE operation=%s AND role=%s',(op,role)).fetchone()==(1,)
    assert db.execute("SELECT count(*) FROM photo.operation_output_policies WHERE operation IN('collection.group','collection.cull') AND max_bytes=8388608").fetchone()[0]==2


def test_culling_control_functions_and_purge_have_narrow_authority(db):
    for signature in ['photo.register_apply_culling_review(uuid,uuid,uuid,jsonb)','photo.process_apply_culling_review(uuid,uuid,uuid)']:
        assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(signature,)).fetchone()[0]
        assert not db.execute("SELECT EXISTS(SELECT FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE p.oid=%s::regprocedure AND acl.grantee=0 AND acl.privilege_type='EXECUTE')",(signature,)).fetchone()[0]
    for role in ['photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal']:
        assert not db.execute("SELECT has_table_privilege(%s,'photo.apply_culling_reviews','DELETE')",(role,)).fetchone()[0]
    assert db.execute("SELECT has_table_privilege('photo_fn_owner','photo.apply_culling_reviews','DELETE')").fetchone()[0]
    purge=db.execute("SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure)").fetchone()[0]
    assert 'DELETE FROM photo.apply_culling_reviews WHERE tenant_id=j.tenant_id' in purge
    private=db.execute("SELECT pg_get_functiondef('photo.adopt_apply_results_before_atomic_rejection(uuid,uuid,uuid)'::regprocedure)").fetchone()[0]
    assert "cv.selection->'selected_item_ids' @> jsonb_build_array(n.source_item_id::text)" in private
    assert not db.execute("SELECT has_function_privilege('photo_scheduler','photo.adopt_apply_results_before_atomic_rejection(uuid,uuid,uuid)','EXECUTE')").fetchone()[0]


def _culling_barrier(db):
    from uuid import uuid4
    tenant,run,token,items=_build(db)
    plan=[dict(phase=phase,operation=op,output_role=role,cardinality=card) for phase,op,role,card in [
        ('resolve','style.resolve','resolved_style','item'),('cull_group','collection.group','scene_groups','aggregate'),('cull_rank','collection.cull','cull_recommendations','aggregate'),('cull_review','','','review'),('color','image.color','working_image','item'),('local','image.local','working_image','item'),('quality','quality.check','quality_report','item')]]
    assert db.execute('SELECT photo.register_apply_v4_plan(%s,%s,%s,%s,%s)',(tenant,run,token,'e'*64,Jsonb(plan))).fetchone()[0]
    db.execute("UPDATE photo.apply_builds SET phase='cull_review',stage_index=3 WHERE run_id=%s",(run,))
    db.execute("UPDATE photo.runs r SET status='running',config_snapshot=jsonb_build_object('input_snapshot_digest',s.digest) FROM photo.snapshots s WHERE r.id=%s AND s.id=r.input_snapshot_id",(run,))
    def produced(phase,operation,role,params):
        task=uuid4();item,asset,version,source,source_sha=items[0]
        db.execute("INSERT INTO photo.apply_nodes(id,tenant_id,run_id,phase,ordinal,source_item_id,source_asset_id,source_version_id,node_key,operation,inputs,params_sha256,params) VALUES(%s,%s,%s,%s,0,%s,%s,%s,%s,%s,%s,%s,%s)",(task,tenant,run,phase,item,asset,version,phase+'/0000',operation,Jsonb([dict(artifact_id=str(source),sha256=source_sha)]),'f'*64,Jsonb(params)))
        db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,input_digest,execution_profile) VALUES(%s,%s,%s,%s,%s,'1','cpu','blocked','[]',%s,'{}')",(task,tenant,run,phase+'/0000',operation,'a'*64))
        artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'photo-private',%s,'v1',%s,256,'application/json','ready',%s,%s,0,0) RETURNING id",(tenant,f'outputs/{uuid4()}','b'*64,role,task)).fetchone()[0]
        ref=dict(artifact_id=str(artifact),sha256='b'*64)
        result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,0,0,%s,'{}',%s) RETURNING id",(tenant,task,Jsonb([ref]),uuid4().hex*2)).fetchone()[0]
        db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
        return ref
    groups=produced('cull_group','collection.group','scene_groups',{})
    proposal=produced('cull_rank','collection.cull','cull_recommendations',dict(groups=groups))
    rows=[dict(item_id=str(item),asset_id=str(asset),asset_version_id=str(version),scene_id='s1',exact_group_id='e1',burst_group_id='b1',rank=i+1,score=.8,recommended=i==0,manual=False,eye_review=True,eye_state='unknown') for i,(item,asset,version,_,_) in enumerate(items)]
    return tenant,run,token,dict(groups=groups,proposal=proposal,items=rows,never_delete=True)


def test_culling_review_rejects_incomplete_json_and_preserves_snapshot(db):
    import copy
    tenant,run,token,projection=_culling_barrier(db)
    before=db.execute('SELECT input_snapshot_id,config_snapshot FROM photo.runs WHERE id=%s',(run,)).fetchone()
    for missing in ['never_delete','items','groups','proposal']:
        bad=copy.deepcopy(projection);bad.pop(missing)
        assert not db.execute('SELECT photo.register_apply_culling_review(%s,%s,%s,%s)',(tenant,run,token,Jsonb(bad))).fetchone()[0]
    for missing in ['rank','score','eye_state','recommended','asset_id']:
        bad=copy.deepcopy(projection);bad['items'][0].pop(missing)
        assert not db.execute('SELECT photo.register_apply_culling_review(%s,%s,%s,%s)',(tenant,run,token,Jsonb(bad))).fetchone()[0]
    assert db.execute('SELECT count(*) FROM photo.apply_culling_reviews WHERE run_id=%s',(run,)).fetchone()[0]==0
    assert db.execute('SELECT photo.register_apply_culling_review(%s,%s,%s,%s)',(tenant,run,token,Jsonb(projection))).fetchone()[0]
    assert db.execute('SELECT input_snapshot_id,config_snapshot FROM photo.runs WHERE id=%s',(run,)).fetchone()==before
    assert db.execute("SELECT status,graph_sealed FROM photo.runs WHERE id=%s",(run,)).fetchone()==('waiting_review',False)
    assert db.execute("SELECT state,kind FROM photo.reviews WHERE run_id=%s",(run,)).fetchone()==('pending','culling')
    # A pending selection never advances, including a stale pre-open scheduler token.
    assert not db.execute('SELECT photo.process_apply_culling_review(%s,%s,%s)',(tenant,run,token)).fetchone()[0]
    assert db.execute('SELECT phase FROM photo.apply_builds WHERE run_id=%s',(run,)).fetchone()==('cull_review',)
