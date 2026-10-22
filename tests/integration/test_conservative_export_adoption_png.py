"""Authored SQL scope cases, pending deep test execution."""
from uuid import uuid4

SIGNATURE = 'photo.v30_export_adoption_defect_png(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text)'


def test_scheduler_png_authority_is_not_public(db):
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')", (SIGNATURE,)).fetchone()[0]
    for role in ('photo_api', 'photo_worker', 'photo_gateway', 'photo_portal'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, SIGNATURE)).fetchone()[0]


def test_scheduler_png_without_current_claim_is_refused(db):
    params = [uuid4() for _ in range(6)] + ['request', 'a' * 64, uuid4()]
    for _ in range(5):
        params.extend([uuid4(), 'b' * 64])
    placeholders = ','.join(['%s'] * 19)
    assert db.execute('SELECT photo.v30_export_adoption_defect_png(' + placeholders + ')', params).fetchone()[0] is False
