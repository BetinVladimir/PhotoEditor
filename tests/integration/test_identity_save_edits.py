from uuid import uuid4

from psycopg.types.json import Jsonb


def test_identity_save_edits_claims_and_exposes_nullable_style(db):
    digest = "a" * 64
    user, _ = db.execute("SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)",
                         (f"{uuid4()}@test.example",)).fetchone()
    tenant = db.execute("SELECT id FROM photo.tenants WHERE owner_user_id=%s", (user,)).fetchone()[0]
    project = db.execute("INSERT INTO photo.projects(tenant_id,name,visibility) VALUES(%s,'Identity','workspace') RETURNING id",
                         (tenant,)).fetchone()[0]
    artifact = db.execute("""INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role)
        VALUES(%s,'photo',%s,'v1',%s,1024,'application/json','working_image') RETURNING id""",
        (tenant, f"masters/{uuid4()}", digest)).fetchone()[0]
    asset = db.execute("""INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance)
        VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id""", (tenant, project, artifact)).fetchone()[0]
    seed_snapshot = db.execute("""INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count)
        VALUES(%s,'input','user',%s,%s,0) RETURNING id""", (tenant, user, digest)).fetchone()[0]
    seed_run = db.execute("""INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed)
        VALUES(%s,'ingest','running',%s,'{}',%s,%s,true) RETURNING id""",
        (tenant, seed_snapshot, digest, user)).fetchone()[0]
    version = db.execute("""INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,created_by_run_id,content_hash,metadata,review_state)
        VALUES(%s,%s,%s,%s,%s,'{}','accepted') RETURNING id""",
        (tenant, asset, artifact, seed_run, digest)).fetchone()[0]
    source = db.execute("INSERT INTO photo.collections(tenant_id,project_id,name,state) VALUES(%s,%s,'Source','draft') RETURNING id",
                        (tenant, project)).fetchone()[0]
    item = db.execute("""INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position)
        VALUES(%s,%s,%s,%s,1) RETURNING id""", (tenant, source, asset, version)).fetchone()[0]
    snapshot = db.execute("""INSERT INTO photo.snapshots(tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count)
        VALUES(%s,'selection',%s,1,'user',%s,%s,1) RETURNING id""", (tenant, source, user, digest)).fetchone()[0]
    db.execute("""INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,asset_version_id,artifact_id,sha256,payload)
        VALUES(%s,%s,0,%s,%s,%s,%s,%s)""", (tenant, snapshot, item, version, artifact, digest, Jsonb({"position": 1})))
    output = db.execute("""INSERT INTO photo.collections(tenant_id,project_id,name,state,source_collection_id,source_revision)
        VALUES(%s,%s,'Edited','draft',%s,1) RETURNING id""", (tenant, project, source)).fetchone()[0]
    config = {"identity_style": True, "style_config": {"mode": "fixed"}, "person_mask_required": False}
    run = db.execute("""INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,output_collection_id,style_version_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
        VALUES(%s,'apply','queued',%s,%s,NULL,%s,%s,%s,'continue_items',false) RETURNING id""",
        (tenant, snapshot, output, Jsonb(config), "b" * 64, user)).fetchone()[0]
    db.execute("""INSERT INTO photo.apply_builds(tenant_id,run_id,source_collection_id,style_version_id,item_count,phase,graph_version)
        VALUES(%s,%s,%s,NULL,1,'resolve',4)""", (tenant, run, source))
    assert db.execute("SELECT graph_version FROM photo.apply_builds WHERE run_id=%s",(run,)).fetchone()[0]==4
    claim = db.execute("SELECT tenant_id,run_id,style_version_id,lease_token FROM photo.claim_apply_build()").fetchone()
    assert claim[0] == tenant and claim[1] == run and claim[2] is None
    row = db.execute("SELECT item_id,style_artifact_id,style_sha256,style_bytes FROM photo.apply_inputs(%s,%s,%s)",
                     (tenant, run, claim[3])).fetchone()
    assert row == (item, None, None, None)
