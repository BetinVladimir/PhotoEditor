from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]


def test_style_aggregate_output_policies_are_bounded():
    sql=(ROOT/'backend/db/migrations/00116_style_aggregate_output_policies.sql').read_text()
    assert "('style.aggregate','1','style_candidate','[\"application/json\"]',100000000,1)" in sql
    assert "('style.aggregate','1','aggregate_report','[\"application/json\"]',1048576,1)" in sql


def test_style_aggregate_is_registered_in_cpu_runtime():
    source=(ROOT/'workers/photo_worker/runtime/service.py').read_text()
    assert 'aggregate_operations(artifacts)' in source
