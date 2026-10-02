from pathlib import Path


ROOT=Path(__file__).resolve().parents[2]


def test_sensor_dust_continuation_is_fenced_and_forward_only():
    sql=(ROOT/'backend/db/migrations/00193_apply_v4_sensor_dust_continuation.sql').read_text()
    for token in ('sensor.dust_repair','set_apply_v4_group_count','open_apply_v4_dust_reviews',
                  'process_apply_v4_dust_reviews','apply_v4_dust_decisions','z.review_required',
                  "v.kind='sensor_dust'",'SECURITY DEFINER','photo_fn_owner','REVOKE ALL',
                  'photo_scheduler',"RAISE EXCEPTION 'Forward-only migration'"):
        assert token in sql
    assert "p_token" in sql and "lease_until" in sql


def test_empty_group_set_and_automatic_no_candidate_pass_are_explicit():
    sql=(ROOT/'backend/db/migrations/00193_apply_v4_sensor_dust_continuation.sql').read_text()
    assert 'p_count NOT BETWEEN 0 AND 1000' in sql
    assert "coalesce(v.selection,jsonb_build_object('version','sensor_dust_review_v1'" in sql
    assert "(NOT z.review_required OR v.id IS NOT NULL)" in sql
