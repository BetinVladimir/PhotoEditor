"""Actual admission and accounting SQL; metadata fixtures, no physical photo QA."""
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb

FAMILIES = ('exposure','removal','background','retouch')


def saved_preview(db, family):
    if family == 'retouch':
        from retouch_preview_admission_fixture import prepared_retouch_preview_fixture
        item = prepared_retouch_preview_fixture(db)
        return item['args'][0],item['run']
    if family == 'background':
        from test_v27_background_cleanup_preview_quota_authority import quota_fixture,reserve
        args,run,source,profile = quota_fixture(db)
    else:
        if family == 'exposure':
            from exposure_preview_activation_fixture import prepared_params_fixture,reserve
        else:
            from removal_preview_activation_fixture import prepared_params_fixture,reserve
        args,run,scope = prepared_params_fixture(db)
        source,profile = scope['source_body'],scope['preview_profile']
        source = source.decode('utf-8') if isinstance(source, bytes) else source
    # Positive lifecycle setup owns capacity/accounting; helper callers elsewhere
    # retain their own missing-usage and capacity-refusal scenarios.
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT(tenant_id) DO NOTHING', (args[0],))
    db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas, '{storage_bytes}', '107374182400'::jsonb) WHERE id=%s", (args[0],))
    assert reserve(db,args,run,source,profile) is True
    return args[0],run


