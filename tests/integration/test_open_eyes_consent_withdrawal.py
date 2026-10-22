"""Authored production SQL boundaries. Full physical race fixtures deferred."""
from uuid import uuid4


def test_open_eyes_unbound_consent_is_not_a_failure_witness(db):
    assert db.execute('SELECT photo.completed_open_eyes_consent_loss(%s,%s,%s)', (uuid4(),uuid4(),uuid4())).fetchone()[0] is None
    assert db.execute('SELECT photo.open_eyes_withdrawn_quota_settled(%s,%s)', (uuid4(),uuid4())).fetchone()[0] is False


def test_open_eyes_loss_uses_only_authenticated_donor_proof(db):
    body=lambda name:db.execute('SELECT pg_get_functiondef(%s::regprocedure)',(name,)).fetchone()[0]
    witness=body('photo.completed_open_eyes_consent_loss(uuid,uuid,uuid)')
    for required in ('portrait.open_eyes','v28_donor_descriptor_jcs','v28_donor_queued_receipt',
                     'descriptor_sha','auth_session_id','v28_donor_issuer_context','proof_jcs',
                     'OPEN_EYES_CONSENT_REVOKED','OPEN_EYES_SOURCE_CONFIRMATION_WITHDRAWN'):
        assert required in witness
    assert 'v28_glare_source_issuer_context' not in witness
    assert 'v28_restoration_descriptor_jcs' not in witness
    withdrawal=body('photo.withdraw_completed_open_eyes_consent()')
    assert 'v26_derivative_task_id' in withdrawal
    assert 'v28_restoration_preview_task_id' not in withdrawal
    assert 'v28_donor_authority_failure' in withdrawal
    for required in ('lease_until','output_uploads','backend_documents','resource_reservations','resource_slots'):
        assert required in body('photo.open_eyes_withdrawn_quota_settled(uuid,uuid)')
    guard=body('photo.guard_v28_open_eyes_review_update()')
    assert 'OPEN_EYES_CANCEL_AUTHORITY' in guard
    assert "to_jsonb(NEW)-'state'-'revision'" in guard


def test_open_eyes_withdrawal_is_scheduler_only(db):
    for signature in ('photo.withdraw_completed_open_eyes_consent()','photo.release_next_open_eyes_withdrawn_quota()'):
        assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(signature,)).fetchone()[0]
        for role in ('photo_api','photo_worker','photo_portal','photo_gateway','photo_maintenance'):
            assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0]
