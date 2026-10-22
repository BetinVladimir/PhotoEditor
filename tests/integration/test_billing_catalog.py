"""Real PostgreSQL checks. Only isolated databases from the existing harness are used."""
from pathlib import Path
from uuid import uuid4
import pytest
import psycopg

ROOT = Path(__file__).resolve().parents[2]
NEW = ['02598_billing_catalog_scaffold.sql', '02599_billing_catalog_seed.sql']
pytestmark = pytest.mark.migration_head(17)

@pytest.fixture
def catalog(db):
    for name in NEW:
        db.execute((ROOT / 'backend/db/migrations' / name).read_text().split('-- +goose Down')[0])
    return db

def rejects(db, statement, params=(), error=psycopg.errors.CheckViolation):
    with pytest.raises(error):
        with db.transaction():
            db.execute(statement, params)

def test_seed_exact_catalog_and_replay(catalog):
    db = catalog
    expected = {'billing_plans': 7, 'billing_plan_versions': 7, 'billing_plan_prices': 6,
                'billing_plan_limits': 36, 'billing_resource_types': 8, 'billing_topup_offers': 45,
                'billing_provider_accounts': 0, 'billing_provider_prices': 0,
                'billing_operation_policies': 0, 'billing_admission_policies': 2}
    for table, count in expected.items():
        assert db.execute('SELECT count(*) FROM photo.' + table).fetchone()[0] == count
    assert db.execute("SELECT amount_net_minor FROM photo.billing_plan_prices p JOIN photo.billing_plan_versions v ON v.id=p.plan_version_id JOIN photo.billing_plans b ON b.id=v.plan_id ORDER BY b.sort_order").fetchall() == [(0,), (900,), (1900,), (4900,), (8900,), (19900,)]
    assert db.execute("SELECT grant_period FROM photo.billing_plan_limits l JOIN photo.billing_plan_versions v ON v.id=l.plan_version_id JOIN photo.billing_plans p ON p.id=v.plan_id WHERE p.code='trial' AND resource_code='outgoing_bytes'").fetchone() == ('month',)
    assert db.execute("SELECT resource_code,sum(quantity),count(*) FROM photo.billing_topup_offers GROUP BY resource_code ORDER BY resource_code").fetchall() == [('outgoing_bytes', 6*10**12, 6), ('standard_photo', 8000, 15), ('storage_bytes', 21600*10**9, 24)]
    db.execute((ROOT / 'backend/db/migrations' / NEW[1]).read_text().split('-- +goose Down')[0])
    assert db.execute('SELECT count(*) FROM photo.billing_topup_offers').fetchone()[0] == 45
    db.execute("UPDATE photo.billing_plans SET name_key='corrupted' WHERE code='start'")
    rejects(db, (ROOT / 'backend/db/migrations' / NEW[1]).read_text().split('-- +goose Down')[0], error=psycopg.errors.RaiseException)

@pytest.mark.parametrize('sql', [
    "UPDATE photo.billing_plan_versions SET status='published'",
    "UPDATE photo.billing_plans SET self_service_enabled=true WHERE code='start'",
    "UPDATE photo.billing_topup_offers SET is_active=true",
    "UPDATE photo.billing_admission_policies SET enabled=true",
    "UPDATE photo.billing_topup_offers SET quantity=0",
    "UPDATE photo.billing_plan_prices SET amount_net_minor=-1",
    "UPDATE photo.billing_plan_prices SET currency='USD'",
    "UPDATE photo.billing_plan_limits SET enum_value='bad' WHERE resource_code='standard_photo'",
    "UPDATE photo.billing_plan_limits SET enum_value='automatic' WHERE resource_code='retention_mode'",
    "UPDATE photo.billing_topup_offers SET ttl_days=NULL WHERE expiry_policy='fixed_ttl'",
    "UPDATE photo.billing_topup_offers SET bundled_egress_bytes=1 WHERE resource_code='standard_photo'",
    "UPDATE photo.billing_admission_policies SET cache_ttl_seconds=lease_seconds+1",
    "UPDATE photo.billing_admission_policies SET optimistic_allowance=chunk_quantity+1",
])
def test_invalid_catalog_values(catalog, sql):
    rejects(catalog, sql)

