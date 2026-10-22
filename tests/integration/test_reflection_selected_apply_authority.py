"""Authored boundary scenarios; no successful physical model qualification is claimed."""
from uuid import uuid4


def test_reflection_selected_apply_is_api_only(db):
    signature = 'photo.apply_v28_reflection_version(uuid,uuid,uuid,uuid,text,text,text)'
    assert db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')", (signature,)).fetchone()[0]
    for role in ('photo_scheduler', 'photo_worker', 'photo_portal', 'photo_gateway'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0]


def test_reflection_unbound_review_cannot_read_or_create_version(db):
    tenant, review, actor, session = uuid4(), uuid4(), uuid4(), uuid4()
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
    with db.transaction(force_rollback=True):
        db.execute('SET LOCAL ROLE photo_api')
        assert db.execute('SELECT photo.v28_reflection_current_authority(%s,%s,%s,%s)',
                          (tenant, review, actor, session)).fetchone()[0] is False
        assert db.execute('SELECT photo.apply_v28_reflection_version(%s,%s,%s,%s,%s,%s,%s)',
                          (tenant, review, actor, session, 'a'*64, '{}', '{}')).fetchone()[0] is None


def test_reflection_current_authority_is_used_at_mutation_boundaries(db):
    definition = lambda signature: db.execute('SELECT pg_get_functiondef(%s::regprocedure)', (signature,)).fetchone()[0]
    gate = definition('photo.v28_reflection_current_authority(uuid,uuid,uuid,uuid)')
    for required in ('lock_v28_restoration_qualification', 'v28_manual_pair_origin',
                     'v28_restoration_inventory_digest_ready', 'v2_input_closure_ready',
                     'canonical_result_id', 'fencing_token', 'v2_editor_authorized',
                     'v28_restoration_params_intent', "NOT IN('cpu','cuda')"):
        assert required in gate
    for signature in (
        'photo.decide_v28_restoration_review(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text)',
        'photo.get_v28_restoration_review_selection(uuid,uuid,uuid,uuid)',
        'photo.apply_v28_reflection_version(uuid,uuid,uuid,uuid,text,text,text)',
    ):
        assert 'v28_reflection_current_authority' in definition(signature)
    apply = definition('photo.apply_v28_reflection_version(uuid,uuid,uuid,uuid,text,text,text)')
    assert "selected.route='base'" in apply and "RETURN src.id" in apply
    assert "selected.route='skip'" in apply
    assert 'V28.ReflectionAppliedVersion' in apply
    assert "reflection_source" in apply
    assert 'INSERT INTO photo.tasks' not in apply
