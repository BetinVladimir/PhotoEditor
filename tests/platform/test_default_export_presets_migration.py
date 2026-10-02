from pathlib import Path

SQL=(Path(__file__).parents[2]/'backend/db/migrations/00167_default_export_presets.sql').read_text()

def test_defaults_are_atomic_portable_and_forward_only():
    for token in ('AFTER INSERT ON photo.tenants','SECURITY DEFINER','photo_fn_owner','REVOKE ALL','Master','Web','work_rec2020_gamma24_v1','srgb_v1','allow_upscale','Forward-only migration'):
        assert token in SQL
    assert 'model' in SQL.lower() and 'credential' in SQL.lower()

def test_seed_hashes_are_fixed_canonical_contract_hashes():
    assert '53ce136942dc0e726e24ce51f6aafcd6fae5edace49b38a8534030720639f826' in SQL
    assert '4d6d9b57d4318d1d0cd33677c5d31ca842c5e5fcd31cfd18c096896817d8b21a' in SQL
