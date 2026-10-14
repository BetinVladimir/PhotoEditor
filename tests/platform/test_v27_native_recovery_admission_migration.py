"""Regression: recovery compatibility must preserve existing security gates.

Source comparison only; PostgreSQL migration/runtime acceptance is separate.
"""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2] / 'backend/db/migrations'
ORIGINALS = (
    '00750_v27_native_admission_evidence.sql',
    '00751_v27_native_atomic_seal.sql',
    '00757_v27_face_admission_evidence.sql',
    '00758_v27_face_completed_evidence.sql',
    '00768_v27_skin_admission_evidence.sql',
    '00769_v27_skin_completed_evidence.sql',
    '00779_v27_native_defect_admission.sql',
    '00780_v27_native_defect_completed_evidence.sql',
)
GUARD = "\n OR (native ? 'recovery' AND (jsonb_typeof(native->'recovery') IS DISTINCT FROM 'object' OR native->'recovery'->'schema_version' IS DISTINCT FROM '1'::jsonb))"


def blocks(source):
    return [chunk.split('-- +goose StatementEnd')[0]
            for chunk in source.split('-- +goose StatementBegin')[1:]
            if '(native-ARRAY[' in chunk]


def test_recovery_compatibility_preserves_all_original_gates_and_acls():
    sql = (ROOT / '00798_v27_native_recovery_metadata_admission.sql').read_text()
    expected = [block for filename in ORIGINALS for block in blocks((ROOT / filename).read_text())]
    actual = blocks(sql)
    assert len(expected) == len(actual) == 8
    for old, new in zip(expected, actual):
        assert new.count(GUARD) == 1
        normalized = new.replace('CREATE OR REPLACE FUNCTION', 'CREATE FUNCTION', 1)
        normalized = normalized.replace("(native-ARRAY['recovery','schema_version'", "(native-ARRAY['schema_version'")
        normalized = normalized.replace(GUARD, '')
        assert normalized == old
    assert sql.count('-- +goose StatementBegin') == sql.count('-- +goose StatementEnd') == 9
    assert "RAISE EXCEPTION 'Forward-only migration'" in sql
