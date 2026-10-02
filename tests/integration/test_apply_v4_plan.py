from uuid import uuid4
from psycopg.types.json import Jsonb


def _build(db,count=2):
    user,_=db.execute("SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)",(f'{uuid4()}@test.example',)).fetchone()
    tenant=db.execute('SELECT id FROM photo.tenants WHERE owner_user_id=%s',(user,)).fetchone()[0]
    project=db.execute("INSERT INTO photo.projects(tenant_id,name,visibility) VALUES(%s,'v4','restricted') RETURNING id",(tenant,)).fetchone()[0]
    source=db.execute("INSERT INTO photo.collections(tenant_id,project_id,name,state) VALUES(%s,%s,'source','draft') RETURNING id",(tenant,project)).fetchone()[0]
    items=[]
    for ordinal in range(count):
        digest=chr(ord('a')+ordinal)*64
        artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role) VALUES(%s,'photo',%s,'v1',%s,1024,'application/json','ready','working_image') RETURNING id",(tenant,f'masters/{uuid4()}',digest)).fetchone()[0]
        asset=db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tenant,project,artifact)).fetchone()[0]
        version=db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",(tenant,asset,artifact,digest)).fetchone()[0]
        item=db.execute("INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position) VALUES(%s,%s,%s,%s,%s) RETURNING id",(tenant,source,asset,version,ordinal+1)).fetchone()[0]
        items.append((item,asset,version,artifact,digest))
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count) VALUES(%s,'selection',%s,1,'user',%s,%s,%s) RETURNING id",(tenant,source,user,'a'*64,count)).fetchone()[0]
    for ordinal,(item,_,version,artifact,digest) in enumerate(items):
        db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,%s,%s,%s,%s,%s,%s)",(tenant,snapshot,ordinal,item,version,artifact,digest,Jsonb({'position':ordinal+1})))
    run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'apply','queued',%s,'{}',%s,%s,false) RETURNING id",(tenant,snapshot,'b'*64,user)).fetchone()[0]
    token=uuid4()
    db.execute("INSERT INTO photo.apply_builds(tenant_id,run_id,source_collection_id,item_count,phase,graph_version,lease_token,lease_until) VALUES(%s,%s,%s,%s,'resolve',4,%s,clock_timestamp()+interval '2 minutes')",(tenant,run,source,count,token))
    return tenant,run,token,items


def test_apply_v4_plan_registration_is_fenced_and_idempotent(db):
    tenant,run,token,_=_build(db)
    plan=[
        {'phase':'resolve','operation':'style.resolve','output_role':'resolved_style','cardinality':'item'},
        {'phase':'color','operation':'image.color','output_role':'working_image','cardinality':'item'},
        {'phase':'local','operation':'image.local','output_role':'working_image','cardinality':'item'},
        {'phase':'quality','operation':'quality.check','output_role':'quality_report','cardinality':'item'},
    ]
    digest='c'*64
    assert not db.execute('SELECT photo.register_apply_v4_plan(%s,%s,%s,%s,%s)',(tenant,run,uuid4(),digest,Jsonb(plan))).fetchone()[0]
    assert db.execute('SELECT photo.register_apply_v4_plan(%s,%s,%s,%s,%s)',(tenant,run,token,digest,Jsonb(plan))).fetchone()[0]
    assert db.execute('SELECT photo.register_apply_v4_plan(%s,%s,%s,%s,%s)',(tenant,run,token,digest,Jsonb(plan))).fetchone()[0]
    assert db.execute('SELECT stage_index,phase FROM photo.apply_builds WHERE tenant_id=%s AND run_id=%s',(tenant,run)).fetchone()==(0,'resolve')
    assert db.execute('SELECT count(*) FROM photo.apply_v4_stages WHERE tenant_id=%s AND run_id=%s',(tenant,run)).fetchone()[0]==4
    changed=[*plan];changed[-1]={'phase':'quality_other','operation':'quality.check','output_role':'quality_report','cardinality':'item'}
    assert not db.execute('SELECT photo.register_apply_v4_plan(%s,%s,%s,%s,%s)',(tenant,run,token,digest,Jsonb(changed))).fetchone()[0]


