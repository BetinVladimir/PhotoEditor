"""Prepared PNG authority must stay scheduler-only and fail without receipts.
Authored for migration-backed qualification; not executed during coding.
"""
from uuid import uuid4


def test_prepared_png_accessor_is_private_except_scheduler(db):
    signature = 'photo.v30_interchange_prepared_defect_png_parents(uuid,uuid,text,uuid,jsonb,uuid,text)'
    for role in ('photo_api', 'photo_worker', 'photo_gateway', 'photo_portal'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, signature)).fetchone()[0]
    assert db.execute('SELECT has_function_privilege(\'photo_scheduler\',%s,\'EXECUTE\')', (signature,)).fetchone()[0]
    assert db.execute("SELECT prosecdef,proowner='photo_fn_owner'::regrole FROM pg_proc WHERE oid=%s::regprocedure", (signature,)).fetchone() == (True, True)


def test_prepared_png_accessor_rejects_unbound_receipt(db):
    tenant, workflow, entry, artifact = [uuid4() for _ in range(4)]
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(uuid4()),))
    result = db.execute('SELECT photo.v30_interchange_prepared_defect_png_parents(%s,%s,%s,%s,%s::jsonb,%s,%s)', (tenant, workflow, '{}', entry, '{}', artifact, 'a'*64)).fetchone()[0]
    assert result is None
