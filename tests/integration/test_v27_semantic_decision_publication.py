from uuid import uuid4
import pytest
import psycopg


def test_semantic_publication_functions_are_api_only(db):
    public_functions=(
        'photo.semantic_approved_image_binary(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,boolean,uuid,text,text,text,integer)',
        'photo.semantic_approved_mask_binary(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,text,text)',
        'photo.semantic_approved_candidate_child(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,text,text)',
        'photo.semantic_approved_image_object(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,boolean,uuid,text)',
        'photo.semantic_approved_protection_binary(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,uuid,text,uuid,text,text)',
        'photo.semantic_approved_protection_origin(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,uuid,text,uuid,text,text)',
        'photo.semantic_approved_protection_object(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text,uuid,text)',
        'photo.semantic_approved_protection_pin(uuid,uuid,uuid,uuid,bigint,uuid,bigint,uuid,uuid,text)',
        'photo.reserve_semantic_decision_document(uuid,uuid,uuid,uuid,bigint,text,text,bigint,text)',
        'photo.complete_semantic_decision_document(uuid,uuid,uuid,uuid,bigint,text,uuid,uuid,text,bigint,text,text)',
    )
    private_functions=(
        'photo.finish_semantic_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,uuid,jsonb,jsonb,jsonb,jsonb)',
        'photo.activate_semantic_apply_child(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb)',
        'photo.semantic_decision_document_owner(uuid,uuid,uuid,text)',
        'photo.lock_semantic_decision_batch(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb)',
        'photo.create_semantic_apply_draft_shell(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb,jsonb)',
        'photo.create_semantic_apply_draft_pre_quota(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb,jsonb)',
        'photo.reserve_semantic_apply_quota(uuid,uuid,uuid,uuid,jsonb,jsonb)',
        'photo.semantic_apply_sealed_inventory_ready(uuid,uuid,uuid,jsonb,jsonb,jsonb)',
        'photo.seal_semantic_apply_inputs_pre_persisted_check(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb)',
        'photo.lock_semantic_decision_publication(uuid,uuid,uuid,uuid,bigint)',
    )
    for function in public_functions:
        assert db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')",(function,)).fetchone()[0]
        for role in ('photo_worker','photo_scheduler','photo_gateway','photo_portal'):
            assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,function)).fetchone()[0]
    for function in private_functions:
        for role in ('photo_worker','photo_scheduler','photo_gateway','photo_portal'):
            assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,function)).fetchone()[0]
        assert not db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')",(function,)).fetchone()[0]
    with db.transaction(force_rollback=True):
        db.execute('SET LOCAL ROLE photo_api')
        with pytest.raises(psycopg.errors.InsufficientPrivilege):
            db.execute('SELECT * FROM photo.reserve_semantic_decision_document(%s,%s,%s,%s,1,%s,%s,10,%s)',(uuid4(),uuid4(),uuid4(),uuid4(),'a'*64,'b'*64,'fixture'))