def test_apply_v4_advance_uses_plan_cardinality(db):
    tenant,run,token,_=_build(db)
    plan=[
        {'phase':'resolve','operation':'style.resolve','output_role':'resolved_style','cardinality':'item'},
        {'phase':'color','operation':'image.color','output_role':'working_image','cardinality':'item'},
        {'phase':'local','operation':'image.local','output_role':'working_image','cardinality':'item'},
        {'phase':'quality','operation':'quality.check','output_role':'quality_report','cardinality':'item'},
    ]
    assert db.execute('SELECT photo.register_apply_v4_plan(%s,%s,%s,%s,%s)',(tenant,run,token,'e'*64,Jsonb(plan))).fetchone()[0]
    params=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role) VALUES(%s,'photo-private',%s,'v1',%s,2,'application/json','ready','task_params') RETURNING id",(tenant,f'tenants/{tenant}/runs/{run}/documents/{uuid4()}','f'*64)).fetchone()[0]
    profile=Jsonb({'backend':'cpu','precision':'float32','tile_size':512,'resource_estimate':{'ram_bytes':1,'vram_bytes':0,'scratch_bytes':1,'output_bytes':1,'pixel_count':1}})
    for ordinal in range(2):
        node=uuid4();item=uuid4();asset=uuid4();version=uuid4();key=f'style.resolve/{ordinal:04d}'
        db.execute("INSERT INTO photo.apply_nodes(id,tenant_id,run_id,phase,ordinal,source_item_id,source_asset_id,source_version_id,node_key,operation,inputs,params_sha256,params) VALUES(%s,%s,%s,'resolve',%s,%s,%s,%s,%s,'style.resolve',%s,%s,'{}')",(node,tenant,run,ordinal,item,asset,version,key,Jsonb([{'artifact_id':str(params),'sha256':'f'*64}]),'f'*64))
        db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,%s,%s,'style.resolve','1','cpu','blocked',%s,%s,%s,%s)",(node,tenant,run,key,Jsonb([{'artifact_id':str(params),'sha256':'f'*64}]),params,'a'*64,profile))
    assert db.execute('SELECT photo.advance_apply_v4_build(%s,%s)',(run,token)).fetchone()[0]
    assert db.execute('SELECT phase,stage_index,lease_token FROM photo.apply_builds WHERE run_id=%s',(run,)).fetchone()==('color',1,None)
    assert db.execute("SELECT count(*) FROM photo.tasks WHERE run_id=%s AND state='ready'",(run,)).fetchone()[0]==2
    assert db.execute('SELECT graph_sealed FROM photo.runs WHERE id=%s',(run,)).fetchone()[0]
    for task_id, in db.execute("SELECT id FROM photo.tasks WHERE run_id=%s",(run,)).fetchall():
        result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,0,0,'[]','{}',%s) RETURNING id",(tenant,task_id,'b'*64)).fetchone()[0]
        db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task_id))
    claimed=db.execute('SELECT run_id,phase,graph_version,stage_index FROM photo.claim_apply_build_versioned()').fetchone()
    assert claimed==(run,'color',4,1)
    assert not db.execute('SELECT graph_sealed FROM photo.runs WHERE id=%s',(run,)).fetchone()[0]


def test_apply_v4_plan_rejects_unknown_operation(db):
    tenant,run,token,_=_build(db)
    plan=[
        {'phase':'resolve','operation':'style.resolve','output_role':'resolved_style','cardinality':'item'},
        {'phase':'color','operation':'arbitrary.execute','output_role':'working_image','cardinality':'item'},
        {'phase':'local','operation':'image.local','output_role':'working_image','cardinality':'item'},
        {'phase':'quality','operation':'quality.check','output_role':'quality_report','cardinality':'item'},
    ]
    assert not db.execute('SELECT photo.register_apply_v4_plan(%s,%s,%s,%s,%s)',(tenant,run,token,'d'*64,Jsonb(plan))).fetchone()[0]


