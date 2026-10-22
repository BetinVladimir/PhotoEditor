from uuid import uuid4

import pytest

from psycopg.types.json import Jsonb


@pytest.mark.parametrize("defect", ["archive_task_failed", "missing_archive", "stale_attempt", "stale_fence", "expired_archive", "expired_manifest", "postsign_stale_attempt", "postsign_stale_fence", "direct_valid", "direct_stale_attempt", "direct_stale_fence", "direct_stale_artifact_fence", "direct_pending_producer"])
def test_download_finalizer_never_publishes_partial_volumes(db, defect):
    digest="f"*64
    user=db.execute("SELECT user_id FROM photo.auth_bootstrap_user(%s,'en',true)",(f"{uuid4()}@test.example",)).fetchone()[0]
    tenant=db.execute("SELECT id FROM photo.tenants WHERE owner_user_id=%s",(user,)).fetchone()[0]
    project=db.execute("INSERT INTO photo.projects(tenant_id,name,visibility,download_allowed) VALUES(%s,'Finalizer','workspace',true) RETURNING id",(tenant,)).fetchone()[0]
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
            archive=db.execute("""INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,created_by_task_id,output_role,produced_attempt,produced_fence)
                VALUES(%s,'photo',%s,%s,%s,10,%s,%s,%s,1,1) RETURNING id""",
                (tenant,f"archives/{uuid4()}",f"archive-v{ordinal}",digest,"image/jpeg" if defect.startswith("direct_") else "application/zip",task,"encoded_image" if defect.startswith("direct_") else "archive")).fetchone()[0]
            outputs.append({"artifact_id":str(archive),"sha256":digest})
        manifest=db.execute("""INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,created_by_task_id,output_role,produced_attempt,produced_fence)
            VALUES(%s,'photo',%s,%s,%s,10,'application/json',%s,'archive_manifest',1,1) RETURNING id""",
            (tenant,f"manifests/{uuid4()}",f"manifest-v{ordinal}",digest,task)).fetchone()[0]
        outputs.append({"artifact_id":str(manifest),"sha256":digest})
        result=db.execute("""INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest)
            VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id""",(tenant,task,Jsonb(outputs),digest)).fetchone()[0]
        db.execute("UPDATE photo.tasks SET state='succeeded',attempt=1,fencing_token=1,canonical_result_id=%s WHERE id=%s",(result,task))

    if defect=="archive_task_failed":
        db.execute("UPDATE photo.tasks SET state='failed' WHERE id=ANY(%s)",(tasks,))
        assert db.execute("SELECT photo.finalize_download_archives(%s,%s)",(tenant,run)).fetchone()[0] is True
        status,failure=db.execute("SELECT status,failure FROM photo.download_jobs WHERE id=%s",(job,)).fetchone()
        assert status=="failed"
        assert failure=={"code":"DOWNLOAD_ARCHIVE_FAILED","items":[{"ordinal":i,"asset_version_id":str(version),"code":"ARCHIVE_FAILED"} for i in range(2)]}
        assert db.execute("SELECT count(*) FROM photo.download_volumes WHERE download_job_id=%s AND status='ready'",(job,)).fetchone()[0]==0
        return

    attach_result(tasks[0],1)
    attach_result(tasks[1],2,valid=defect != "missing_archive")
    if defect.startswith("direct_"):
        db.execute("DELETE FROM photo.download_archive_plans WHERE tenant_id=%s AND run_id=%s", (tenant,run))
        db.execute("UPDATE photo.tasks SET operation='export.encode' WHERE tenant_id=%s AND run_id=%s", (tenant,run))
        db.execute("UPDATE photo.download_job_items SET delivery_kind='single' WHERE tenant_id=%s AND download_job_id=%s", (tenant,job))
        for number,task in enumerate(tasks,1):
            db.execute("""UPDATE photo.download_volumes SET status='ready',artifact_id=(
                SELECT id FROM photo.artifacts WHERE tenant_id=%s AND created_by_task_id=%s AND output_role='encoded_image')
                WHERE tenant_id=%s AND download_job_id=%s AND volume_no=%s""", (tenant,task,tenant,job,number))
        db.execute("UPDATE photo.download_build_state SET phase='packaging' WHERE tenant_id=%s AND run_id=%s", (tenant,run))
        db.execute("UPDATE photo.runs SET graph_sealed=false WHERE tenant_id=%s AND id=%s", (tenant,run))
        if defect == "direct_stale_attempt":
            db.execute("UPDATE photo.tasks SET attempt=2 WHERE id=%s", (tasks[1],))
        elif defect == "direct_stale_fence":
            db.execute("UPDATE photo.tasks SET fencing_token=2 WHERE id=%s", (tasks[1],))
        elif defect == "direct_stale_artifact_fence":
            db.execute("UPDATE photo.artifacts SET produced_fence=2 WHERE created_by_task_id=%s AND output_role='encoded_image'",(tasks[1],))
        elif defect == "direct_pending_producer":
            db.execute("UPDATE photo.tasks SET state='queued' WHERE id=%s",(tasks[1],))
        db.execute("SET LOCAL ROLE photo_scheduler")
        sealed = db.execute("SELECT photo.seal_download_archives(%s,%s)", (tenant,run)).fetchone()[0]
        db.execute("RESET ROLE")
        assert sealed is (defect != "direct_pending_producer")
        expected="ready" if defect=="direct_valid" else ("building" if defect=="direct_pending_producer" else "failed")
        assert db.execute("SELECT status FROM photo.download_jobs WHERE id=%s", (job,)).fetchone()[0] == expected
        if expected=="failed":
            assert db.execute("SELECT expires_at<=clock_timestamp() FROM photo.download_jobs WHERE id=%s",(job,)).fetchone()[0] is True
            failure=db.execute("SELECT failure FROM photo.download_jobs WHERE id=%s",(job,)).fetchone()[0]
            assert failure=={"code":"DOWNLOAD_ARCHIVE_FAILED","items":[{"ordinal":1,"asset_version_id":str(version),"code":"EXPORT_OUTPUT_UNAVAILABLE"}]}
            # Expiry does not erase diagnosis or override canonical-output retention.
            encoded=db.execute("SELECT artifact_id FROM photo.download_volumes WHERE download_job_id=%s AND volume_no=2",(job,)).fetchone()[0]
            db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(encoded,))
            # Canonical task outputs remain retained by the current run/result
            # retention roots; failed-job TTL alone cannot bypass those roots.
            claimed=db.execute("SELECT artifact_id FROM photo.claim_artifact_gc()").fetchone()
            assert claimed is None or claimed[0] != encoded
            assert db.execute("SELECT status FROM photo.download_jobs WHERE id=%s",(job,)).fetchone()[0]=="failed"
            assert db.execute("SELECT failure FROM photo.download_jobs WHERE id=%s",(job,)).fetchone()[0]==failure
        return
    if defect == "stale_attempt":
        db.execute("UPDATE photo.tasks SET attempt=2 WHERE id=%s", (tasks[1],))
    elif defect == "stale_fence":
        db.execute("UPDATE photo.tasks SET fencing_token=2 WHERE id=%s", (tasks[1],))
    elif defect in ("expired_archive", "expired_manifest"):
        role = "archive" if defect == "expired_archive" else "archive_manifest"
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE created_by_task_id=%s AND output_role=%s", (tasks[1], role))
    db.execute("SET LOCAL ROLE photo_scheduler")
    finalized = db.execute("SELECT photo.finalize_download_archives(%s,%s)",(tenant,run)).fetchone()[0]
    db.execute("RESET ROLE")
    if defect.startswith("postsign_"):
        assert finalized is True
        artifact = db.execute("SELECT artifact_id FROM photo.download_volumes WHERE download_job_id=%s AND volume_no=1", (job,)).fetchone()[0]
        coordinates = db.execute("SELECT object_key,object_version,sha256,bytes,media_type FROM photo.artifacts WHERE id=%s", (artifact,)).fetchone()
        token = uuid4().bytes + uuid4().bytes
        db.execute("""INSERT INTO photo.download_intents(tenant_id,principal_type,principal_id,artifact_id,policy_version,token_hash,expires_at,consumed_at,intent_kind)
            VALUES(%s,'user',%s,%s,1,%s,clock_timestamp()+interval '1 minute',clock_timestamp(),'volume')""", (tenant,user,artifact,token))
        def current():
            db.execute("SET LOCAL ROLE photo_api")
            try:
                return db.execute("SELECT photo.v30_delivery_download_intent_current(%s,'photo',%s,%s,%s,%s,%s)", (token,*coordinates)).fetchone()[0]
            finally:
                db.execute("RESET ROLE")
        assert current() is True
        field = "attempt" if defect == "postsign_stale_attempt" else "fencing_token"
        # Closed literal allowlist; no external SQL identifiers.
        if field == "attempt":
            db.execute("UPDATE photo.tasks SET attempt=2 WHERE id=%s", (tasks[0],))
        else:
            db.execute("UPDATE photo.tasks SET fencing_token=2 WHERE id=%s", (tasks[0],))
        assert current() is False
        return
    assert finalized is True
    assert db.execute("SELECT array_agg(status ORDER BY volume_no) FROM photo.download_volumes WHERE download_job_id=%s",(job,)).fetchone()[0]==["queued","queued"]
    assert db.execute("SELECT status FROM photo.download_jobs WHERE id=%s",(job,)).fetchone()[0]=="failed"
