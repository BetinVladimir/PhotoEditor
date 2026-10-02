from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]


def test_sensor_dust_review_and_repair_are_strictly_registered():
    sql = (ROOT / "backend/db/migrations/00192_sensor_dust_review_repair.sql").read_text()
    for token in ("sensor.dust_repair", "photo.create_dust_review", "SECURITY DEFINER",
                  "t.operation<>'sensor.dust'", "output_role='dust_candidates'",
                  "t.inputs @>", "photo_fn_owner", "REVOKE ALL", "photo_scheduler",
                  "Forward-only migration"):
        assert token in sql
    assert "ON CONFLICT" not in sql
