"""Authored SQL boundary checks; not a positive photo/consent fixture."""
from uuid import uuid4


def test_glare_completed_authority_is_scheduler_only(db):
    signature = 'photo.v28_glare_completed_source_ready(uuid,uuid,uuid)'
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')", (signature,)).fetchone()[0]
    for role in ('photo_api', 'photo_worker', 'photo_portal', 'photo_gateway', 'photo_maintenance'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0]


def test_unbound_completed_glare_cannot_publish(db):
    tenant, task, result = uuid4(), uuid4(), uuid4()
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
    with db.transaction(force_rollback=True):
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.v28_glare_completed_source_ready(%s,%s,%s)', (tenant, task, result)).fetchone()[0] is False
        assert db.execute('SELECT photo.create_v28_restoration_review(%s,%s,%s)', (tenant, task, result)).fetchone()[0] is None
    db.execute('RESET ROLE')


def test_completed_glare_consumers_preserve_independent_authorities(db):
    definition = lambda signature: db.execute('SELECT pg_get_functiondef(%s::regprocedure)', (signature,)).fetchone()[0]
    gate = definition('photo.v28_glare_completed_source_ready(uuid,uuid,uuid)')
    for required in ('lock_v28_glare_source_session', 'lock_v28_donor_session',
                     'lock_v28_restoration_qualification', 'v28_restoration_inventory_digest_ready',
                     'auth_session_id', 'v28_glare_source_issuer_context', 'v28_donor_issuer_context',
                     'proof_jcs', 'FOR SHARE', 'canonical_result_id', 'fencing_token'):
        assert required in gate
    assert 'get_v28_restoration_review_selection' not in gate
    assert 'v28_restoration_adoption_receipt' not in gate
    for signature, mutation in (
        ('photo.adopt_v28_restoration_verified(uuid,uuid,uuid,text)', 'UPDATE photo.runs'),
        ('photo.create_v28_restoration_review(uuid,uuid,uuid)', 'INSERT INTO photo.reviews'),
    ):
        body = definition(signature)
        assert body.index('v28_glare_completed_source_ready') < body.index(mutation)
