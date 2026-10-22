"""Authored boundary scenarios; no successful physical model qualification is claimed."""
from uuid import uuid4


def test_negative_selected_apply_is_api_only(db):
    signature = 'photo.apply_v29_negative_version(uuid,uuid,uuid,uuid,text,text,text)'
    assert db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')", (signature,)).fetchone()[0]
    for role in ('photo_scheduler', 'photo_worker', 'photo_portal', 'photo_gateway'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0]


def test_negative_unbound_review_cannot_read_or_create_version(db):
    tenant, review, actor, session = uuid4(), uuid4(), uuid4(), uuid4()
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
    with db.transaction(force_rollback=True):
        db.execute('SET LOCAL ROLE photo_api')
        assert db.execute('SELECT * FROM photo.get_v29_negative_apply_selection(%s,%s,%s,%s)',
                          (tenant, actor, session, review)).fetchone() is None
        assert db.execute('SELECT photo.apply_v29_negative_version(%s,%s,%s,%s,%s,%s,%s)',
                          (tenant, review, actor, session, 'a'*64, '{}', '{}')).fetchone()[0] is None


def test_negative_selected_apply_retains_human_choice_and_current_authority(db):
    definition = lambda signature: db.execute('SELECT pg_get_functiondef(%s::regprocedure)', (signature,)).fetchone()[0]
    gate = definition('photo.v29_negative_apply_current_authority(uuid,uuid,uuid,uuid)')
    assert 'read_v29_negative_queued_receipt' in gate
    assert 'v2_editor_authorized' in gate
    assert 'v29_negative_review_selection' in gate
    apply = definition('photo.apply_v29_negative_version(uuid,uuid,uuid,uuid,text,text,text)')
    for required in ('get_v29_negative_apply_selection', 'v29_negative_apply_current_authority',
                     'V29.NegativeAppliedVersion', 'negative_source', 'RETURN src.id'):
        assert required in apply
    assert "selected.route='skip'" in apply
    assert 'INSERT INTO photo.tasks' not in apply
