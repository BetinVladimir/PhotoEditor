from pathlib import Path

SQL=(Path(__file__).resolve().parents[2]/'backend/db/migrations/00187_apply_v4_composition.sql').read_text()


def test_composition_structure_is_registered_and_apply_allowlisted():
    assert "VALUES('composition.structure','1','cpu','cpu.composition'" in SQL
    assert "VALUES('composition.structure','1','image_structure'" in SQL
    assert "'composition.structure','composition.plan','geometry.crop'" in SQL
    assert 'SECURITY DEFINER' in SQL and 'REVOKE ALL ON FUNCTION photo.register_apply_v4_plan' in SQL
