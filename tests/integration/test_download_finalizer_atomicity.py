from uuid import uuid4

from psycopg.types.json import Jsonb


def test_download_finalizer_never_publishes_partial_volumes(db):
    digest="f"*64
    user=db.execute("SELECT user_id FROM photo.auth_bootstrap_user(%s,'en',true)",(f"{uuid4()}@test.example",)).fetchone()[0]
    tenant=db.execute("SELECT id FROM photo.tenants WHERE owner_user_id=%s",(user,)).fetchone()[0]
    project=db.execute("INSERT INTO photo.projects(tenant_id,name,visibility) VALUES(%s,'Finalizer','workspace') RETURNING id",(tenant,)).fetchone()[0]
    source=db.execute("""INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role)
        VALUES(%s,'photo',%s,'source-v1',%s,1,'image/jpeg','working_image') RETURNING id""",(tenant,f"sources/{uuid4()}",digest)).fetchone()[0]
    asset=db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tenant,project,source)).fetchone()[0]
    version=db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",(tenant,asset,source,digest)).fetchone()[0]
    collection=db.execute("INSERT INTO photo.collections(tenant_id,project_id,name,state) VALUES(%s,%s,'Source','draft') RETURNING id",(tenant,project)).fetchone()[0]
    snapshot=db.execute("""INSERT INTO photo.snapshots(tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count)
        VALUES(%s,'selection',%s,1,'user',%s,%s,2) RETURNING id""",(tenant,collection,user,digest)).fetchone()[0]
    run=db.execute("""INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed)
        VALUES(%s,'download','queued',%s,'{}',%s,%s,false) RETURNING id""",(tenant,snapshot,digest,user)).fetchone()[0]
    job=db.execute("""INSERT INTO photo.download_jobs(tenant_id,collection_id,principal_type,principal_id,snapshot_id,preset,status,run_id)
        VALUES(%s,%s,'user',%s,%s,'{}','queued',%s) RETURNING id""",(tenant,collection,user,snapshot,run)).fetchone()[0]
    db.execute("UPDATE photo.download_build_state SET phase='archiving' WHERE tenant_id=%s AND run_id=%s",(tenant,run))
    db.execute("UPDATE photo.runs SET status='running',graph_sealed=true WHERE id=%s",(run,))
    db.execute("UPDATE photo.download_jobs SET status='building' WHERE id=%s",(job,))

    params=db.execute("""INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role)
        VALUES(%s,'photo',%s,'params-v1',%s,2,'application/json','params') RETURNING id""",(tenant,f"params/{uuid4()}",digest)).fetchone()[0]
    tasks=[uuid4(),uuid4()]
    for index,task in enumerate(tasks,1):
        inputs=Jsonb([{"artifact_id":str(source),"sha256":digest}])
        db.execute("""INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
            VALUES(%s,%s,%s,%s,'archive.build','1','archive','blocked',%s,%s,%s,%s)""",
            (task,tenant,run,f"archive.build/{index:04d}",inputs,params,digest,Jsonb({"backend":"cpu"})))
        db.execute("""INSERT INTO photo.download_archive_plans(id,tenant_id,run_id,volume_no,node_key,inputs,producer_tasks,params_sha,params)
            VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s)""",
            (task,tenant,run,index,f"archive.build/{index:04d}",inputs,Jsonb([str(task)]),digest,Jsonb({"volume_no":index})))
        db.execute("INSERT INTO photo.download_job_items(tenant_id,download_job_id,ordinal,asset_version_id,artifact_id,sha256,entry_name,volume_no,bytes,delivery_kind) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,1,'zip')",
            (tenant,job,index-1,version,source,digest,f"photo-{index}.jpg",index))
        db.execute("INSERT INTO photo.download_volumes(tenant_id,download_job_id,volume_no,status) VALUES(%s,%s,%s,'queued')",(tenant,job,index))

    def attach_result(task,ordinal,valid=True):
        outputs=[]
        if valid:
            archive=db.execute("""INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,created_by_task_id,output_role)
                VALUES(%s,'photo',%s,%s,%s,10,'application/zip',%s,'archive') RETURNING id""",
                (tenant,f"archives/{uuid4()}",f"archive-v{ordinal}",digest,task)).fetchone()[0]
            outputs.append({"artifact_id":str(archive),"sha256":digest})
        manifest=db.execute("""INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,created_by_task_id,output_role)
            VALUES(%s,'photo',%s,%s,%s,10,'application/json',%s,'archive_manifest') RETURNING id""",
            (tenant,f"manifests/{uuid4()}",f"manifest-v{ordinal}",digest,task)).fetchone()[0]
        outputs.append({"artifact_id":str(manifest),"sha256":digest})
        result=db.execute("""INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest)
            VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id""",(tenant,task,Jsonb(outputs),digest)).fetchone()[0]
        db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))

    attach_result(tasks[0],1)
    attach_result(tasks[1],2,valid=False)
    db.execute("SET LOCAL ROLE photo_scheduler")
    assert db.execute("SELECT photo.finalize_download_archives(%s,%s)",(tenant,run)).fetchone()[0] is False
    db.execute("RESET ROLE")
    assert db.execute("SELECT array_agg(status ORDER BY volume_no) FROM photo.download_volumes WHERE download_job_id=%s",(job,)).fetchone()[0]==["queued","queued"]
    assert db.execute("SELECT status FROM photo.download_jobs WHERE id=%s",(job,)).fetchone()[0]=="building"
