"""Canonical history authority scenarios, authored without execution."""
from uuid import uuid4


def test_canonical_history_is_scheduler_only(db):
    signature = 'photo.v30_canonical_image_ancestor(uuid,uuid,uuid,text,uuid,text)'
    for role in ('photo_api', 'photo_worker', 'photo_gateway', 'photo_portal'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, signature)).fetchone()[0]
    assert db.execute('SELECT has_function_privilege(\'photo_scheduler\',%s,\'EXECUTE\')', (signature,)).fetchone()[0]


def test_archive_ids_or_same_ref_do_not_authorize_history(db):
    tenant, project, image = uuid4(), uuid4(), uuid4()
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
    assert not db.execute('SELECT photo.v30_canonical_image_ancestor(%s,%s,%s,%s,%s,%s)', (tenant, project, image, 'a'*64, image, 'a'*64)).fetchone()[0]
