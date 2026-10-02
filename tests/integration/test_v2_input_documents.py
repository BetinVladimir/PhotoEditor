"""V2 transport documents use fenced backend publication and quota accounting."""
from pathlib import Path
from uuid import uuid4

from psycopg.types.json import Jsonb

from test_admission import ready_task


ROOT = Path(__file__).resolve().parents[2]
SCAFFOLDS = ROOT / "backend/db/v2-scaffolds"


def test_v2_input_document_reserve_complete_replay_and_scope(db):
    for name in ("00196_run_kind.sql", "00196_schema.sql", "00196_input_documents.sql"):
        db.execute((SCAFFOLDS / name).read_text())
    item = ready_task(db)
    project = db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Input docs') RETURNING id",
                         (item["tenant"],)).fetchone()[0]
    snapshot = db.execute("SELECT input_snapshot_id FROM photo.runs WHERE id=%s",
                          (item["run"],)).fetchone()[0]
    db.execute("UPDATE photo.runs SET kind='v2',failure_policy='fail_fast',config_snapshot=%s WHERE id=%s",
               (Jsonb({"project_id": str(project)}), item["run"]))
    db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,"
               "workflow_type,renderer_version,policy_sha) "
               "VALUES(%s,%s,%s,%s,'preview','v2.2',%s)",
               (item["tenant"], project, item["run"], snapshot, "f" * 64))

    reserve = "SELECT * FROM photo.reserve_v2_input_document(%s,%s,%s,%s,%s,%s,%s)"
    complete = "SELECT photo.complete_v2_input_document(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)"
    args = (item["tenant"], item["run"], item["task"], "v2_input_chunk_0", "a" * 64, 16, "test")
    db.execute("SET LOCAL ROLE photo_scheduler")
    created = db.execute(reserve, args).fetchone()
    assert created[0] == "created" and not created[3]
    assert db.execute(reserve, args).fetchone()[0] == "resumed"
    current = db.execute(reserve, args).fetchone()
    assert current[0] == "resumed" and current[1] == created[1]
    assert not db.execute(complete, (*args[:4], current[1], created[2], *args[4:], "v1")).fetchone()[0]
    assert db.execute(complete, (*args[:4], current[1], current[2], *args[4:], "v1")).fetchone()[0]
    assert db.execute(reserve, args).fetchone()[0] == "replayed"
    assert db.execute(reserve, (*args[:4], "b" * 64, 16, "test")).fetchone()[0] == "conflict"
    assert db.execute(reserve, (item["tenant"], item["run"], item["task"],
                                "v2_input_chunk_1954", "a" * 64, 16, "test")).fetchone()[0] == "forbidden"
    assert db.execute(reserve, (item["tenant"], item["run"], uuid4(),
                                "v2_input_manifest", "a" * 64, 16, "test")).fetchone()[0] == "forbidden"
    db.execute("RESET ROLE")
    assert db.execute("SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s",
                      (item["tenant"],)).fetchone() == (16, 0)
    assert db.execute("SELECT output_role,object_version FROM photo.artifacts WHERE id=%s",
                      (created[1],)).fetchone() == ("v2_input_chunk_0", "v1")
    db.execute("UPDATE photo.runs SET cancel_requested=true WHERE id=%s", (item["run"],))
    db.execute("SET LOCAL ROLE photo_scheduler")
    assert db.execute(reserve, (item["tenant"], item["run"], item["task"],
                                "v2_input_manifest", "b" * 64, 16, "test")).fetchone()[0] == "forbidden"
