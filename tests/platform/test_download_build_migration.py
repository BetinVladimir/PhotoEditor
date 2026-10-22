import json
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]


def test_download_build_migration_has_fenced_complete_barrier():
    sql = (ROOT / "backend/db/migrations/00090_download_export_build.sql").read_text()
    for required in (
        "CREATE TABLE photo.download_export_plans",
        "CREATE FUNCTION photo.claim_download_build()",
        "CREATE FUNCTION photo.renew_download_build",
        "CREATE FUNCTION photo.release_download_build",
        "CREATE FUNCTION photo.register_download_export_plan",
        "CREATE OR REPLACE FUNCTION photo.reserve_backend_document",
        "CREATE OR REPLACE FUNCTION photo.complete_backend_document",
        "CREATE OR REPLACE FUNCTION photo.materialize_download_export_task",
        "CREATE FUNCTION photo.seal_download_exports",
        "UPDATE photo.tasks SET state='ready'",
    ):
        assert required in sql
    assert "FOR UPDATE OF rr SKIP LOCKED" in sql
    assert "r.graph_sealed" in sql


def test_working_image_producer_contract_uses_forward_policy_authority():
    registry = json.loads((ROOT / "backend/internal/workflow/operations.json").read_text())
    expected = {
        op["name"]
        for op in registry["operations"]
        if op["version"] == "1" and op["output_type"] == "WorkingImage"
    }
    sql = (ROOT / "backend/db/migrations/00090_download_export_build.sql").read_text()
    fragment = sql.split("t.operation IN (", 1)[1].split(")", 1)[0]
    actual = {part.strip().strip("'") for part in fragment.split(",")}
    # Immutable historical list is replaced by the exact forward policy patch.
    assert actual <= expected
    current=(ROOT / "backend/db/migrations/02476_download_lifecycle_source_closure.sql").read_text()
    assert "anchor:=$a$t.operation IN (" + fragment + ")$a$;" in current
    assert "policy.operation=t.operation AND policy.version=t.operation_version" in current
    assert "policy.role='working_image' AND policy.media_types ? 'application/json'" in current
    assert "policy.role='working_rgb' AND policy.media_types ? 'application/octet-stream'" in current
    assert "RAISE EXCEPTION 'DOWNLOAD_MASTER_PRODUCER_SOURCE'" in current
    assert "source:=replace(source,anchor,replacement)" in current


def test_download_packaging_and_archive_barriers_are_durable():
    sql91 = (ROOT / "backend/db/migrations/00091_download_packaging_barrier.sql").read_text()
    sql92 = (ROOT / "backend/db/migrations/00092_download_archive_build.sql").read_text()
    sql93 = (ROOT / "backend/db/migrations/00093_finalize_download_archives.sql").read_text()
    for required in (
        "CREATE TABLE photo.download_build_state",
        "CREATE FUNCTION photo.download_encode_outcomes",
        "CREATE FUNCTION photo.begin_download_packaging",
        "UPDATE photo.runs SET graph_sealed=false",
    ):
        assert required in sql91
    for required in (
        "CREATE TABLE photo.download_archive_plans",
        "CREATE FUNCTION photo.download_archive_context",
        "CREATE FUNCTION photo.register_download_archive_plan",
        "CREATE FUNCTION photo.materialize_download_archive_task",
        "CREATE FUNCTION photo.seal_download_archives",
        "expected_params IS DISTINCT FROM p_params",
    ):
        assert required in sql92
    assert "CREATE FUNCTION photo.finalize_download_archives" in sql93
    assert "output_role='archive_manifest'" in sql93
    assert "status='ready',expires_at=until_time" in sql93


def test_portal_download_intents_are_single_use_and_policy_bound():
    sql = (ROOT / "backend/db/migrations/00094_portal_download_intents.sql").read_text()
    assert "CREATE FUNCTION photo.create_portal_download_intent" in sql
    assert "CREATE FUNCTION photo.redeem_portal_download_intent" in sql
    assert "FOR UPDATE" in sql
    assert "consumed_at IS NOT NULL" in sql
    assert "UPDATE photo.download_intents SET consumed_at=clock_timestamp()" in sql
    assert "l.permissions->'download'" in sql
