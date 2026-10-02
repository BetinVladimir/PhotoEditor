from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]


def test_bulk_chunk_processor_is_fenced_and_bounded():
    sql = (ROOT / "backend/db/migrations/00095_bulk_chunks.sql").read_text()
    for required in (
        "CREATE FUNCTION photo.enqueue_bulk_chunk()",
        "CREATE FUNCTION photo.process_bulk_chunk(p_bulk uuid,p_event uuid)",
        "SECURITY DEFINER SET search_path=pg_catalog,photo",
        "ORDER BY id FOR UPDATE",
        "c.revision<>b.expected_next_revision",
        "t.revision<>b.target_expected_next_revision",
        "ORDER BY si.ordinal LIMIT 500",
        "UPDATE photo.bulk_operations SET next_ordinal=next_value",
        "GRANT EXECUTE ON FUNCTION photo.enqueue_bulk_chunk() TO photo_scheduler",
        "GRANT EXECUTE ON FUNCTION photo.process_bulk_chunk(uuid,uuid) TO photo_worker",
    ):
        assert required in sql


def test_bulk_chunk_processor_records_item_outcomes_and_terminal_state():
    sql = (ROOT / "backend/db/migrations/00095_bulk_chunks.sql").read_text()
    assert "INSERT INTO photo.bulk_item_results" in sql
    assert "EXCEPTION WHEN unique_violation" in sql
    assert "WHEN OTHERS THEN INSERT INTO photo.bulk_item_results" in sql
    assert "WHEN failures=0 THEN 'completed' ELSE 'completed_with_errors'" in sql
    assert "UPDATE photo.runs SET status=CASE" in sql
    assert "'skipped','CANCELLED'" in sql
    assert "'failed','REVISION_CONFLICT'" in sql


def test_bulk_chunk_processor_validates_persisted_params_before_execution():
    sql = (ROOT / "backend/db/migrations/00095_bulk_chunks.sql").read_text()
    assert "params_valid:=CASE b.action" in sql
    assert "EXCEPTION WHEN OTHERS THEN params_valid:=false" in sql
    assert "IF NOT params_valid THEN" in sql
    assert "b.params=jsonb_build_object('target_collection_id',b.target_collection_id)" in sql


def test_bulk_command_is_durable_and_duplicate_safe():
    sql = (ROOT / "backend/db/migrations/00095_bulk_chunks.sql").read_text()
    assert "pending_event_id IS NULL" in sql
    assert "'photo.commands.v1','collection.mutate'" in sql
    assert "b.pending_event_id IS DISTINCT FROM p_event" in sql
    assert "SET pending_event_id=NULL" in sql
