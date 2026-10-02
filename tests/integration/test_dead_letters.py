from uuid import uuid4

from psycopg.types.json import Jsonb


def test_dead_letter_is_sanitized_and_replay_requeues_exact_task(db):
    digest = "a" * 64
    user, _ = db.execute("SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)", (f"{uuid4()}@test.example",)).fetchone()
    db.execute("INSERT INTO photo.system_admins(user_id) VALUES(%s)", (user,))
    tenant = db.execute("SELECT id FROM photo.tenants WHERE owner_user_id=%s", (user,)).fetchone()[0]
    artifact = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role) VALUES(%s,'photo',%s,'v1',%s,2,'application/json','ready','task_params') RETURNING id", (tenant, f"params/{uuid4()}", digest)).fetchone()[0]
    snapshot = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,source_revision,principal_type,principal_id,digest,item_count) VALUES(%s,'input',0,'user',%s,%s,0) RETURNING id", (tenant, user, digest)).fetchone()[0]
    run = db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'preview','failed',%s,'{}',%s,%s,true) RETURNING id", (tenant, snapshot, digest, user)).fetchone()[0]
    task = db.execute("INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,terminal_reason) VALUES(%s,%s,'dead/test','asset.verify','1','media','failed','[]',%s,%s,%s,'WORKER_LOST') RETURNING id", (tenant, run, artifact, digest, Jsonb({"backend": "cpu", "resource_estimate": {"ram_bytes": 1, "vram_bytes": 0, "scratch_bytes": 1, "output_bytes": 0, "pixel_count": 0}}))).fetchone()[0]
    event = uuid4()
    letter = db.execute("SELECT photo.record_dead_letter(%s,%s,'media','dead.media',%s,%s)", (task, event, "b" * 64, Jsonb({"schema_version": 1, "reason": "delivery_limit"}))).fetchone()[0]
    assert letter is not None
    listed = db.execute("SELECT id,tenant_id,task_id,event_id,state FROM photo.list_dead_letters(%s,NULL,NULL,10)", (user,)).fetchone()
    assert listed == (letter, tenant, task, event, "pending")
    accepted = db.execute("SELECT outcome,task_id,status FROM photo.replay_dead_letter(%s,%s,'retry-1',%s)", (user, letter, "c" * 64)).fetchone()
    assert accepted == ("accepted", task, "queued")
    assert db.execute("SELECT state,dispatch_command,terminal_reason FROM photo.tasks WHERE id=%s", (task,)).fetchone() == ("ready", None, None)
    assert db.execute("SELECT outcome,task_id,status FROM photo.replay_dead_letter(%s,%s,'retry-1',%s)", (user, letter, "c" * 64)).fetchone() == ("replayed", task, "queued")


def test_dead_letter_rejects_raw_secret_metadata_and_non_admin(db):
    user, _ = db.execute("SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)", (f"{uuid4()}@test.example",)).fetchone()
    assert db.execute("SELECT photo.record_dead_letter(NULL,NULL,'unknown','dead.unknown',%s,%s)", ("d" * 64, Jsonb({"body": "secret"}))).fetchone()[0] is None
    assert db.execute("SELECT count(*) FROM photo.list_dead_letters(%s,NULL,NULL,10)", (user,)).fetchone()[0] == 0
    assert db.execute("SELECT outcome FROM photo.replay_dead_letter(%s,%s,'key',%s)", (user, uuid4(), "e" * 64)).fetchone()[0] == "forbidden"


def test_system_admin_authorization_rechecks_live_session_through_api_role(db):
    user, _ = db.execute("SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)", (f"{uuid4()}@test.example",)).fetchone()
    db.execute("INSERT INTO photo.system_admins(user_id) VALUES(%s)", (user,))
    session = uuid4()
    db.execute("INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,%s,%s,'native',clock_timestamp()+interval '1 hour')", (session, user, b"r" * 32, uuid4()))
    db.execute("SET LOCAL ROLE photo_api")
    assert db.execute("SELECT photo.authorize_system_admin(%s,%s)", (user, session)).fetchone()[0]
    db.execute("RESET ROLE")
    db.execute("UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s", (session,))
    db.execute("SET LOCAL ROLE photo_api")
    assert not db.execute("SELECT photo.authorize_system_admin(%s,%s)", (user, session)).fetchone()[0]
    db.execute("RESET ROLE")