def quota(db, tenant, run):
    return db.execute("SELECT state,bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(tenant,run)).fetchone()


def reserved(db, tenant):
    return db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()[0]


def release(db, tenant, run):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        result = db.execute('SELECT photo.release_semantic_preapply_preview_quota(%s,%s)',(tenant,run)).fetchone()[0]
        db.execute('RESET ROLE')
        return result


@pytest.mark.parametrize('family',FAMILIES)
@pytest.mark.parametrize('status',('completed','failed','cancelled'))
def test_all_admitted_preview_families_release_terminal_quota_once(db,family,status):
    tenant,run = saved_preview(db,family)
    amount = quota(db,tenant,run)[1]
    before = reserved(db,tenant)
    assert amount > 0 and release(db,tenant,run) is False  # draft is not terminal
    db.execute('UPDATE photo.runs SET status=%s WHERE tenant_id=%s AND id=%s',(status,tenant,run))
    assert release(db,tenant,run) is True
    assert quota(db,tenant,run) == ('released',0)
    assert reserved(db,tenant) == before-amount
    assert release(db,tenant,run) is True
    assert reserved(db,tenant) == before-amount


@pytest.mark.parametrize('family',FAMILIES)
def test_cleaning_document_blocks_release_until_fenced_cleanup_receipt(db,family):
    tenant,run = saved_preview(db,family)
    db.execute("UPDATE photo.runs SET status='failed' WHERE tenant_id=%s AND id=%s",(tenant,run))
    document,token = uuid4(),uuid4()
    db.execute("""INSERT INTO photo.backend_documents(id,tenant_id,run_id,owner_id,purpose,
      artifact_id,token,sha256,bytes,bucket,state,cleanup_token,cleanup_lease_until)
      VALUES(%s,%s,%s,%s,'task_params',%s,%s,%s,2,'photo','cleaning',%s,clock_timestamp()+interval '1 minute')""",
      (document,tenant,run,uuid4(),uuid4(),uuid4(),'a'*64,token))
    db.execute('UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+2 WHERE tenant_id=%s',(tenant,))
    before = reserved(db,tenant)
    assert release(db,tenant,run) is False
    assert db.execute('SELECT photo.finish_document_cleanup(%s,%s)',(document,uuid4())).fetchone() == (False,)
    assert reserved(db,tenant) == before
    assert release(db,tenant,run) is False
    # This receipt models confirmed external cleanup; it is not S3 evidence.
    assert db.execute('SELECT photo.finish_document_cleanup(%s,%s)',(document,token)).fetchone() == (True,)
    amount = quota(db,tenant,run)[1]
    assert release(db,tenant,run) is True
    assert reserved(db,tenant) == before-2-amount


@pytest.mark.parametrize('family',FAMILIES)
def test_expired_reserved_draft_is_cancelled_and_released_only_after_terminal_settlement(db,family):
    tenant,run = saved_preview(db,family)
    db.execute("UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(tenant,run))
    amount = quota(db,tenant,run)[1]
    before = reserved(db,tenant)
    assert db.execute('SELECT photo.cancel_stale_semantic_preapply_preview()').fetchone() == (True,)
    assert db.execute('SELECT status,cancel_requested FROM photo.runs WHERE tenant_id=%s AND id=%s',(tenant,run)).fetchone() == ('cancelling',True)
    assert release(db,tenant,run) is False
    assert reserved(db,tenant) == before
    assert db.execute('SELECT photo.finalize_run_cancellation()').fetchone() == (1,)
    assert release(db,tenant,run) is True
    assert reserved(db,tenant) == before-amount


def test_unrelated_preview_profile_cannot_use_semantic_quota_release(db):
    tenant,run = saved_preview(db,'removal')
    db.execute("UPDATE photo.runs SET status='failed',config_snapshot='{}'::jsonb WHERE tenant_id=%s AND id=%s",(tenant,run))
    before = quota(db,tenant,run),reserved(db,tenant)
    assert release(db,tenant,run) is False
    assert (quota(db,tenant,run),reserved(db,tenant)) == before


@pytest.mark.parametrize('expired_sidecar',[False,True])
def test_retouch_finalizer_requires_current_canonical_bundle_then_releases_quota(db,expired_sidecar):
    from retouch_preview_bundle_fixture import retouch_bundle_fixture
    item = retouch_bundle_fixture(db,admitted=True)
    tenant,run = item['args'][0],item['run']
    db.execute("UPDATE photo.runs SET status='running' WHERE tenant_id=%s AND id=%s",(tenant,run))
    if expired_sidecar:
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND id=%s",(tenant,item['refs']['before'].artifact_id))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        result = db.execute('SELECT photo.finalize_next_semantic_retouch_preview()').fetchone()[0]
        db.execute('RESET ROLE')
    assert result is (not expired_sidecar)
    assert db.execute('SELECT status FROM photo.runs WHERE tenant_id=%s AND id=%s',(tenant,run)).fetchone() == (('running' if expired_sidecar else 'completed'),)
    if expired_sidecar:
        assert release(db,tenant,run) is False
    else:
        before,amount = reserved(db,tenant),quota(db,tenant,run)[1]
        assert release(db,tenant,run) is True
        assert reserved(db,tenant) == before-amount


def test_mixed_semantic_preview_families_cannot_release_an_ambiguous_reservation(db):
    tenant,run = saved_preview(db,'removal')
    db.execute("UPDATE photo.runs SET status='failed',config_snapshot=jsonb_set(config_snapshot,'{semantic_retouch_preview}','{}'::jsonb) WHERE tenant_id=%s AND id=%s",(tenant,run))
    before = quota(db,tenant,run),reserved(db,tenant)
    assert release(db,tenant,run) is False
    assert db.execute('SELECT photo.release_next_semantic_preapply_preview_quota()').fetchone() == (False,)
    assert (quota(db,tenant,run),reserved(db,tenant)) == before


def test_invalid_old_principal_does_not_starve_later_valid_terminal_reservation(db):
    first_tenant,first_run = saved_preview(db,'removal')
    tenant,run = saved_preview(db,'retouch')
    for t,r in ((first_tenant,first_run),(tenant,run)):
        db.execute("UPDATE photo.runs SET status='failed' WHERE tenant_id=%s AND id=%s",(t,r))
    db.execute("UPDATE photo.runs SET created_at=clock_timestamp()-interval '1 day' WHERE tenant_id=%s AND id=%s",(first_tenant,first_run))
    db.execute("UPDATE photo.quota_reservations SET files=1 WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(first_tenant,first_run))
    blocked = quota(db,first_tenant,first_run),reserved(db,first_tenant)
    assert db.execute('SELECT photo.release_next_semantic_preapply_preview_quota()').fetchone() == (True,)
    assert quota(db,tenant,run) == ('released',0)
    assert (quota(db,first_tenant,first_run),reserved(db,first_tenant)) == blocked


@pytest.mark.migration_head(2528)
def test_upgrade_2529_preserves_existing_reserved_drafts_and_accounting(db):
    from pathlib import Path
    items = [saved_preview(db,family) for family in FAMILIES]
    def snapshot():
        return [(db.execute('SELECT to_jsonb(r) FROM photo.runs r WHERE tenant_id=%s AND id=%s',(tenant,run)).fetchone(),
                 quota(db,tenant,run),reserved(db,tenant)) for tenant,run in items]
    before = snapshot()
    path = Path(__file__).resolve().parents[2]/'backend/db/migrations/02529_semantic_preview_lifecycle_quota.sql'
    db.execute(path.read_text().split('-- +goose Down')[0])
    assert snapshot() == before
    for tenant,run in items:
        db.execute("UPDATE photo.runs SET status='failed' WHERE tenant_id=%s AND id=%s",(tenant,run))
        assert release(db,tenant,run) is True
        assert quota(db,tenant,run) == ('released',0)


def test_semantic_lifecycle_installation_acl_is_scheduler_only(db):
    roles = ('photo_api','photo_auth','photo_portal','photo_worker','photo_scheduler',
             'photo_gateway','photo_maintenance','photo_publisher','photo_mail','photo_semantic_admission')
    for signature,public_scheduler in (
        ('photo.release_semantic_preapply_preview_quota(uuid,uuid)',True),
        ('photo.release_next_semantic_preapply_preview_quota()',True),
        ('photo.cancel_stale_semantic_preapply_preview()',True),
        ('photo.finalize_next_semantic_preapply_preview()',True),
        ('photo.finalize_next_semantic_retouch_preview()',True),
        ('photo.semantic_preview_lifecycle_current(uuid,uuid)',False),
        ('photo.finalize_next_semantic_preapply_preview_before_retouch()',False)):
        row = db.execute("SELECT pg_get_userbyid(proowner),prosecdef,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)",(signature,)).fetchone()
        assert row is not None and row[0]=='photo_fn_owner' and row[1] is True
        assert any(value in row[2] for value in ('search_path=pg_catalog,photo','search_path=pg_catalog, photo'))
        for role in roles:
            assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone() == (public_scheduler and role=='photo_scheduler',)


def test_two_release_schedulers_serialize_one_accounting_decrement(database):
    import psycopg
    from threading import Barrier
    from concurrent.futures import ThreadPoolExecutor
    with psycopg.connect(**database) as seed:
        tenant,run = saved_preview(seed,'removal')
        seed.execute("UPDATE photo.runs SET status='failed' WHERE tenant_id=%s AND id=%s",(tenant,run))
        before,amount = reserved(seed,tenant),quota(seed,tenant,run)[1]
    barrier = Barrier(2)
    def settle():
        with psycopg.connect(**database) as connection:
            connection.execute("SET LOCAL statement_timeout='5s'")
            barrier.wait(timeout=5)
            return release(connection,tenant,run)
    with ThreadPoolExecutor(max_workers=2) as workers:
        futures = [workers.submit(settle) for _ in range(2)]
        assert [future.result(timeout=10) for future in futures] == [True,True]
    with psycopg.connect(**database) as observer:
        assert quota(observer,tenant,run) == ('released',0)
        assert reserved(observer,tenant) == before-amount
