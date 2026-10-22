"""Full head fresh/upgrade security catalog scenarios on the dedicated isolated PostgreSQL instance."""
from pathlib import Path
from uuid import uuid4
import pytest
from psycopg import sql

ROOT = Path(__file__).resolve().parents[2]


def assert_security_catalog(db, *, policy_row_lock_installed=True):
    purpose_expression = db.execute("""
      SELECT pg_get_expr(conbin,conrelid) FROM pg_constraint
      WHERE conrelid='photo.backend_documents'::regclass
        AND conname='backend_documents_purpose_check'
    """).fetchone()[0]
    # The table predicate must admit physical delivery publications while
    # refusing similarly named purposes outside the reviewed bounded family.
    for purpose, accepted in (
        ('task_params', True), ('v30_delivery_assembly', True),
        ('v30_delivery_slots_000', True), ('v30_delivery_slots_999', True),
        ('v30_delivery_signed_closure', True), ('v30_delivery_signed_envelope', True),
        ('v30_delivery_slots_00', False), ('v30_delivery_slots_0000', False),
        ('v30_delivery_slots_xyz', False), ('v30_delivery_assembly_extra', False),
    ):
        query = sql.SQL('SELECT ({}) FROM (VALUES (%s::text)) AS input(purpose)').format(
            sql.SQL(purpose_expression))
        assert db.execute(query, (purpose,)).fetchone() == (accepted,), purpose
    assert db.execute("SELECT NOT rolcanlogin AND NOT rolsuper AND NOT rolbypassrls FROM pg_roles WHERE rolname='photo_fn_owner'").fetchone() == (True,)
    violations = db.execute("""
      SELECT p.oid::regprocedure::text FROM pg_proc p
      JOIN pg_namespace n ON n.oid=p.pronamespace
      WHERE n.nspname='photo' AND p.prosecdef AND (
       pg_get_userbyid(p.proowner)<>'photo_fn_owner'
       OR NOT coalesce(p.proconfig @> ARRAY['search_path=pg_catalog, photo']::text[]
           OR p.proconfig @> ARRAY['search_path=pg_catalog,photo']::text[],false)
       OR EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
          WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))
      ORDER BY 1
    """).fetchall()
    assert not violations, violations
    for role in ('photo_api', 'photo_auth', 'photo_portal', 'photo_worker',
                 'photo_scheduler', 'photo_publisher', 'photo_maintenance'):
        assert db.execute("SELECT NOT rolsuper AND NOT rolbypassrls FROM pg_roles WHERE rolname=%s", (role,)).fetchone() == (True,)
    # 02573 restores row-lock authority only to the SECURITY DEFINER owner.
    assert db.execute("SELECT has_column_privilege('photo_fn_owner','photo.capacity_policy_timings','policy_sha','UPDATE'),has_column_privilege('photo_fn_owner','photo.capacity_policy_timings','policy_jcs','UPDATE'),has_table_privilege('photo_fn_owner','photo.capacity_policy_timings','DELETE')").fetchone() == (policy_row_lock_installed,False,False)
    for role in ('photo_api','photo_worker','photo_scheduler'):
        assert db.execute("SELECT has_column_privilege(%s,'photo.capacity_policy_timings','policy_sha','UPDATE')", (role,)).fetchone() == (False,)
    # Worker cannot replace the reviewed functions or bypass tenant RLS.
    assert db.execute("SELECT has_schema_privilege('photo_worker','photo','CREATE')").fetchone() == (False,)


def test_fresh_full_head_security_catalog(db):
    assert_security_catalog(db)


@pytest.mark.migration_head(2499)
def test_upgrade_tail_preserves_existing_tenant_and_acl_boundary(db):
    user = db.execute("INSERT INTO photo.users(email_normalized) VALUES(%s) RETURNING id", (str(uuid4())+'@migration.test',)).fetchone()[0]
    tenant = db.execute("INSERT INTO photo.tenants(name,owner_user_id,quotas,retention) VALUES('Upgrade preservation',%s,'{}','{}') RETURNING id", (user,)).fetchone()[0]
    before = db.execute('SELECT to_jsonb(t) FROM photo.tenants t WHERE id=%s', (tenant,)).fetchone()[0]
    baseline = db.execute("SELECT oid,proowner,proacl::text,prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure('photo.tenant_capabilities()')").fetchone()
    assert baseline is not None, 'Upgrade fixture must contain the existing capability function'
    for path in sorted((ROOT/'backend/db/migrations').glob('*.sql')):
        if int(path.name.split('_',1)[0]) > 2499:
            db.execute(path.read_text().split('-- +goose Down')[0])
    assert db.execute('SELECT to_jsonb(t) FROM photo.tenants t WHERE id=%s', (tenant,)).fetchone()[0] == before
    assert db.execute("SELECT oid,proowner,proacl::text,prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure('photo.tenant_capabilities()')").fetchone() == baseline
    # The tail intentionally changes capability logic (02546/02547/02560),
    # while preserving identity and permissions checked above.
    definition = db.execute("SELECT pg_get_functiondef('photo.tenant_capabilities()'::regprocedure)").fetchone()[0]
    for predicate in ("semantic.preview_local_exposure", "editor.effects_seal",
                      "editor.layers_precompose", "w.execution_pool='signer'",
                      "w.signer_qualified_until>clock_timestamp()"):
        assert predicate in definition, predicate
    assert_security_catalog(db)
    # Forward-only rejection leaves tenant state intact under a savepoint.
    guarded = ROOT/'backend/db/migrations/00001_global_auth.sql'
    with pytest.raises(Exception, match='Forward-only'):
        with db.transaction():
            db.execute(guarded.read_text().split('-- +goose Down',1)[1])
    assert db.execute('SELECT to_jsonb(t) FROM photo.tenants t WHERE id=%s', (tenant,)).fetchone()[0] == before


