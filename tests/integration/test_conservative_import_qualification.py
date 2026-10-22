"""Authored SQL admission cases; not executed during the coding phase.

These refusal/ACL cases do not prove measured algorithm quality or successful
physical report verification; those require operator-installed fixtures.
"""
from uuid import uuid4

SIGNATURE = 'photo.v30_import_conservative_qualification_object(uuid,uuid,text,jsonb,uuid,text)'


def test_native_auto_qualification_is_scheduler_only(db):
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')", (SIGNATURE,)).fetchone()[0]
    for role in ('photo_api', 'photo_worker', 'photo_gateway', 'photo_portal'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, SIGNATURE)).fetchone()[0]


def test_native_auto_missing_policy_or_source_never_returns_report(db):
    import json
    tenant = uuid4()
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
    for operation, backend, license_cleared in (
        ('defects.auto_repair', 'cpu', True),
        ('sensor.dust_auto_repair', 'cpu', True),
        ('defects.auto_repair', 'cuda', True),
        ('sensor.dust_auto_repair', 'mps', True),
        ('defects.auto_repair', 'cpu', False),
        ('unknown', 'cpu', True),
    ):
        expected = {'Operation': operation, 'Version': '1',
                    'Backend': backend, 'LicenseCleared': license_cleared}
        rows = db.execute(
            "SELECT * FROM photo.v30_import_conservative_qualification_object(%s,%s,%s,%s::jsonb,%s,%s)",
            (tenant, uuid4(), '{}', json.dumps(expected), uuid4(), 'a' * 64),
        ).fetchall()
        assert rows == []


def test_auto_dispatch_retains_canonical_adoption_and_old_branches(db):
    definition = db.execute("SELECT pg_get_functiondef('photo.v30_import_adoption_qualification_object(uuid,uuid,integer,bigint,uuid,text,jsonb,uuid,text)'::regprocedure)").fetchone()[0]
    for name in ('v30_interchange_import_adoption_context',
                 'v30_import_conservative_qualification_object',
                 'v30_import_denoise_qualification_object',
                 'v30_import_upscale_qualification_object',
                 'v30_import_stats_qualification_object',
                 'v30_import_person_mask_qualification_object'):
        assert name in definition
