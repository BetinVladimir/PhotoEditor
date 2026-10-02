from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]

def test_ingest_sensor_alignment_is_attested_and_forward_only():
    sql=(ROOT/'backend/db/migrations/00189_ingest_sensor_alignment.sql').read_text()
    assert 'p_metadata jsonb' in sql
    assert "p_image||jsonb_build_object('ingest',p_metadata)" in sql
    assert 'REVOKE EXECUTE ON FUNCTION photo.adopt_ingest_image(uuid,uuid,bigint,uuid,text,jsonb)' in sql
    assert "RAISE EXCEPTION 'Forward-only migration'" in sql
