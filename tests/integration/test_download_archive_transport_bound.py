from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb

@pytest.mark.parametrize("count", [512, 513])
def test_archive_plan_table_enforces_actual_transport_bound(db, count):
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
    refs=[{"artifact_id":str(source),"sha256":digest} for _ in range(count)]
    values=(uuid4(),tenant,run,Jsonb(refs),Jsonb([str(uuid4()) for _ in refs]),digest)
    def insert():
        db.execute("""INSERT INTO photo.download_archive_plans(id,tenant_id,run_id,volume_no,node_key,inputs,producer_tasks,params_sha,params)
            VALUES(%s,%s,%s,1,'archive.build/0001',%s,%s,%s,'{}')""",values)
    if count==512:
        insert()
        assert db.execute("SELECT jsonb_array_length(inputs) FROM photo.download_archive_plans WHERE id=%s",(values[0],)).fetchone()[0]==512
    else:
        from psycopg.errors import CheckViolation
        with pytest.raises(CheckViolation) as error:
            with db.transaction(): insert()
        assert error.value.diag.constraint_name=="download_archive_transport_inputs"