def test_apply_v4_plan_accepts_explicit_depth_before_background_blur(db):
    tenant,run,token,_=_build(db)
    plan=[
        {'phase':'mask','operation':'mask.person','output_role':'person_mask_ref','cardinality':'item'},
        {'phase':'resolve','operation':'style.resolve','output_role':'resolved_style','cardinality':'item'},
        {'phase':'local','operation':'image.local','output_role':'working_image','cardinality':'item'},
        {'phase':'depth','operation':'depth.predict','output_role':'relative_depth_ref','cardinality':'item'},
        {'phase':'background','operation':'background.blur','output_role':'working_image','cardinality':'item'},
        {'phase':'quality','operation':'quality.check','output_role':'quality_report','cardinality':'item'},
    ]
    assert db.execute('SELECT photo.register_apply_v4_plan(%s,%s,%s,%s,%s)',(tenant,run,token,'0'*64,Jsonb(plan))).fetchone()[0]


def test_apply_v4_node_is_bound_to_current_plan_and_snapshot(db):
    tenant,run,token,items=_build(db)
    plan=[
        {'phase':'resolve','operation':'style.resolve','output_role':'resolved_style','cardinality':'item'},
        {'phase':'color','operation':'image.color','output_role':'working_image','cardinality':'item'},
        {'phase':'local','operation':'image.local','output_role':'working_image','cardinality':'item'},
        {'phase':'quality','operation':'quality.check','output_role':'quality_report','cardinality':'item'},
    ]
    assert db.execute('SELECT photo.register_apply_v4_plan(%s,%s,%s,%s,%s)',(tenant,run,token,'e'*64,Jsonb(plan))).fetchone()[0]
    item,asset,version,artifact,digest=items[0]
    node={'id':str(uuid4()),'phase':'resolve','ordinal':0,'source_item_id':str(item),'source_asset_id':str(asset),'source_version_id':str(version),
          'node_key':'style.resolve/0000','operation':'style.resolve','inputs':[{'artifact_id':str(artifact),'sha256':digest}],
          'prerequisites':[],'params_sha256':'f'*64,'params':{'image':{'artifact_id':str(artifact),'sha256':digest}}}
    assert db.execute('SELECT photo.register_apply_v4_node(%s,%s,%s,%s)',(tenant,run,token,Jsonb(node))).fetchone()[0]
    assert db.execute('SELECT photo.register_apply_v4_node(%s,%s,%s,%s)',(tenant,run,token,Jsonb(node))).fetchone()[0]
    wrong={**node,'id':str(uuid4()),'node_key':'wrong/0000','operation':'image.color'}
    assert not db.execute('SELECT photo.register_apply_v4_node(%s,%s,%s,%s)',(tenant,run,token,Jsonb(wrong))).fetchone()[0]
    stale={**node,'id':str(uuid4()),'node_key':'stale/0000','source_item_id':str(uuid4())}
    assert not db.execute('SELECT photo.register_apply_v4_node(%s,%s,%s,%s)',(tenant,run,token,Jsonb(stale))).fetchone()[0]


def test_apply_v4_adoption_uses_quality_target_with_canonical_provenance(db):
    definition=db.execute("SELECT pg_get_functiondef('photo.adopt_apply_results(uuid,uuid,uuid)'::regprocedure)").fetchone()[0]
    assert "b.graph_version=4" in definition
    assert "n.params->'target'" in definition
    assert "candidate.created_by_task_id=pt.id" in definition
    assert "x.produced_attempt=producer_result.attempt" in definition
    assert "x.produced_fence=producer_result.fencing_token" in definition
    assert "producer_result.outputs @>" in definition
    assert "source_phase:=CASE WHEN b.graph_version=4 THEN 'quality' ELSE 'local' END" in definition
    assert "v.decision_route='skip'" in definition
    assert "skipped:=skipped+1" in definition
    assert "adopted+skipped<>b.item_count" in definition
    assert "v.decision_route='repair'" in definition
    assert "JOIN photo.defect_branches d" in definition
    assert "rt.operation='defects.repair'" in definition


