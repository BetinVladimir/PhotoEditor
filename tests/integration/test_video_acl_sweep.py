"""TK-113: cross-cutting ACL check of every video table and function."""
import pytest

ROLES = ['photo_api', 'photo_portal', 'photo_worker', 'photo_scheduler', 'photo_maintenance', 'photo_admin', 'photo_publisher',
         'photo_mail', 'photo_gateway', 'photo_auth']
VIDEO_ROLES = {'photo_api', 'photo_portal', 'photo_worker', 'photo_scheduler', 'photo_maintenance', 'photo_admin'}


def video_tables(db):
    return [r[0] for r in db.execute(
        "SELECT format('%I.%I',n.nspname,c.relname) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace "
        "WHERE c.relkind IN('r','p') AND ((n.nspname='photo' AND c.relname LIKE 'video\\_%') OR n.nspname='photo_parts')")]


def video_functions(db):
    return db.execute(
        "SELECT p.oid::regprocedure::text,p.prosecdef,pg_get_userbyid(p.proowner),p.proconfig FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace "
        "WHERE n.nspname='photo' AND (p.proname LIKE '%video%' OR p.proname IN('ensure_video_egress_partitions')) ORDER BY 1").fetchall()


def test_tables_exist_and_are_closed_to_every_application_role(db):
    tables = video_tables(db)
    assert len(tables) >= 18, tables
    for t in tables:
        for role in ROLES:
            for priv in ('SELECT', 'INSERT', 'UPDATE', 'DELETE', 'TRUNCATE', 'REFERENCES', 'TRIGGER'):
                assert not db.execute('SELECT has_table_privilege(%s,%s,%s)', (role, t, priv)).fetchone()[0], f'{role} has {priv} on {t}'
        assert db.execute('SELECT relrowsecurity AND relforcerowsecurity FROM pg_class WHERE oid=%s::regclass', (t,)).fetchone()[0], f'{t} must have forced RLS'


def test_every_table_is_owned_by_the_function_owner_and_has_only_its_policy(db):
    for t in video_tables(db):
        owner = db.execute('SELECT pg_get_userbyid(relowner) FROM pg_class WHERE oid=%s::regclass', (t,)).fetchone()[0]
        pols = db.execute('SELECT polname,array_agg(r.rolname) FROM pg_policy p LEFT JOIN LATERAL unnest(p.polroles) o(oid) ON true '
                          'LEFT JOIN pg_roles r ON r.oid=o.oid WHERE polrelid=%s::regclass GROUP BY polname', (t,)).fetchall()
        assert pols == [('trusted_functions', ['photo_fn_owner'])], (t, pols)
        assert owner in ('photo_fn_owner', 'photo_test') or owner.startswith('photo_'), (t, owner)


def test_functions_are_security_definer_pinned_and_not_public(db):
    fns = video_functions(db)
    assert len(fns) >= 50, len(fns)
    for sig, secdef, owner, cfg in fns:
        assert secdef, sig
        assert owner == 'photo_fn_owner', (sig, owner)
        assert cfg and any(c.startswith('search_path=') and 'pg_catalog' in c for c in cfg), (sig, cfg)
        assert not db.execute("SELECT has_function_privilege('public',%s::regprocedure,'EXECUTE')", (sig,)).fetchone()[0], f'{sig} executable by PUBLIC'


# Called only from other SECURITY DEFINER functions or as a trigger: nobody may execute them directly.
INTERNAL = {'photo.admit_video_job(uuid,text,integer,integer,bigint)', 'photo.video_sequence_revisions_guard()',
            'photo.settle_video_job(uuid,uuid,text,text,text,text,jsonb)'}


def test_every_function_is_reachable_by_a_declared_role_only(db):
    for sig, *_ in video_functions(db):
        if sig in INTERNAL:
            assert not any(db.execute("SELECT has_function_privilege(%s,%s::regprocedure,'EXECUTE')", (r, sig)).fetchone()[0] for r in ROLES), sig
            continue
        granted = {r for r in ROLES if db.execute("SELECT has_function_privilege(%s,%s::regprocedure,'EXECUTE')", (r, sig)).fetchone()[0]}
        assert granted, f'{sig} is not granted to anyone'
        assert granted <= VIDEO_ROLES, f'{sig} granted to {granted - VIDEO_ROLES}'


@pytest.mark.parametrize('role,forbidden', [
    ('photo_api', ['create_video_job_attempt', 'claim_video_job', 'finish_video_job', 'finish_video_ingest', 'finish_video_upload_completion',
                   'purge_video_history', 'ensure_video_egress_partitions', 'set_video_tenant_limits', 'lease_video_egress']),
    ('photo_portal', ['begin_video_upload', 'create_video_render', 'claim_video_job', 'purge_video_history', 'set_video_tenant_limits']),
    ('photo_worker', ['begin_video_upload', 'create_video_share', 'purge_video_history', 'set_video_tenant_limits']),
    ('photo_scheduler', ['create_video_share', 'purge_video_history', 'set_video_tenant_limits']),
    ('photo_maintenance', ['begin_video_upload', 'create_video_share', 'claim_video_job', 'set_video_tenant_limits']),
])
def test_role_separation(db, role, forbidden):
    for name in forbidden:
        for (sig,) in db.execute("SELECT p.oid::regprocedure::text FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo' AND p.proname=%s", (name,)).fetchall():
            assert not db.execute("SELECT has_function_privilege(%s,%s::regprocedure,'EXECUTE')", (role, sig)).fetchone()[0], f'{role} can run {sig}'
