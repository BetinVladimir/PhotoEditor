"""Authored boundary/source guards; full photo fixtures belong to deep testing."""
from uuid import uuid4


def test_unbound_restoration_completed_media_is_unavailable(db):
    tenant, preview = uuid4(), uuid4()
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
    assert db.execute('SELECT photo.restoration_completed_preview_media_live(%s,%s)', (tenant, preview)).fetchone()[0] is False
    assert db.execute('SELECT photo.completed_glare_consent_loss(%s,%s,%s)', (tenant, uuid4(), uuid4())).fetchone()[0] is None
    assert db.execute('SELECT photo.glare_withdrawn_quota_settled(%s,%s)', (tenant, uuid4())).fetchone()[0] is False


def test_restoration_decision_and_selection_repeat_live_four_stage_media(db):
    body = lambda name: db.execute('SELECT pg_get_functiondef(%s::regprocedure)', (name,)).fetchone()[0]
    for name in ('photo.v28_restoration_review_decision_ready(uuid,uuid)', 'photo.v28_restoration_review_selection(uuid,uuid)'):
        assert 'photo.restoration_completed_preview_media_live(p_tenant,preview.id)' in body(name)
    gate = body('photo.restoration_completed_preview_media_live(uuid,uuid)')
    for required in ('v28_restoration_preview_derivative_receipt', 'canonical_result_id', 'produced_attempt',
                     'produced_fence', 'output_uploads', 'object_version', 'expires_at', 'verified_at',
                     'manifest_id', 'profile_hash', 'n=4', 'FOR SHARE'):
        assert required in gate


def test_glare_withdrawal_requires_typed_proof_and_preserves_drain(db):
    body = lambda name: db.execute('SELECT pg_get_functiondef(%s::regprocedure)', (name,)).fetchone()[0]
    witness = body('photo.completed_glare_consent_loss(uuid,uuid,uuid)')
    for required in ('proof_jcs', 'v28_glare_source_issuer_context', 'v28_donor_issuer_context',
                     'auth_session_id', 'GLARE_CONSENT_REVOKED', 'GLARE_SOURCE_CONFIRMATION_WITHDRAWN'):
        assert required in witness
    assert 'ErrExpired' not in witness
    withdrawal = body('photo.withdraw_completed_glare_consent()')
    assert "status='cancelling'" in withdrawal
    assert "state='failed'" not in withdrawal
    settled = body('photo.glare_withdrawn_quota_settled(uuid,uuid)')
    for required in ('lease_until', 'output_uploads', 'backend_documents', 'resource_reservations', 'resource_slots'):
        assert required in settled
    quota = body('photo.release_next_glare_withdrawn_quota()')
    assert quota.index('glare_withdrawn_quota_settled(r.tenant_id,r.id)') < quota.index('reserved_bytes=reserved_bytes-q.bytes')


def test_glare_withdrawal_acl_and_exact_review_cancel_guard(db):
    for function in ('photo.withdraw_completed_glare_consent()', 'photo.release_next_glare_withdrawn_quota()'):
        assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')", (function,)).fetchone()[0]
        for role in ('photo_api', 'photo_worker', 'photo_portal', 'photo_gateway', 'photo_maintenance'):
            assert not db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')', (role, function)).fetchone()[0]
    guard = db.execute("SELECT pg_get_functiondef('photo.guard_v28_restoration_review()'::regprocedure)").fetchone()[0]
    assert 'RESTORATION_CANCEL_AUTHORITY' in guard
    assert "to_jsonb(NEW)-'state'-'revision'" in guard