def test_apply_v4_opens_one_fully_bound_defect_review_per_item(db):
    tenant,run,token,items=_build(db)
    plan=[
        {'phase':'resolve','operation':'style.resolve','output_role':'resolved_style','cardinality':'item'},
        {'phase':'defects_propose','operation':'defects.propose','output_role':'defect_candidates','cardinality':'item'},
        {'phase':'defects_review','operation':'','output_role':'','cardinality':'review'},
        {'phase':'defects_repair','operation':'defects.repair','output_role':'working_image','cardinality':'item'},
        {'phase':'quality','operation':'quality.check','output_role':'quality_report','cardinality':'item'},
    ]
    assert db.execute('SELECT photo.register_apply_v4_plan(%s,%s,%s,%s,%s)',(tenant,run,token,'9'*64,Jsonb(plan))).fetchone()[0]
    profile=Jsonb({'backend':'cpu','precision':'float32','tile_size':512,'resource_estimate':{'ram_bytes':2147483648,'vram_bytes':0,'scratch_bytes':1048576,'output_bytes':1048576,'pixel_count':1}})
    for ordinal,(item,asset,version,image,image_sha) in enumerate(items):
        resolved_task=uuid4();proposal_task=uuid4()
        params=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role) VALUES(%s,'photo-private',%s,'v1',%s,2,'application/json','ready','task_params') RETURNING id",(tenant,f'params/{uuid4()}',uuid4().hex*2)).fetchone()[0]
        for task,phase,operation,node_key in ((resolved_task,'resolve','style.resolve',f'style.resolve/{ordinal:04d}'),(proposal_task,'defects_propose','defects.propose',f'defects.propose/{ordinal:04d}')):
            node_params={'image':{'artifact_id':str(image),'sha256':image_sha}}
            db.execute("INSERT INTO photo.apply_nodes(id,tenant_id,run_id,phase,ordinal,source_item_id,source_asset_id,source_version_id,node_key,operation,inputs,prerequisites,params_sha256,params) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,'{}',%s,%s)",(task,tenant,run,phase,ordinal,item,asset,version,node_key,operation,Jsonb([{'artifact_id':str(image),'sha256':image_sha}]),'8'*64,Jsonb(node_params)))
            db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,%s,%s,%s,'1','cpu','blocked',%s,%s,%s,%s)",(task,tenant,run,node_key,operation,Jsonb([{'artifact_id':str(image),'sha256':image_sha}]),params,'7'*64,profile))
        resolved=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'photo',%s,'v1',%s,128,'application/json','ready','resolved_style',%s,0,0) RETURNING id",(tenant,f'outputs/{uuid4()}','6'*64,resolved_task)).fetchone()[0]
        proposal=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'photo',%s,'v1',%s,128,'application/json','ready','defect_candidates',%s,0,0) RETURNING id",(tenant,f'outputs/{uuid4()}','5'*64,proposal_task)).fetchone()[0]
        report=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'photo',%s,'v1',%s,128,'application/json','ready','defect_propose_report',%s,0,0) RETURNING id",(tenant,f'outputs/{uuid4()}','4'*64,proposal_task)).fetchone()[0]
        for task,outputs,digest in ((resolved_task,[{'artifact_id':str(resolved),'sha256':'6'*64}],'3'*64),(proposal_task,[{'artifact_id':str(proposal),'sha256':'5'*64},{'artifact_id':str(report),'sha256':'4'*64}],'2'*64)):
            result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,0,0,%s,'{}',%s) RETURNING id",(tenant,task,Jsonb(outputs),digest)).fetchone()[0]
            db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
    db.execute("UPDATE photo.apply_builds SET phase='defects_review',stage_index=2,lease_token=NULL,lease_until=NULL WHERE run_id=%s",(run,))
    db.execute("UPDATE photo.runs SET status='running',graph_sealed=true WHERE id=%s",(run,))
    claimed=db.execute('SELECT run_id,phase,lease_token FROM photo.claim_apply_build_versioned()').fetchone()
    assert claimed[:2]==(run,'defects_review')
    assert db.execute('SELECT photo.open_apply_v4_defect_reviews(%s,%s,%s)',(tenant,run,claimed[2])).fetchone()[0]
    assert db.execute("SELECT count(*) FROM photo.reviews WHERE tenant_id=%s AND run_id=%s AND kind='defects' AND state='pending'",(tenant,run)).fetchone()[0]==2
    assert db.execute('SELECT status,graph_sealed FROM photo.runs WHERE id=%s',(run,)).fetchone()==('waiting_review',False)
    assert db.execute('SELECT lease_token FROM photo.apply_builds WHERE run_id=%s',(run,)).fetchone()[0] is None
    assert db.execute('SELECT count(*) FROM photo.claim_apply_build_versioned()').fetchone()[0]==0


