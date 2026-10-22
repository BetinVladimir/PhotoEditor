"""Authored source-bound applied media scenarios; not runtime qualification."""
from uuid import uuid4


def test_applied_preview_is_api_only(db):
    signature = 'photo.semantic_shown_applied_preview_object(uuid,uuid,uuid,uuid,text,text,text)'
    assert db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')", (signature,)).fetchone()[0]
    for role in ('photo_worker', 'photo_scheduler', 'photo_portal', 'photo_gateway'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0]


def test_applied_preview_unbound_target_has_no_media(db):
    tenant, target, actor, session = uuid4(), uuid4(), uuid4(), uuid4()
    with db.transaction(force_rollback=True):
        db.execute('SET LOCAL ROLE photo_api')
        for kind in ('params', 'presentation', 'source_image', 'after'):
            assert db.execute('SELECT * FROM photo.semantic_shown_applied_preview_object(%s,%s,%s,%s,%s,%s,%s)',
                              (tenant, target, actor, session, kind, '{}', '{}')).fetchone() is None


def test_applied_preview_uses_target_producer_and_exact_declared_media(db):
    definition = db.execute("SELECT pg_get_functiondef('photo.semantic_shown_applied_preview_object(uuid,uuid,uuid,uuid,text,text,text)'::regprocedure)").fetchone()[0]
    for required in ('semantic_shown_applied_image', 'preview_result_id', 'preview_attempt', 'preview_fence',
                     "output_role='preview_presentation'", "output_role='after'", 'v2_task_inputs',
                     "presentation->'working_image'", 'object_version', 'expires_at>clock_timestamp()'):
        assert required in definition
    assert 'INSERT INTO photo.tasks' not in definition


def test_applied_preview_requires_typed_shown_ancestry_and_exact_operation(db):
    definition = db.execute("SELECT pg_get_functiondef('photo.semantic_shown_applied_preview_object(uuid,uuid,uuid,uuid,text,text,text)'::regprocedure)").fetchone()[0]
    for required in ("i.contract='Params'", "i.contract='WorkingImage'", "i.contract='NpyArray'",
                     'i.contract=evidence_contract', 'i.contract=presentation_contract',
                     "i.contract='EncodedImage'", 'semantic_uuid_v5(run_id,expected',
                     "operation_version='1'", "outputs=jsonb_build_array(body->'presentation')"):
        assert required in definition
