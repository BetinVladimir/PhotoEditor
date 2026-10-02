"""Atomic V2 task input admission against real PostgreSQL role and row guards."""
from pathlib import Path
from uuid import uuid4

import pytest
from psycopg import errors
from psycopg.types.json import Jsonb

from test_database import tenant


ROOT = Path(__file__).resolve().parents[2]
SCAFFOLDS = ROOT / "backend/db/v2-scaffolds"


def test_register_and_seal_task_inputs_is_atomic_and_immutable(db):
    for name in (
        "00196_run_kind.sql", "00196_schema.sql", "00196_guards.sql",
        "00196_project_guards.sql", "00196_artifact_guards.sql",
        "00196_input_documents.sql", "00196_input_closure_gate.sql",
        "00196_task_input_registration.sql", "00196_draft_shell.sql",
        "00196_admission_idempotency.sql", "00196_graph_seal.sql",
    ):
        db.execute((SCAFFOLDS / name).read_text())
    _exercise_task_input_registration(db)


def _exercise_task_input_registration(db):
    tid, user = tenant(db, str(uuid4()) + "@example.test")
    db.execute("INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)", (tid,))
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Admission') RETURNING id",
                         (tid,)).fetchone()[0]
    snapshot = db.execute(
        "INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) "
        "VALUES(%s,'v2','user',%s,%s,0) RETURNING id",
        (tid, user, "a" * 64),
    ).fetchone()[0]
    run = db.execute(
        "INSERT INTO photo.runs(tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,"
        "created_by,failure_policy) VALUES(%s,'v2',%s,%s,%s,%s,'fail_fast') RETURNING id",
        (tid, snapshot, Jsonb({"project_id": str(project)}), "b" * 64, user),
    ).fetchone()[0]
    db.execute(
        "INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,"
        "workflow_type,renderer_version,policy_sha) "
        "VALUES(%s,%s,%s,%s,'preview','v2.2',%s)",
        (tid, project, run, snapshot, "c" * 64),
    )
    entry = db.execute(
        "INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,"
        "object_version,verified_at) VALUES(%s,'test',%s,%s,2,'application/json',"
        "'v1',clock_timestamp()) RETURNING id",
        (tid, str(uuid4()), "d" * 64),
    ).fetchone()[0]
    task = db.execute(
        "INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,"
        "resource_class,inputs,params_artifact_id,input_digest,execution_profile) "
        "VALUES(%s,%s,'preview/0','asset.inspect','1','cpu','[]',%s,%s,%s) RETURNING id",
        (tid, run, entry, "e" * 64, Jsonb({"backend": "cpu"})),
    ).fetchone()[0]

    def publish(target, purpose, digest):
        reserve = "SELECT * FROM photo.reserve_v2_input_document(%s,%s,%s,%s,%s,16,'test')"
        created = db.execute(reserve, (tid, run, target, purpose, digest)).fetchone()
        assert created[0] == "created"
        assert db.execute("SELECT photo.complete_v2_input_document(%s,%s,%s,%s,%s,%s,%s,16,'test','v1')",
                          (tid, run, target, purpose, created[1], created[2], digest)).fetchone()[0]
        return {"artifact_id": str(created[1]), "sha256": digest}

    db.execute("SET LOCAL ROLE photo_scheduler")
    chunk = publish(task, "v2_input_chunk_0", "f" * 64)
    manifest = publish(task, "v2_input_manifest", "1" * 64)
    row = {"ref": {"artifact_id": str(entry), "sha256": "d" * 64}, "contract": "Params"}
    register = "SELECT photo.register_v2_task_input_batch(%s,%s,%s,%s,%s)"
    with pytest.raises(errors.CheckViolation, match="V2_INPUT_BATCH_GAP"), db.transaction():
        db.execute(register, (tid, run, task, 1, Jsonb([row])))
    assert db.execute(register, (tid, run, task, 0, Jsonb([row]))).fetchone()[0] == 1
    seal = "SELECT photo.seal_v2_task_inputs(%s,%s,%s,%s,%s,%s,%s,%s)"
    with pytest.raises(errors.CheckViolation, match="V2_DIRECT_ROOT_INVALID"), db.transaction():
        db.execute(seal, (tid, run, task, Jsonb(manifest), Jsonb([chunk]), 1, "a" * 64,
                          Jsonb([{"artifact_id": str(uuid4()), "sha256": "a" * 64}])))
    assert db.execute(seal, (tid, run, task, Jsonb(manifest), Jsonb([chunk]), 1,
                            "a" * 64, Jsonb([]))).fetchone()[0]
    db.execute("RESET ROLE")
    assert db.execute("SELECT photo.v2_input_closure_ready(%s)", (task,)).fetchone()[0]
    assert db.execute("SELECT count(*) FROM photo.v2_task_inputs WHERE task_id=%s", (task,)).fetchone()[0] == 3
    assert db.execute("SELECT inputs->0->>'artifact_id' FROM photo.tasks WHERE id=%s",
                      (task,)).fetchone()[0] == manifest["artifact_id"]
    # Graph admission is a separate, quota-backed CAS after every task's closure
    # has been sealed. A legacy operation is denied until explicitly enabled.
    db.execute("INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,"
               "holder_type,holder_id,bytes,files,expires_at) "
               "VALUES(%s,'user',%s,'run',%s,1024,1,clock_timestamp()+interval '1 hour')",
               (tid, user, run))
    db.execute("UPDATE photo.tenant_usage SET reserved_bytes=1024 WHERE tenant_id=%s", (tid,))
    db.execute("INSERT INTO photo.operation_output_policies(operation,version,role,"
               "media_types,max_bytes,max_count) VALUES('asset.inspect','1','document',"
               "'[\"application/json\"]',8388608,1)")
    db.execute("INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,"
               "route,key,request_hash,resource_id,expires_at) "
               "VALUES(%s,'user',%s,'V2.Admit','seal-retry',%s,%s,clock_timestamp()+interval '1 day')",
               (tid, user, "9" * 64, run))
    db.execute("SET LOCAL ROLE photo_scheduler")
    assert not db.execute("SELECT photo.seal_v2_graph(%s,%s)", (tid, run)).fetchone()[0]
    db.execute("RESET ROLE")
    db.execute("UPDATE photo.operation_registry SET v2_enabled=true "
               "WHERE operation='asset.inspect' AND version='1'")
    second = db.execute(
        "INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,"
        "resource_class,inputs,params_artifact_id,input_digest,execution_profile) "
        "VALUES(%s,%s,'preview/1','asset.inspect','1','cpu','[]',%s,%s,%s) RETURNING id",
        (tid, run, entry, "2" * 64, Jsonb({"backend": "cpu"})),
    ).fetchone()[0]
    db.execute("SET LOCAL ROLE photo_scheduler")
    assert not db.execute("SELECT photo.seal_v2_graph(%s,%s)", (tid, run)).fetchone()[0]
    second_chunk = publish(second, "v2_input_chunk_0", "3" * 64)
    second_manifest = publish(second, "v2_input_manifest", "4" * 64)
    assert db.execute(register, (tid, run, second, 0, Jsonb([row]))).fetchone()[0] == 1
    assert db.execute(seal, (tid, run, second, Jsonb(second_manifest), Jsonb([second_chunk]),
                            1, "a" * 64, Jsonb([]))).fetchone()[0]
    db.execute("RESET ROLE")
    db.execute("INSERT INTO photo.task_edges(tenant_id,run_id,prerequisite_id,dependent_id) "
               "VALUES(%s,%s,%s,%s),(%s,%s,%s,%s)",
               (tid, run, task, second, tid, run, second, task))
    db.execute("SET LOCAL ROLE photo_scheduler")
    assert not db.execute("SELECT photo.seal_v2_graph(%s,%s)", (tid, run)).fetchone()[0]
    db.execute("RESET ROLE")
    db.execute("DELETE FROM photo.task_edges WHERE tenant_id=%s AND run_id=%s", (tid, run))
    db.execute("SET LOCAL ROLE photo_scheduler")
    assert db.execute("SELECT photo.seal_v2_graph(%s,%s)", (tid, run)).fetchone()[0]
    assert db.execute("SELECT photo.seal_v2_graph(%s,%s)", (tid, run)).fetchone()[0]
    db.execute("RESET ROLE")
    assert db.execute("SELECT status,graph_sealed FROM photo.runs WHERE id=%s", (run,)).fetchone() == ("queued", True)
    assert db.execute("SELECT state FROM photo.tasks WHERE id=%s", (task,)).fetchone()[0] == "ready"
    assert db.execute("SELECT state FROM photo.tasks WHERE id=%s", (second,)).fetchone()[0] == "ready"
    assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s "
                      "AND type='v2.run.queued'", (tid, run)).fetchone()[0] == 1
    assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s "
                      "AND routing_key='v2.run.queued' AND payload->>'run_id'=%s",
                      (tid, str(run))).fetchone()[0] == 1
    recorded = db.execute("SELECT response_status,response_body FROM photo.idempotency_records "
                          "WHERE tenant_id=%s AND resource_id=%s", (tid, run)).fetchone()
    workflow_id = db.execute("SELECT id FROM photo.v2_workflows WHERE run_id=%s", (run,)).fetchone()[0]
    assert recorded == (202, {"run_id": str(run), "workflow_id": str(workflow_id), "state": "queued"})
    db.execute("SET LOCAL ROLE photo_scheduler")
    assert db.execute("SELECT photo.v2_admission_ticket(%s,%s,'seal-retry',%s)",
                      (tid, user, "9" * 64)).fetchone()[0] == recorded[1]
    assert db.execute("SELECT photo.v2_admission_ticket(%s,%s,'seal-retry',%s)",
                      (tid, user, "8" * 64)).fetchone()[0] is None
    db.execute("RESET ROLE")
    with pytest.raises(errors.ObjectNotInPrerequisiteState, match="V2_TASK_INPUTS_IMMUTABLE"), db.transaction():
        db.execute("UPDATE photo.tasks SET inputs='[]' WHERE id=%s", (task,))
    assert not db.execute("SELECT has_function_privilege('photo_worker',"
                          "'photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb)',"
                          "'EXECUTE')").fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_worker',"
                          "'photo.seal_v2_graph(uuid,uuid)','EXECUTE')").fetchone()[0]
    assert not db.execute("SELECT has_table_privilege('photo_scheduler',"
                          "'photo.v2_task_inputs','INSERT')").fetchone()[0]