def test_apply_v4_sensor_dust_group_review_and_decision_barrier(db):
    tenant,run,token,items=_build(db,5)
    plan=[
        {'phase':'resolve','operation':'style.resolve','output_role':'resolved_style','cardinality':'item'},
        {'phase':'dust_detect','operation':'sensor.dust','output_role':'dust_candidates','cardinality':'group'},
        {'phase':'dust_review','operation':'','output_role':'','cardinality':'review'},
        {'phase':'dust_repair','operation':'sensor.dust_repair','output_role':'working_image','cardinality':'item'},
        {'phase':'quality','operation':'quality.check','output_role':'quality_report','cardinality':'item'},
    ]
    assert db.execute('SELECT photo.register_apply_v4_plan(%s,%s,%s,%s,%s)',(tenant,run,token,'1'*64,Jsonb(plan))).fetchone()[0]
    db.execute("UPDATE photo.apply_builds SET phase='dust_detect',stage_index=1 WHERE run_id=%s",(run,))
    assert db.execute('SELECT photo.set_apply_v4_group_count(%s,%s,%s,1)',(tenant,run,token)).fetchone()[0]
    task=uuid4();first=items[0];frames=[{'artifact_id':str(x[3]),'sha256':x[4]} for x in items]
    params={'frames':frames,'camera_id':'raw-'+'2'*64,'sensor_width':400,'sensor_height':300,
            'raw_crop':{'x':0,'y':0,'width':400,'height':300},'orientation':1,'recurrence':.8}
    db.execute("INSERT INTO photo.apply_nodes(id,tenant_id,run_id,phase,ordinal,source_item_id,source_asset_id,source_version_id,node_key,operation,inputs,prerequisites,params_sha256,params) VALUES(%s,%s,%s,'dust_detect',0,%s,%s,%s,'sensor.dust/0000','sensor.dust',%s,'{}',%s,%s)",(task,tenant,run,first[0],first[1],first[2],Jsonb(frames),'3'*64,Jsonb(params)))
    params_artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role) VALUES(%s,'photo-private',%s,'v1',%s,2,'application/json','ready','task_params') RETURNING id",(tenant,f'params/{uuid4()}','3'*64)).fetchone()[0]
    profile=Jsonb({'backend':'cpu','precision':'float32','tile_size':512,'resource_estimate':{'ram_bytes':536870912,'vram_bytes':0,'scratch_bytes':1048576,'output_bytes':1048576,'pixel_count':1}})
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,%s,'sensor.dust/0000','sensor.dust','1','cpu','blocked',%s,%s,%s,%s)",(task,tenant,run,Jsonb(frames),params_artifact,'4'*64,profile))
    proposal=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'photo',%s,'v1',%s,128,'application/json','ready','dust_candidates',%s,0,0) RETURNING id",(tenant,f'outputs/{uuid4()}','5'*64,task)).fetchone()[0]
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,review_required,result_digest) VALUES(%s,%s,0,0,%s,'{}',true,%s) RETURNING id",(tenant,task,Jsonb([{'artifact_id':str(proposal),'sha256':'5'*64}]),'6'*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
    db.execute("UPDATE photo.apply_builds SET phase='dust_review',stage_index=2,lease_token=NULL,lease_until=NULL WHERE run_id=%s",(run,))
    db.execute("UPDATE photo.runs SET status='running',graph_sealed=true WHERE id=%s",(run,))
    claimed=db.execute('SELECT run_id,phase,lease_token FROM photo.claim_apply_build_versioned()').fetchone();assert claimed[:2]==(run,'dust_review')
    assert db.execute('SELECT photo.process_apply_v4_dust_reviews(%s,%s,%s)',(tenant,run,claimed[2])).fetchone()[0]=='opened'
    review=db.execute("SELECT id,revision FROM photo.reviews WHERE tenant_id=%s AND run_id=%s AND kind='sensor_dust'",(tenant,run)).fetchone();assert review
    assert db.execute('SELECT count(*) FROM photo.claim_apply_build_versioned()').fetchone()[0]==0
    selection={'version':'sensor_dust_review_v1','proposal':{'artifact_id':str(proposal),'sha256':'5'*64},'base':frames[0],'revision':review[1]+1,'accepted_candidate_ids':[],'rejected_candidate_ids':[]}
    actor=db.execute('SELECT owner_user_id FROM photo.tenants WHERE id=%s',(tenant,)).fetchone()[0]
    db.execute("UPDATE photo.reviews SET state='decided',decision='reject',decision_route='base',revision=revision+1,selection=%s,decision_hash=%s,decided_by=%s,decided_at=clock_timestamp() WHERE id=%s",(Jsonb(selection),'7'*64,actor,review[0]))
    claimed=db.execute('SELECT run_id,phase,lease_token FROM photo.claim_apply_build_versioned()').fetchone();assert claimed[:2]==(run,'dust_review')
    assert db.execute('SELECT photo.process_apply_v4_dust_reviews(%s,%s,%s)',(tenant,run,claimed[2])).fetchone()[0]=='advanced'
    phase,sealed,status=db.execute('SELECT b.phase,r.graph_sealed,r.status FROM photo.apply_builds b JOIN photo.runs r ON r.id=b.run_id WHERE b.run_id=%s',(run,)).fetchone();assert (phase,sealed,status)==('dust_repair',True,'running')
    repair_claim=db.execute('SELECT run_id,phase,lease_token FROM photo.claim_apply_build_versioned()').fetchone();assert repair_claim[:2]==(run,'dust_repair')
    decisions=db.execute('SELECT group_ordinal,task_id,artifact_id,selection,route FROM photo.apply_v4_dust_decisions(%s,%s,%s)',(tenant,run,repair_claim[2])).fetchall()
    assert len(decisions)==1 and decisions[0][0:3]==(0,task,proposal) and decisions[0][3]==selection and decisions[0][4]=='base'


