"""Own minimal SQL fixtures for committed generation receipt reconciliation.

Synthetic ready object metadata and a pre-existing receipt are intentional:
these scenarios prove database replay/authorization boundaries only. They do
not prove GPU generation, object publication, or photographic quality.
"""
from hashlib import sha256
from uuid import uuid4
import rfc8785
from psycopg.types.json import Jsonb


def generation_replay_fixtures(db):
    fixtures = []
    for mode in ('draft_changed', 'runtime_unavailable'):
        actor, _ = db.execute(
            "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)",
            (f"generation-{uuid4()}@test.example",)).fetchone()
        tenant = db.execute("SELECT id FROM photo.tenants WHERE owner_user_id=%s", (actor,)).fetchone()[0]
        session = db.execute(
            "INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) "
            "VALUES(%s,%s,%s,'native',clock_timestamp()+interval '1 hour') RETURNING id",
            (actor, uuid4().bytes + uuid4().bytes, uuid4())).fetchone()[0]
        project = db.execute(
            "INSERT INTO photo.projects(tenant_id,name,visibility) VALUES(%s,'Generation replay','workspace') RETURNING id",
            (tenant,)).fetchone()[0]
        digest = sha256(f"generation-source-{uuid4()}".encode()).hexdigest()
        source = db.execute(
            "INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role) "
            "VALUES(%s,'photo',%s,'fixture-v1',%s,100,'application/json','ready','working_image') RETURNING id",
            (tenant, f"generation-replay/{uuid4()}", digest)).fetchone()[0]
        asset = db.execute(
            "INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) "
            "VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id", (tenant, project, source)).fetchone()[0]
        version = db.execute(
            "INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) "
            "VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id", (tenant, asset, source, digest)).fetchone()[0]
        mask_digest = sha256(f"generation-mask-{uuid4()}".encode()).hexdigest()
        mask = db.execute(
            "INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role) "
            "VALUES(%s,'photo',%s,'fixture-mask-v1',%s,100,'application/octet-stream','ready','mask') RETURNING id",
            (tenant, f"generation-replay-mask/{uuid4()}", mask_digest)).fetchone()[0]
        request = dict(asset_version_id=str(version), draft_revision=1,
            mask_ref=dict(artifact=dict(artifact_id=str(mask), sha256=mask_digest),
                canvas=dict(width=32, height=32, source_sha256=digest),
                transform={}, selector='fixture', confidence=1, protected=False),
            protected_refs=[], params=dict(prompt='fixture generation', seed=17), variant_count=1)
        db.execute(
            "INSERT INTO photo.editor_drafts(tenant_id,asset_version_id,user_id,revision,params,mask_refs) "
            "VALUES(%s,%s,%s,%s,'{}','[]')", (tenant, version, actor, 2 if mode == 'draft_changed' else 1))
        snapshot = db.execute(
            "INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count,artifact_id) "
            "VALUES(%s,'input','user',%s,%s,1,%s) RETURNING id", (tenant, actor, digest, source)).fetchone()[0]
        config = dict(schema_version=1, explicit_generation=True, asset_version_id=str(version),
            draft_revision=1, variant_count=1)
        run = db.execute(
            "INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) "
            "VALUES(%s,'preview','queued',%s,%s,%s,%s,true) RETURNING id",
            (tenant, snapshot, Jsonb(config), sha256(rfc8785.dumps(config)).hexdigest(), actor)).fetchone()[0]
        expected = dict(run_id=str(run), status='queued')
        key = str(uuid4())
        db.execute(
            "INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,"
            "response_status,response_body,resource_id,expires_at) "
            "VALUES(%s,'user',%s,'Generation.CreateCandidate',%s,%s,202,%s,%s,clock_timestamp()+interval '1 day')",
            (tenant, actor, key, sha256(rfc8785.dumps(request)).hexdigest(), Jsonb(expected), run))
        assert db.execute("SELECT count(*) FROM photo.worker_registrations WHERE tenant_id=%s", (tenant,)).fetchone()[0] == 0
        fixtures.append(dict(Tenant=str(tenant), Actor=str(actor), Session=str(session),
            Key=key, Mode=mode, Request=request, Expected=expected))
    return fixtures
