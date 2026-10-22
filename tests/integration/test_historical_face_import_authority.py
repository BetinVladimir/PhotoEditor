"""Historical materialization authority; authored, deliberately not executed."""
from uuid import uuid4


def test_historical_readers_are_scheduler_only(db):
    signatures = (
        'photo.v30_interchange_prepared_ref(uuid,uuid,text,uuid,jsonb,uuid,text,uuid,text)',
        'photo.v30_import_historical_document_authorized(uuid,uuid,text,uuid,text,text,uuid,text,uuid,text,text,text,uuid,text)',
    )
    for signature in signatures:
        for role in ('photo_api', 'photo_worker', 'photo_gateway', 'photo_portal'):
            assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0]
        assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')", (signature,)).fetchone()[0]


def test_unknown_original_face_refs_cannot_become_local_authority(db):
    tenant = uuid4()
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
    args = (tenant, uuid4(), '{}', uuid4(), 'a'*64, 'FaceLandmarks',
            uuid4(), 'b'*64, uuid4(), 'c'*64, 'd'*64, 'mps', uuid4(), 'e'*64)
    assert db.execute('SELECT photo.v30_import_historical_document_authorized(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)', args).fetchone()[0] is False


def test_generated_origin_scope_and_adoption_run_are_private(db):
    signature = 'photo.v30_interchange_prepared_scope(uuid,uuid,text,uuid,text,text)'
    for role in ('photo_api', 'photo_worker', 'photo_scheduler', 'photo_gateway', 'photo_portal'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0]
    helper = 'photo.v30_import_historical_adoption_run(uuid,uuid,integer,bigint,uuid)'
    for role in ('photo_api', 'photo_worker', 'photo_gateway', 'photo_portal'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, helper)).fetchone()[0]
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')", (helper,)).fetchone()[0]
