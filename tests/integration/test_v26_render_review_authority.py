"""Render-review SQL entrypoint authorization; not a positive render lifecycle."""
from uuid import uuid4

import psycopg
import pytest


CREATE_REVIEW = 'photo.create_v26_render_review(uuid,uuid,uuid,uuid,uuid)'


def test_render_review_has_only_scheduler_entrypoint(db):
    assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                      ('photo_scheduler', CREATE_REVIEW, 'EXECUTE')).fetchone()[0]
    for role in ('photo_api', 'photo_worker', 'photo_gateway', 'photo_portal', 'public'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',
                              (role, CREATE_REVIEW, 'EXECUTE')).fetchone()[0]
    for role in ('photo_scheduler', 'photo_api', 'photo_worker', 'public'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',
                              (role, 'photo.finalize_v2_run_pre_render_reviews(uuid,uuid)',
                               'EXECUTE')).fetchone()[0]


def test_render_review_missing_tenant_has_no_side_effects(db):
    tenant = uuid4()
    with db.transaction(force_rollback=True):
        db.execute('SET LOCAL ROLE photo_scheduler')
        result = db.execute('SELECT photo.create_v26_render_review(%s,%s,%s,%s,%s)',
                            (tenant, uuid4(), uuid4(), uuid4(), uuid4())).fetchone()[0]
        assert result is None
        db.execute('RESET ROLE')
        assert db.execute('SELECT count(*) FROM photo.reviews WHERE tenant_id=%s',
                          (tenant,)).fetchone()[0] == 0
        assert db.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s',
                          (tenant,)).fetchone()[0] == 0


@pytest.mark.parametrize('role', ['photo_api', 'photo_worker'])
def test_render_review_creation_refuses_untrusted_caller(db, role):
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        with db.transaction(force_rollback=True):
            # Role values are a fixed test allowlist, never external input.
            db.execute('SET LOCAL ROLE ' + role)
            db.execute('SELECT photo.create_v26_render_review(%s,%s,%s,%s,%s)',
                       tuple(uuid4() for _ in range(5)))


def test_review_before_media_keeps_readiness_private(db):
    enqueue = 'photo.enqueue_v26_review_before_media(uuid,uuid)'
    readiness = 'photo.v26_review_media_source_ready(uuid)'
    assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                      ('photo_scheduler', enqueue, 'EXECUTE')).fetchone()[0]
    for role in ('photo_scheduler', 'photo_api', 'photo_worker', 'public'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',
                              (role, readiness, 'EXECUTE')).fetchone()[0]
    for role in ('photo_api', 'photo_worker', 'public'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',
                              (role, enqueue, 'EXECUTE')).fetchone()[0]
    with db.transaction(force_rollback=True):
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.enqueue_v26_review_before_media(%s,%s)',
                          (uuid4(), uuid4())).fetchone()[0] is None


def test_review_media_publication_lock_is_private(db):
    signature = 'photo.lock_v26_review_media_publication(uuid)'
    for role in ('photo_scheduler', 'photo_api', 'photo_worker', 'photo_gateway', 'public'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',
                              (role, signature, 'EXECUTE')).fetchone()[0]


def test_review_media_publication_missing_job_has_no_authority(db):
    # Administrative fixture invokes the private helper, not a public capability.
    with db.transaction(force_rollback=True):
        assert db.execute('SELECT photo.lock_v26_review_media_publication(%s)',
                          (uuid4(),)).fetchone()[0] is False