@pytest.mark.migration_head(2448)
def test_culling_upgrade_preserves_pending_guard_and_extends_only_route_domain(db):
    pending = db.execute("""
        SELECT oid,pg_get_constraintdef(oid) FROM pg_constraint
        WHERE conrelid='photo.reviews'::regclass AND conname='reviews_pending_empty'
    """).fetchone()
    assert pending is not None
    # Both checks mention decision_route; matching source text alone is unsafe.
    assert db.execute("""
        SELECT count(*) FROM pg_constraint WHERE conrelid='photo.reviews'::regclass
        AND contype='c' AND pg_get_constraintdef(oid) LIKE '%decision_route%'
    """).fetchone()[0] >= 2
    migration = ROOT/'backend/db/migrations/02449_apply_culling_selection.sql'
    db.execute(migration.read_text().split('-- +goose Down')[0])
    assert db.execute("""
        SELECT oid,pg_get_constraintdef(oid) FROM pg_constraint
        WHERE conrelid='photo.reviews'::regclass AND conname='reviews_pending_empty'
    """).fetchone() == pending
    expression = db.execute("""
        SELECT pg_get_expr(conbin,conrelid) FROM pg_constraint
        WHERE conrelid='photo.reviews'::regclass AND conname='reviews_decision_route_domain'
    """).fetchone()[0]
    for value, expected in (('selection', True), ('unregistered-route', False)):
        query = sql.SQL('SELECT ({}) FROM (VALUES (%s::text)) AS input(decision_route)').format(sql.SQL(expression))
        assert db.execute(query, (value,)).fetchone() == (expected,)


@pytest.mark.migration_head(2571)
def test_native_queue_upgrade_preserves_security_and_closed_config_predicates(db):
    import re
    names = ('native_admission', 'native_completed', 'face_admission', 'face_completed',
             'target_person_admission', 'target_person_sealed',
             'target_detector_admission', 'target_detector_sealed',
             'target_face_admission', 'target_face_sealed')
    signatures = [f'photo.lock_semantic_{name}_evidence(uuid,uuid,jsonb)' for name in names]
    def identity(signature):
        return db.execute('SELECT oid,proowner,proacl::text,prosecdef,proconfig FROM pg_proc WHERE oid=%s::regprocedure', (signature,)).fetchone()
    before = {s: identity(s) for s in signatures}
    migration = ROOT/'backend/db/migrations/02572_native_queue_evidence_compatibility.sql'
    db.execute(migration.read_text().split('-- +goose Down')[0])
    for signature in signatures:
        assert identity(signature) == before[signature], signature
        definition = db.execute('SELECT pg_get_functiondef(%s::regprocedure)', (signature,)).fetchone()[0]
        match = re.search(r"\(config-ARRAY\['queue_class','project_id',([^]]+)\]\)<>'\{\}' OR \(config \? 'queue_class' AND config->'queue_class' IS DISTINCT FROM '\"batch\"'::jsonb\)", definition)
        assert match, signature
        marker = 'v27_detector_prepare' if match[1] == 'config_key' else match[1].strip("'")
        # Execute the installed rejection expression, including JSON null/type
        # edge cases; absence preserves saved legacy config hashes.
        expression = match[0].replace('config_key', "'"+marker+"'")
        for extra, rejected in (({}, False), ({'queue_class': 'batch'}, False),
                                ({'queue_class': 'interactive'}, True),
                                ({'queue_class': None}, True),
                                ({'queue_class': 1}, True),
                                ({'queue_class': ['batch']}, True),
                                ({'unregistered': True}, True)):
            import json
            config = {'project_id': 'fixture', marker: {}, **extra}
            query = sql.SQL('SELECT ({}) FROM (VALUES (%s::jsonb)) AS input(config)').format(sql.SQL(expression))
            assert db.execute(query, (json.dumps(config),)).fetchone() == (rejected,), (signature, extra)
    # This fixture applies02572 only:02573 is checked by fresh/current-tail
    # fixtures, while its absence here must preserve the pre02573 ACL.
    assert_security_catalog(db, policy_row_lock_installed=False)
