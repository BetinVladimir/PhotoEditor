"""Trusted V2 compiler draft shell and quota admission on a real database."""
from pathlib import Path
from uuid import uuid4

from psycopg.types.json import Jsonb

from test_database import tenant


ROOT = Path(__file__).resolve().parents[2]
SCAFFOLDS = ROOT / "backend/db/v2-scaffolds"


def test_draft_shell_requires_principal_snapshot_params_and_quota(db):
    for name in (
        "00196_run_kind.sql", "00196_schema.sql", "00196_guards.sql",
        "00196_project_guards.sql", "00196_artifact_guards.sql",
        "00196_draft_shell.sql", "00196_pending_replay_guard.sql",
        "00196_admission_idempotency.sql",
        "00196_draft_expiry.sql",
    ):
        db.execute((SCAFFOLDS / name).read_text())
    tid, actor = tenant(db, str(uuid4()) + "@example.test")
    db.execute("INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)", (tid,))
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) "
                         "VALUES(%s,'V2 draft') RETURNING id", (tid,)).fetchone()[0]
    snapshot = db.execute(
        "INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) "
        "VALUES(%s,'v2','user',%s,%s,0) RETURNING id",
        (tid, actor, "a" * 64),
    ).fetchone()[0]
    params = db.execute(
        "INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,"
        "object_version,verified_at) VALUES(%s,'test',%s,%s,2,'application/json','v1',"
        "clock_timestamp()) RETURNING id",
        (tid, str(uuid4()), "b" * 64),
    ).fetchone()[0]
    run, workflow_id, task = uuid4(), uuid4(), uuid4()
    rows = [{"id": str(task), "node_key": "preview/0", "operation": "asset.inspect",
             "operation_version": "1", "resource_class": "cpu",
             "params_artifact_id": str(params), "params_sha": "b" * 64,
             "input_digest": "c" * 64, "execution_profile": {"backend": "cpu"}}]
    query = "SELECT photo.create_v2_draft_shell(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)"
    args = (tid, run, workflow_id, project, snapshot, actor, "preview", "d" * 64, "e" * 64,
            Jsonb({"project_id": str(project)}), Jsonb(rows), Jsonb([]), 1024)
    db.execute("SET LOCAL ROLE photo_scheduler")
    assert not db.execute(query, args[:-1] + (11_000_000,)).fetchone()[0]
    assert not db.execute(query, args[:5] + (uuid4(),) + args[6:]).fetchone()[0]
    bad_rows = [{**rows[0], "params_sha": "f" * 64}]
    assert not db.execute(query, args[:10] + (Jsonb(bad_rows),) + args[11:]).fetchone()[0]
    assert db.execute(query, args).fetchone()[0]
    assert not db.execute(query, args).fetchone()[0]
    db.execute("RESET ROLE")
    assert db.execute("SELECT kind,status,graph_sealed FROM photo.runs WHERE id=%s",
                      (run,)).fetchone() == ("v2", "draft", False)
    assert db.execute("SELECT state FROM photo.tasks WHERE id=%s", (task,)).fetchone()[0] == "blocked"
    assert db.execute("SELECT bytes,state FROM photo.quota_reservations WHERE holder_id=%s",
                      (run,)).fetchone() == (1024, "reserved")
    assert db.execute("SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s",
                      (tid,)).fetchone()[0] == 1024
    extra_run = uuid4()
    extra_task = uuid4()
    extra_rows = [{**rows[0], "id": str(extra_task)}]
    bad_edges = [{"prerequisite_id": str(extra_task), "dependent_id": str(uuid4())}]
    db.execute("SET LOCAL ROLE photo_scheduler")
    assert not db.execute(query, (tid, extra_run, uuid4(), project, snapshot, actor, "preview", "d" * 64,
                                  "e" * 64, Jsonb({"project_id": str(project)}), Jsonb(extra_rows),
                                  Jsonb(bad_edges), 1024)).fetchone()[0]
    db.execute("RESET ROLE")
    assert db.execute("SELECT count(*) FROM photo.runs WHERE id=%s", (extra_run,)).fetchone()[0] == 0
    assert db.execute("SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s",
                      (tid,)).fetchone()[0] == 1024
    begin = "SELECT * FROM photo.begin_v2_admission(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)"
    pending_args = (tid, extra_run, uuid4(), project, snapshot, actor, "preview", "d" * 64,
                    "e" * 64, Jsonb({"project_id": str(project)}), Jsonb(extra_rows),
                    Jsonb([]), 1024, "draft-retry", "f" * 64)
    db.execute("SET LOCAL ROLE photo_scheduler")
    assert db.execute(begin, pending_args).fetchone() == ("created", extra_run)
    assert db.execute(begin, pending_args[:1] + (uuid4(),) + pending_args[2:]).fetchone() == ("pending", extra_run)
    drifted_rows = [{**extra_rows[0], "input_digest": "1" * 64}]
    assert db.execute(begin, pending_args[:10] + (Jsonb(drifted_rows),) +
                      pending_args[11:]).fetchone() == ("conflict", extra_run)
    assert db.execute(begin, pending_args[:8] + ("2" * 64,) +
                      pending_args[9:]).fetchone() == ("conflict", extra_run)
    assert db.execute(begin, pending_args[:12] + (2048,) +
                      pending_args[13:]).fetchone() == ("conflict", extra_run)
    assert db.execute(begin, pending_args).fetchone() == ("pending", extra_run)
    assert db.execute(begin, pending_args[:-1] + ("0" * 64,)).fetchone() == ("conflict", None)
    db.execute("RESET ROLE")
    db.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' "
               "WHERE tenant_id=%s AND holder_id=%s", (tid, extra_run))
    db.execute("SET LOCAL ROLE photo_maintenance")
    assert db.execute("SELECT photo.expire_one_v2_draft()").fetchone()[0]
    assert not db.execute("SELECT photo.expire_v2_draft(%s,%s)", (tid, extra_run)).fetchone()[0]
    db.execute("RESET ROLE")
    assert db.execute("SELECT status,cancel_requested FROM photo.runs WHERE id=%s",
                      (extra_run,)).fetchone() == ("cancelled", True)
    assert db.execute("SELECT state,terminal_reason FROM photo.tasks WHERE id=%s",
                      (extra_task,)).fetchone() == ("cancelled", "v2_draft_expired")
    assert db.execute("SELECT bytes,state FROM photo.quota_reservations WHERE holder_id=%s",
                      (extra_run,)).fetchone() == (0, "released")
    assert db.execute("SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s",
                      (tid,)).fetchone()[0] == 1024
    db.execute("SET LOCAL ROLE photo_scheduler")
    assert db.execute(begin, pending_args).fetchone() == ("expired", extra_run)
    db.execute("RESET ROLE")
    viewer = db.execute("INSERT INTO photo.users(email_normalized) VALUES(%s) RETURNING id",
                        (str(uuid4()) + "@example.test",)).fetchone()[0]
    db.execute("INSERT INTO photo.memberships(tenant_id,user_id,role) "
               "VALUES(%s,%s,'viewer')", (tid, viewer))
    viewer_snapshot = db.execute(
        "INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) "
        "VALUES(%s,'v2','user',%s,%s,0) RETURNING id",
        (tid, viewer, "a" * 64),
    ).fetchone()[0]
    db.execute("SET LOCAL ROLE photo_scheduler")
    assert not db.execute(query, (tid, uuid4(), uuid4(), project, viewer_snapshot, viewer, "preview",
                                  "d" * 64, "e" * 64, Jsonb({"project_id": str(project)}),
                                  Jsonb(rows), Jsonb([]), 1024)).fetchone()[0]
    db.execute("RESET ROLE")
    assert not db.execute("SELECT has_function_privilege('photo_worker',"
                          "'photo.create_v2_draft_shell(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,"
                          "jsonb,jsonb,jsonb,bigint)','EXECUTE')").fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_scheduler',"
                          "'photo.v2_pending_shell_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,"
                          "jsonb,jsonb,jsonb,bigint)','EXECUTE')").fetchone()[0]
