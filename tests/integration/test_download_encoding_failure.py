from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb

@pytest.mark.parametrize("scenario", ["terminal", "pending", "cancelled_job", "wrong_tenant", "missing_plan", "missing_snapshot_item"])
def test_download_encoding_failure_is_terminal_only_after_complete_inventory(db, scenario):
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
    db.execute("UPDATE photo.download_build_state SET phase='encoding' WHERE tenant_id=%s AND run_id=%s",(tenant,run))
    db.execute("UPDATE photo.runs SET status='running',graph_sealed=true WHERE id=%s",(run,))
    db.execute("UPDATE photo.download_jobs SET status='building' WHERE id=%s",(job,))

    params=db.execute("""INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role)
        VALUES(%s,'photo',%s,'params-v1',%s,2,'application/json','params') RETURNING id""",(tenant,f"params/{uuid4()}",digest)).fetchone()[0]

    tasks=[uuid4(),uuid4()]
    inputs=Jsonb([{"artifact_id":str(source),"sha256":digest},{"artifact_id":str(params),"sha256":digest}])
    for ordinal,task in enumerate(tasks):
        db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,%s,%s,%s,%s,'{}')",(tenant,snapshot,ordinal,version,source,digest))
        key=f"export.encode/{ordinal:016d}"
        db.execute("""INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
            VALUES(%s,%s,%s,%s,'export.encode','1','cpu','blocked',%s,%s,%s,%s)""",(task,tenant,run,key,inputs,params,digest,Jsonb({"backend":"cpu"})))
        db.execute("""INSERT INTO photo.download_export_plans(id,tenant_id,run_id,ordinal,node_key,inputs,params_sha,params)
            VALUES(%s,%s,%s,%s,%s,%s,%s,'{}')""",(task,tenant,run,ordinal,key,inputs,digest))
        db.execute("UPDATE photo.tasks SET state=%s WHERE id=%s",("ready" if scenario=="pending" and ordinal==1 else "failed",task))
    if scenario=="cancelled_job":db.execute("UPDATE photo.download_jobs SET cancel_requested=true WHERE id=%s",(job,))
    if scenario=="missing_plan":db.execute("DELETE FROM photo.download_export_plans WHERE id=%s",(tasks[1],))
    if scenario=="missing_snapshot_item":db.execute("DELETE FROM photo.snapshot_items WHERE tenant_id=%s AND snapshot_id=%s AND ordinal=1",(tenant,snapshot))
    actual_tenant=uuid4() if scenario=="wrong_tenant" else tenant
    changed=db.execute("SELECT photo.fail_download_encoding(%s,%s)",(actual_tenant,run)).fetchone()[0]
    status,failure=db.execute("SELECT status,failure FROM photo.download_jobs WHERE id=%s",(job,)).fetchone()
    if scenario!="terminal":
        assert changed is False and status=="building" and failure is None
        return
    assert changed is True and status=="failed"
    # Public progress keeps the frozen selection total even before packaging.
    done,total=db.execute("""SELECT count(*) FILTER(WHERE t.state IN ('succeeded','failed','cancelled','skipped')),s.item_count
        FROM photo.download_jobs j JOIN photo.snapshots s ON s.tenant_id=j.tenant_id AND s.id=j.snapshot_id
        JOIN photo.tasks t ON t.tenant_id=j.tenant_id AND t.run_id=j.run_id AND t.operation='export.encode'
        WHERE j.id=%s GROUP BY s.item_count""",(job,)).fetchone()
    assert (done,total)==(2,2)

    assert failure=={"code":"DOWNLOAD_EXPORT_FAILED","items":[{"ordinal":i,"asset_version_id":str(version),"code":"EXPORT_FAILED"} for i in range(2)]}
    revision=db.execute("SELECT revision FROM photo.runs WHERE id=%s",(run,)).fetchone()[0]
    assert db.execute("SELECT photo.fail_download_encoding(%s,%s)",(tenant,run)).fetchone()[0] is True
    assert db.execute("SELECT revision FROM photo.runs WHERE id=%s",(run,)).fetchone()[0]==revision
    assert db.execute("SELECT count(*) FROM photo.download_volumes WHERE download_job_id=%s",(job,)).fetchone()[0]==0
    assert db.execute("SELECT has_function_privilege('photo_scheduler','photo.fail_download_encoding(uuid,uuid)','EXECUTE'),has_function_privilege('photo_api','photo.fail_download_encoding(uuid,uuid)','EXECUTE'),has_function_privilege('photo_portal','photo.fail_download_encoding(uuid,uuid)','EXECUTE')").fetchone()==(True,False,False)
