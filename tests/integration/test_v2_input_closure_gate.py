"""V2 dispatch cannot start until exact task input rows and transport exist."""
from pathlib import Path
from uuid import uuid4

from psycopg.types.json import Jsonb

from test_database import tenant


ROOT = Path(__file__).resolve().parents[2]
SCAFFOLDS = ROOT / "backend/db/v2-scaffolds"
SCHEMA_SHA = "341984c53d787175fb5a9eec4e5d55ab74e9cd293075add8965d6064aba2e815"


def test_v2_input_closure_gate_requires_exact_sealed_inventory(db):
    for name in ("00196_run_kind.sql", "00196_schema.sql", "00196_input_closure_gate.sql"):
        db.execute((SCAFFOLDS / name).read_text())
    tid, user = tenant(db, str(uuid4()) + "@example.test")
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Gate') RETURNING id",
                         (tid,)).fetchone()[0]
    snapshot = db.execute(
        "INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) "
        "VALUES(%s,'v2','user',%s,%s,0) RETURNING id",
        (tid, user, "a" * 64),
    ).fetchone()[0]
    run = db.execute(
        "INSERT INTO photo.runs(tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) "
        "VALUES(%s,'v2',%s,%s,%s,%s,'fail_fast') RETURNING id",
        (tid, snapshot, Jsonb({"project_id": str(project)}), "b" * 64, user),
    ).fetchone()[0]
    db.execute(
        "INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,"
        "workflow_type,renderer_version,policy_sha) "
        "VALUES(%s,%s,%s,%s,'preview','v2.2',%s)",
        (tid, project, run, snapshot, "c" * 64),
    )

    def artifact(digest):
        return db.execute(
            "INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,"
            "object_version,verified_at) VALUES(%s,'test',%s,%s,2,'application/json',"
            "'v1',clock_timestamp()) RETURNING id",
            (tid, str(uuid4()), digest),
        ).fetchone()[0]

    entry = artifact("d" * 64)
    chunk = artifact("e" * 64)
    manifest = artifact("f" * 64)
    manifest_ref = {"artifact_id": str(manifest), "sha256": "f" * 64}
    profile = {"renderer_version": "v2.2", "contracts_sha": SCHEMA_SHA,
               "v2_input_manifest": manifest_ref, "v2_entry_count": 1,
               "v2_entries_sha": "a" * 64}
    task = db.execute(
        "INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,"
        "resource_class,inputs,params_artifact_id,input_digest,execution_profile) "
        "VALUES(%s,%s,'preview/0','editor.preview','1','cpu',%s,%s,%s,%s) RETURNING id",
        (tid, run, Jsonb([manifest_ref]), entry, "a" * 64, Jsonb(profile)),
    ).fetchone()[0]
    ready = "SELECT photo.v2_input_closure_ready(%s)"
    assert not db.execute(ready, (task,)).fetchone()[0]

    def row(artifact_id, digest, contract, ordinal):
        db.execute(
            "INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,"
            "sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,%s,%s)",
            (tid, project, task, artifact_id, digest, contract, ordinal),
        )

    row(entry, "d" * 64, "WorkingImage", 0)
    assert not db.execute(ready, (task,)).fetchone()[0]
    row(chunk, "e" * 64, "InputClosureChunk", 1_000_000)
    assert not db.execute(ready, (task,)).fetchone()[0]
    row(manifest, "f" * 64, "InputClosureManifest", 1_000_001)
    assert db.execute(ready, (task,)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET execution_profile=%s WHERE id=%s",
               (Jsonb({**profile, "v2_entry_count": 2}), task))
    assert not db.execute(ready, (task,)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET execution_profile=%s WHERE id=%s", (Jsonb(profile), task))
    db.execute("UPDATE photo.tasks SET inputs=%s WHERE id=%s",
               (Jsonb([manifest_ref, {"artifact_id": str(entry), "sha256": "1" * 64}]), task))
    assert not db.execute(ready, (task,)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET inputs=%s WHERE id=%s", (Jsonb([manifest_ref]), task))
    db.execute("UPDATE photo.tasks SET execution_profile=%s WHERE id=%s",
               (Jsonb({**profile, "contracts_sha": "0" * 64}), task))
    assert not db.execute(ready, (task,)).fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_worker',"
                          "'photo.v2_input_closure_ready(uuid)','EXECUTE')").fetchone()[0]
