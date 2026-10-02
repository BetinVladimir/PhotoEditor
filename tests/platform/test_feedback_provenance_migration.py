from pathlib import Path


SQL = (Path(__file__).resolve().parents[2] / "backend/db/migrations/00158_feedback_pair_provenance.sql").read_text()


def test_feedback_pair_provenance_is_explicit_and_bounded():
    assert "source_kind text NOT NULL DEFAULT 'explicit_pair'" in SQL
    assert "'feedback_edit'" in SQL
    assert "'user_added_generated'" in SQL