def test_scope_and_typed_foreign_keys(catalog):
    rejects(catalog, "UPDATE photo.billing_plan_prices SET catalog_scope='live'", error=psycopg.errors.ForeignKeyViolation)
    rejects(catalog, "UPDATE photo.billing_plan_limits SET value_type='enum',integer_value=NULL,enum_value='1' WHERE resource_code='standard_photo'", error=psycopg.errors.ForeignKeyViolation)
    rejects(catalog, "INSERT INTO photo.billing_plan_prices SELECT gen_random_uuid(),created_at,plan_version_id,catalog_scope,version+1,currency,amount_net_minor,interval,tax_behavior,is_default FROM photo.billing_plan_prices LIMIT 1", error=psycopg.errors.UniqueViolation)

def account(db, reference=None):
    return db.execute("INSERT INTO photo.billing_provider_accounts(provider,environment,catalog_scope,external_account_id,sandbox_reference,api_version,webhook_format) VALUES ('stripe','test','sandbox','acct_fixture',%s,'test-fixture-version','snapshot') RETURNING id", (reference,)).fetchone()[0]

def test_stripe_test_namespace_and_bindings(catalog):
    db = catalog
    first = account(db)
    rejects(db, "INSERT INTO photo.billing_provider_accounts(provider,environment,catalog_scope,external_account_id,api_version,webhook_format) VALUES ('stripe','test','sandbox','acct_fixture','fixture','snapshot')", error=psycopg.errors.UniqueViolation)
    second = account(db, 'other-isolated-sandbox')
    rejects(db, "UPDATE photo.billing_provider_accounts SET environment='live'")
    rejects(db, "UPDATE photo.billing_provider_accounts SET enabled=true")
    price = db.execute('SELECT id FROM photo.billing_plan_prices LIMIT 1').fetchone()[0]
    offer = db.execute('SELECT id FROM photo.billing_topup_offers LIMIT 1').fetchone()[0]
    stmt = "INSERT INTO photo.billing_provider_prices(provider_account_id,catalog_scope,price_id,offer_id,external_product_id,external_price_id,verified_at) VALUES (%s,'sandbox',%s,%s,'prod_fixture','price_fixture',now())"
    db.execute(stmt, (first, price, None))
    db.execute(stmt, (second, price, None))
    rejects(db, stmt, (first, price, None), psycopg.errors.UniqueViolation)
    rejects(db, stmt, (second, price, offer))
    rejects(db, stmt, (second, None, None))
    rejects(db, stmt, (uuid4(), price, None), psycopg.errors.ForeignKeyViolation)

def test_operation_registry_identity(catalog):
    stmt = "INSERT INTO photo.billing_operation_policies(operation_code,operation_version,charge_context,policy_version,billing_class,units_per_success,success_contract,rerun_policy,effective_from,policy_snapshot_hash) VALUES (%s,'1','standalone',1,'disabled',0,'not_qualified','none',now(),decode(repeat('00',32),'hex'))"
    catalog.execute(stmt, ('image.color',))
    rejects(catalog, stmt, ('not.a.real.operation',), psycopg.errors.ForeignKeyViolation)
    rejects(catalog, "UPDATE photo.billing_operation_policies SET billing_class='paid_operation',units_per_success=1")

@pytest.mark.parametrize('role', ['photo_api','photo_auth','photo_portal','photo_worker','photo_scheduler','photo_publisher','photo_maintenance','photo_fn_owner'])
def test_runtime_roles_cannot_read_or_mutate_catalog(catalog, role):
    db = catalog
    with db.transaction():
        db.execute('SET LOCAL ROLE ' + role)
        if role == 'photo_fn_owner':
            assert db.execute('SELECT count(*) FROM photo.billing_plans').fetchone()[0] == 0
        else:
            rejects(db, 'SELECT * FROM photo.billing_plans', error=psycopg.errors.InsufficientPrivilege)
        rejects(db, "INSERT INTO photo.billing_plans(code,name_key,description_key,sort_order) VALUES ('injected','x','x',0)", error=psycopg.errors.InsufficientPrivilege)
        rejects(db, "UPDATE photo.billing_plans SET is_public=true", error=psycopg.errors.InsufficientPrivilege)
        rejects(db, "DELETE FROM photo.billing_plans", error=psycopg.errors.InsufficientPrivilege)

@pytest.mark.migration_head(2599)
def test_catalog_at_full_migration_head(db):
    # Validates additive compatibility with every existing source migration, not a production DB.
    assert db.execute('SELECT count(*) FROM photo.billing_plans').fetchone() == (7,)
    assert db.execute("SELECT count(*) FROM photo.billing_plan_versions WHERE status<>'draft'").fetchone() == (0,)
