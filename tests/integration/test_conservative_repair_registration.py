"""Automatic repair is a separate CPU contract with bounded gateway outputs.

Authored only; execute after current forward-only migrations in deep testing.
"""
import pytest


@pytest.mark.parametrize('automatic,reviewed,report,routing', [
    ('defects.auto_repair', 'defects.repair', 'defect_repair_report', 'cpu.defects_auto_repair'),
    ('sensor.dust_auto_repair', 'sensor.dust_repair', 'dust_repair_report', 'cpu.dust_auto_repair'),
])
def test_automatic_repair_registration_preserves_reviewed_output_bounds(db, automatic, reviewed, report, routing):
    operation = db.execute("SELECT resource_class,routing_key,model_id,timeout_seconds,backends FROM photo.operation_registry WHERE operation=%s AND version='1'", (automatic,)).fetchone()
    assert operation == ('cpu', routing, None, 300, ['cpu'])
    def outputs(name):
        return db.execute("SELECT role,media_types,max_bytes,max_count FROM photo.operation_output_policies WHERE operation=%s AND version='1' ORDER BY role", (name,)).fetchall()
    current = outputs(automatic)
    assert {row[0] for row in current} == {'working_rgb', 'working_image', report}
    assert current == outputs(reviewed)
    assert all(row[2] > 0 and row[3] == 1 for row in current)
    # Registration does not grant runtime roles permission to widen output policies.
    for role in ('photo_api', 'photo_worker', 'photo_gateway', 'photo_scheduler'):
        assert db.execute("SELECT has_table_privilege(%s,'photo.operation_output_policies','INSERT,UPDATE,DELETE')", (role,)).fetchone()[0] is False
