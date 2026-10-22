"""SQL privilege scenarios for atomic relight publication, authored not executed."""
import pytest


@pytest.mark.parametrize('role', ['photo_scheduler', 'photo_worker', 'photo_api', 'public'])
def test_relight_publication_is_private_and_commit_is_scheduler_only(db, role):
    private = [
        'photo.create_v28_relight_review(uuid,uuid,uuid,text)',
        'photo.adopt_v28_relight_pre_review(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,text,text,text,text)',
    ]
    for signature in private:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",
                          (role, signature)).fetchone()[0] is False
    commit = 'photo.adopt_v28_relight(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,text,text,text,text)'
    assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",
                      (role, commit)).fetchone()[0] is (role == 'photo_scheduler')


def test_relight_review_publication_uses_owner_and_fixed_search_path(db):
    signatures = [
        'photo.create_v28_relight_review(uuid,uuid,uuid,text)',
        'photo.adopt_v28_relight(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text,text,text,text,text)',
    ]
    for signature in signatures:
        owner, definer, config = db.execute(
            'SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=%s::regprocedure',
            (signature,)).fetchone()
        assert owner == 'photo_fn_owner'
        assert definer is True
        assert 'search_path=pg_catalog, photo' in config


@pytest.mark.parametrize('role', ['photo_scheduler', 'photo_worker', 'photo_api', 'public'])
def test_relight_preview_helpers_are_private_and_selection_is_api_only(db, role):
    for signature in [
        'photo.v28_relight_derivative_source(uuid,uuid)',
        'photo.v28_relight_base_preview_ready(uuid,uuid,text)',
        'photo.v28_relight_review_preview_ready(uuid,uuid)',
    ]:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",
                          (role, signature)).fetchone()[0] is False
    signature = 'photo.get_v28_relight_review_selection(uuid,uuid,uuid,uuid)'
    assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",
                      (role, signature)).fetchone()[0] is (role == 'photo_api')


@pytest.mark.parametrize('role', ['photo_scheduler', 'photo_worker', 'photo_api', 'public'])
def test_relight_decision_functions_are_api_only(db, role):
    for signature in [
        'photo.v28_relight_review_decision_ready(uuid,uuid,uuid,uuid)',
        'photo.decide_v28_relight_review(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text,jsonb)',
    ]:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",
                          (role, signature)).fetchone()[0] is (role == 'photo_api')


@pytest.mark.parametrize('role', ['photo_scheduler', 'photo_worker', 'photo_api', 'public'])
def test_relight_selected_result_cannot_bypass_live_authority(db, role):
    for signature in [
        'photo.v28_relight_review_authority(uuid,uuid,uuid,uuid)',
        'photo.get_v28_relight_review_selection_pre_authority(uuid,uuid,uuid,uuid)',
    ]:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",
                          (role, signature)).fetchone()[0] is False


def test_relight_selected_live_authority_rejects_unknown_review(db):
    from uuid import uuid4
    tenant, actor, session, review = [uuid4() for _ in range(4)]
    with db.transaction():
        db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
        db.execute('SET LOCAL ROLE photo_api')
        assert db.execute('SELECT * FROM photo.get_v28_relight_review_selection(%s,%s,%s,%s)',
                          (tenant, actor, session, review)).fetchall() == []


@pytest.mark.parametrize('role', ['photo_scheduler', 'photo_worker', 'photo_api', 'public'])
def test_relight_background_jobs_are_scheduler_only(db, role):
    for signature in [
        'photo.enqueue_next_v28_relight_before_media()',
        'photo.finalize_v28_relight_selected_run()',
    ]:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",
                          (role, signature)).fetchone()[0] is (role == 'photo_scheduler')
    for signature in [
        'photo.v28_relight_review_completion_ready(uuid,uuid)',
        'photo.v28_relight_selected_run_ready(uuid,uuid)',
    ]:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",
                          (role, signature)).fetchone()[0] is False


@pytest.mark.parametrize('role', ['photo_scheduler', 'photo_worker', 'photo_api', 'public'])
def test_relight_viewed_gate_and_legacy_decision_are_private(db, role):
    for signature in [
        'photo.v28_relight_viewed_pair_ready(uuid,uuid,uuid,uuid,bigint,jsonb)',
        'photo.decide_v28_relight_review_pre_viewed(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text)',
    ]:
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",
                          (role, signature)).fetchone()[0] is False


def test_relight_viewed_decision_refuses_missing_grant(db):
    from uuid import uuid4
    tenant, review, actor, session = [uuid4() for _ in range(4)]
    with db.transaction():
        db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
        db.execute('SET LOCAL ROLE photo_api')
        assert db.execute(
            "SELECT photo.decide_v28_relight_review(%s,%s,%s,%s,1,'skip','{}'::jsonb,%s,'skip',NULL)",
            (tenant, review, actor, session, 'a' * 64)).fetchone()[0] is False


@pytest.mark.parametrize('signature', [
    'photo.v28_relight_viewed_pair_ready(uuid,uuid,uuid,uuid,bigint,jsonb)',
    'photo.decide_v28_relight_review(uuid,uuid,uuid,uuid,bigint,text,jsonb,text,text,jsonb)',
])
def test_relight_viewed_functions_use_fixed_owner_and_search_path(db, signature):
    owner, definer, config = db.execute(
        'SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=%s::regprocedure',
        (signature,)).fetchone()
    assert owner == 'photo_fn_owner'
    assert definer is True
    assert 'search_path=pg_catalog, photo' in config


@pytest.mark.parametrize('role', ['photo_scheduler', 'photo_worker', 'photo_api', 'photo_portal', 'photo_gateway', 'public'])
def test_relight_apply_version_is_api_only(db, role):
    signature = 'photo.apply_v28_relight_version(uuid,uuid,uuid,uuid,text,text,text)'
    assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",
                      (role, signature)).fetchone()[0] is (role == 'photo_api')


def test_relight_apply_version_owner_and_search_path(db):
    row = db.execute(
        "SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc "
        "WHERE oid='photo.apply_v28_relight_version(uuid,uuid,uuid,uuid,text,text,text)'::regprocedure"
    ).fetchone()
    assert row[0] == 'photo_fn_owner'
    assert row[1] is True
    assert 'search_path=pg_catalog, photo' in row[2]


@pytest.mark.parametrize('role', ['photo_scheduler', 'photo_worker', 'photo_api', 'public'])
def test_relight_pending_read_authority_is_api_only(db, role):
    signature = 'photo.v28_relight_review_read_ready(uuid,uuid,uuid,uuid)'
    assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",
                      (role, signature)).fetchone()[0] is (role == 'photo_api')


def test_relight_pending_read_authority_has_fixed_definer_boundary(db):
    owner, definer, settings = db.execute("""
        SELECT pg_get_userbyid(proowner), prosecdef, proconfig
        FROM pg_proc WHERE oid =
        'photo.v28_relight_review_read_ready(uuid,uuid,uuid,uuid)'::regprocedure
    """).fetchone()
    assert owner == 'photo_fn_owner'
    assert definer is True
    assert 'search_path=pg_catalog, photo' in settings