def test_apply_v4_defect_outputs_merge_repair_base_and_skip(db):
    tenant,run,token,items=_build(db,3)
    actor=db.execute('SELECT owner_user_id FROM photo.tenants WHERE id=%s',(tenant,)).fetchone()[0]
    plan=[
        {'phase':'resolve','operation':'style.resolve','output_role':'resolved_style','cardinality':'item'},
        {'phase':'defects_propose','operation':'defects.propose','output_role':'defect_candidates','cardinality':'item'},
        {'phase':'defects_review','operation':'','output_role':'','cardinality':'review'},
        {'phase':'quality','operation':'quality.check','output_role':'quality_report','cardinality':'item'},
    ]
    assert db.execute('SELECT photo.register_apply_v4_plan(%s,%s,%s,%s,%s)',(tenant,run,token,'1'*64,Jsonb(plan))).fetchone()[0]
    profile=Jsonb({'backend':'cpu','precision':'float32','tile_size':256,'resource_estimate':{'ram_bytes':2147483648,'vram_bytes':0,'scratch_bytes':1048576,'output_bytes':1048576,'pixel_count':1}})
    expected=[]
    for ordinal,route_data in enumerate((('repair','accept'),('base','reject'),('skip','skip'))):
        item,asset,version,image,image_sha=items[ordinal];route,decision=route_data
        proposal_task=uuid4();resolved_task=uuid4()
        params=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role) VALUES(%s,'photo-private',%s,'v1',%s,2,'application/json','ready','task_params') RETURNING id",(tenant,f'params/{uuid4()}',uuid4().hex*2)).fetchone()[0]
        for task,phase,operation,key in ((resolved_task,'resolve','style.resolve',f'style.resolve/mixed/{ordinal}'),(proposal_task,'defects_propose','defects.propose',f'defects.propose/mixed/{ordinal}')):
            db.execute("INSERT INTO photo.apply_nodes(id,tenant_id,run_id,phase,ordinal,source_item_id,source_asset_id,source_version_id,node_key,operation,inputs,prerequisites,params_sha256,params) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,'{}',%s,%s)",(task,tenant,run,phase,ordinal,item,asset,version,key,operation,Jsonb([{'artifact_id':str(image),'sha256':image_sha}]),'2'*64,Jsonb({'image':{'artifact_id':str(image),'sha256':image_sha}})))
            db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,%s,%s,%s,'1','cpu','blocked',%s,%s,%s,%s)",(task,tenant,run,key,operation,Jsonb([{'artifact_id':str(image),'sha256':image_sha}]),params,'3'*64,profile))
        resolved=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'photo',%s,'v1',%s,128,'application/json','ready','resolved_style',%s,0,0) RETURNING id",(tenant,f'outputs/{uuid4()}','4'*64,resolved_task)).fetchone()[0]
        proposal=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'photo',%s,'v1',%s,128,'application/json','ready','defect_candidates',%s,0,0) RETURNING id",(tenant,f'outputs/{uuid4()}','5'*64,proposal_task)).fetchone()[0]
        report=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'photo',%s,'v1',%s,128,'application/json','ready','defect_propose_report',%s,0,0) RETURNING id",(tenant,f'outputs/{uuid4()}','6'*64,proposal_task)).fetchone()[0]
        review=db.execute("INSERT INTO photo.reviews(tenant_id,run_id,task_id,candidate_artifact_id,base_artifact_id,candidate_hash,kind,reasons,resolved_artifact_id,proposal_report_artifact_id) VALUES(%s,%s,%s,%s,%s,%s,'defects','[]',%s,%s) RETURNING id",(tenant,run,proposal_task,proposal,image,'5'*64,resolved,report)).fetchone()[0]
        selection={'version':'defect_review_v1','proposal':{'artifact_id':str(proposal),'sha256':'5'*64},'image':{'artifact_id':str(image),'sha256':image_sha},'label_map':{'artifact_id':str(image),'sha256':image_sha},'revision':2,'accepted_candidate_ids':[],'rejected_candidate_ids':[]}
        db.execute("UPDATE photo.reviews SET state='decided',revision=2,decision=%s,decided_by=%s,decided_at=clock_timestamp(),selection=%s,decision_hash=%s,decision_route=%s WHERE id=%s",(decision,actor,Jsonb(selection),str(ordinal+7)*64,route,review))
        expansion=uuid4()
        db.execute("INSERT INTO photo.graph_expansions(id,tenant_id,run_id,parent_task_id,kind,state,expected_count) VALUES(%s,%s,%s,%s,%s,'closed',%s)",(expansion,tenant,run,proposal_task,f'review.defects/{review}',1 if route=='repair' else 0))
        repair_task=None;branch_base=image if route=='base' else None;final=image
        if route=='repair':
            repair_task=uuid4();key=f'defects.repair/review/{review}'
            db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,%s,%s,'defects.repair','1','cpu','blocked',%s,%s,%s,%s)",(repair_task,tenant,run,key,Jsonb([{'artifact_id':str(image),'sha256':image_sha}]),params,'7'*64,profile))
            final=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'photo',%s,'v1',%s,128,'application/json','ready','working_image',%s,0,0) RETURNING id",(tenant,f'outputs/{uuid4()}','8'*64,repair_task)).fetchone()[0]
            result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,0,0,%s,'{}',%s) RETURNING id",(tenant,repair_task,Jsonb([{'artifact_id':str(final),'sha256':'8'*64}]),'9'*64)).fetchone()[0]
            db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,repair_task))
        db.execute("INSERT INTO photo.defect_branches(expansion_id,tenant_id,run_id,review_id,completion_token,payload,task_id,base_artifact_id) VALUES(%s,%s,%s,%s,%s,%s,%s,%s)",(expansion,tenant,run,review,uuid4(),Jsonb({'route':route}),repair_task,branch_base))
        expected.append((ordinal,route,final))
    db.execute("UPDATE photo.apply_builds SET phase='quality',stage_index=3 WHERE run_id=%s",(run,))
    rows=db.execute('SELECT ordinal,route,artifact_id FROM photo.apply_v4_defect_outputs(%s,%s,%s)',(tenant,run,token)).fetchall()
    assert rows==expected
