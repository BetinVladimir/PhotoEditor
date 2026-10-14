"""Authored V28 queue scope/replay fixtures; NOT RUN."""
from uuid import uuid4

import pytest
from psycopg.types.json import Jsonb
from test_database import tenant


@pytest.mark.parametrize("mode,expected", [
    ("new", "interactive"), ("historical", ""), ("interactive", "interactive"),
    ("batch", None), ("null", None), ("empty", None), ("foreign_actor", None),
])
def test_relight_queue_preserves_frozen_class_and_refuses_foreign_scope(db, mode, expected):
    tid, actor = tenant(db, uuid4().hex + "@relight-queue.test")
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Relight queue') RETURNING id", (tid,)).fetchone()[0]
    session = db.execute(
        "INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) "
        "VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') RETURNING id",
        (actor, uuid4().bytes),
    ).fetchone()[0]
    run = uuid4()
    if mode != "new":
        snapshot = db.execute(
            "INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) "
            "VALUES(%s,'v2','user',%s,%s,0) RETURNING id", (tid, actor, "b"*64),
        ).fetchone()[0]
        config = {"project_id": str(project), "v28_relight_request": {}}
        if mode in ("interactive", "batch"):
            config["queue_class"] = mode
        elif mode == "null":
            config["queue_class"] = None
        elif mode == "empty":
            config["queue_class"] = ""
        creator = actor
        if mode == "foreign_actor":
            _, creator = tenant(db, uuid4().hex + "@foreign-queue.test")
        db.execute(
            "INSERT INTO photo.runs(id,tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by) "
            "VALUES(%s,%s,'v2',%s,%s,%s,%s)",
            (run, tid, snapshot, Jsonb(config), "b"*64, creator),
        )
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tid),))
    db.execute("SET LOCAL ROLE photo_scheduler")
    query = "SELECT photo.v32_relight_initial_queue(%s,%s,%s,%s,%s)"
    args = (tid, project, actor, session, run)
    assert db.execute(query, args).fetchone() == (expected,)
    assert db.execute(query, args).fetchone() == (expected,)
    assert db.execute(query, (tid, project, actor, uuid4(), run)).fetchone() == (None,)
    assert db.execute(query, (uuid4(), project, actor, session, run)).fetchone() == (None,)
    if mode != "new":
        db.execute("RESET ROLE")
        assert db.execute("SELECT config_snapshot FROM photo.runs WHERE id=%s", (run,)).fetchone() == (config,)


@pytest.mark.parametrize("role,allowed", [
    ("photo_scheduler", True), ("photo_api", False), ("photo_worker", False),
    ("photo_gateway", False), ("photo_portal", False),
])
def test_relight_initial_queue_acl(db, role, allowed):
    assert db.execute(
        "SELECT has_function_privilege(%s,'photo.v32_relight_initial_queue(uuid,uuid,uuid,uuid,uuid)','EXECUTE')",
        (role,),
    ).fetchone() == (allowed,)
