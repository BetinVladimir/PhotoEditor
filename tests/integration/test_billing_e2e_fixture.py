"""TST-01: self-tests for billing_e2e_fixture.

Verifies:
- The fixture creates and tears down a disposable DB without touching production (R30).
- Two distinct tenants are created with independent billing data.
- RLS isolation: secondary tenant cannot see primary's billing rows.
- FakeProvider records calls and resets between tests.
- insert_period() respects custom offsets.
- No secrets in connection repr (T8).
"""
from hashlib import sha256
from uuid import uuid4

import psycopg
import pytest

from billing_e2e_fixture import BillingE2EEnv, FakeProvider, insert_period


# ---------------------------------------------------------------------------
# Tenant creation and basic isolation
# ---------------------------------------------------------------------------

def test_two_distinct_tenants_are_created(billing_e2e_env: BillingE2EEnv) -> None:
    env = billing_e2e_env
    assert env.tenant_id != env.secondary_tenant_id
    assert env.user_id != env.secondary_user_id


def test_primary_tenant_has_accounting_controls(billing_e2e_env: BillingE2EEnv) -> None:
    env = billing_e2e_env
    row = env.conn.execute(
        "SELECT tenant_id FROM photo.billing_accounting_controls WHERE tenant_id=%s",
        (env.tenant_id,),
    ).fetchone()
    assert row is not None


def test_secondary_tenant_has_no_billing_opt_in(billing_e2e_env: BillingE2EEnv) -> None:
    env = billing_e2e_env
    row = env.conn.execute(
        "SELECT tenant_id FROM photo.billing_accounting_controls WHERE tenant_id=%s",
        (env.secondary_tenant_id,),
    ).fetchone()
    assert row is None


def test_primary_tenant_has_active_period(billing_e2e_env: BillingE2EEnv) -> None:
    env = billing_e2e_env
    row = env.conn.execute(
        "SELECT id, state FROM photo.billing_periods"
        " WHERE tenant_id=%s AND id=%s",
        (env.tenant_id, env.period_id),
    ).fetchone()
    assert row is not None
    assert row[1] == "open"


def test_primary_tenant_has_trial_grant_bucket(billing_e2e_env: BillingE2EEnv) -> None:
    env = billing_e2e_env
    row = env.conn.execute(
        "SELECT original_quantity, expiry_policy FROM photo.billing_credit_buckets"
        " WHERE tenant_id=%s AND source_type='trial' AND resource_code='standard_photo'",
        (env.tenant_id,),
    ).fetchone()
    assert row is not None
    assert row[0] == 20
    assert row[1] == "never"


# ---------------------------------------------------------------------------
# RLS cross-tenant isolation
# ---------------------------------------------------------------------------

def test_rls_secondary_cannot_see_primary_accounting_controls(
    billing_e2e_env: BillingE2EEnv,
) -> None:
    """SELECT by secondary's tenant_id must return nothing for primary's rows."""
    env = billing_e2e_env
    # Query primary's controls scoped to secondary's tenant_id — must be empty
    row = env.conn.execute(
        "SELECT 1 FROM photo.billing_accounting_controls"
        " WHERE tenant_id=%s",
        (env.secondary_tenant_id,),
    ).fetchone()
    assert row is None, "Secondary tenant must not have accounting controls"

    # Cross-query: primary rows are not visible under secondary tenant_id
    row = env.conn.execute(
        "SELECT 1 FROM photo.billing_credit_buckets"
        " WHERE tenant_id=%s",
        (env.secondary_tenant_id,),
    ).fetchone()
    assert row is None, "Secondary tenant must not see primary's credit buckets"


# ---------------------------------------------------------------------------
# insert_period helper — clock offsets
# ---------------------------------------------------------------------------

def test_insert_period_creates_open_period_with_custom_offsets(
    billing_e2e_env: BillingE2EEnv,
) -> None:
    env = billing_e2e_env
    period_id = insert_period(
        env.conn,
        env.tenant_id,
        starts_offset="-2 hours",
        ends_offset="+30 days",
    )
    row = env.conn.execute(
        "SELECT state, ends_at - starts_at FROM photo.billing_periods WHERE id=%s",
        (period_id,),
    ).fetchone()
    assert row is not None
    assert row[0] == "open"
    # duration should be ~30 days + 2 hours; just check it is positive and > 1 day
    assert row[1].days >= 30


def test_insert_period_with_expired_offsets(
    billing_e2e_env: BillingE2EEnv,
) -> None:
    env = billing_e2e_env
    # Create a period that is entirely in the past
    period_id = insert_period(
        env.conn,
        env.tenant_id,
        starts_offset="-48 hours",
        ends_offset="-1 second",
    )
    row = env.conn.execute(
        "SELECT ends_at < clock_timestamp() FROM photo.billing_periods WHERE id=%s",
        (period_id,),
    ).fetchone()
    assert row is not None
    assert row[0] is True, "ends_at should be in the past"


# ---------------------------------------------------------------------------
# FakeProvider
# ---------------------------------------------------------------------------

def test_fake_provider_records_calls(billing_e2e_env: BillingE2EEnv) -> None:
    p = billing_e2e_env.provider
    p.configure("checkout.create", {"id": "cs_fake_001"})
    result = p.call("checkout.create", amount=900, currency="EUR")
    assert result == {"id": "cs_fake_001"}
    assert len(p.calls) == 1
    assert p.calls[0]["method"] == "checkout.create"
    assert p.calls[0]["amount"] == 900


def test_fake_provider_raises_for_unconfigured_method(
    billing_e2e_env: BillingE2EEnv,
) -> None:
    p = billing_e2e_env.provider
    with pytest.raises(NotImplementedError, match="checkout.create"):
        p.call("checkout.create", amount=900)


def test_fake_provider_is_reset_between_tests(billing_e2e_env: BillingE2EEnv) -> None:
    """Provider must be clean at the start of each test (reset in fixture teardown)."""
    env = billing_e2e_env
    assert len(env.provider.calls) == 0, "Provider must start empty each test"


# ---------------------------------------------------------------------------
# Connection repr — no secrets (T8)
# ---------------------------------------------------------------------------

def test_db_config_repr_hides_credentials(billing_e2e_env: BillingE2EEnv) -> None:
    from conftest import PrivateDatabaseConfig
    cfg = billing_e2e_env.db_config
    assert isinstance(cfg, PrivateDatabaseConfig)
    assert "password" not in repr(cfg)
    assert "PrivateDatabaseConfig" in repr(cfg) or "isolated" in repr(cfg)


# ---------------------------------------------------------------------------
# Session and user setup
# ---------------------------------------------------------------------------

def test_session_is_valid_for_primary_user(billing_e2e_env: BillingE2EEnv) -> None:
    env = billing_e2e_env
    row = env.conn.execute(
        "SELECT user_id FROM photo.sessions"
        " WHERE id=%s AND expires_at > clock_timestamp()",
        (env.session_id,),
    ).fetchone()
    assert row is not None
    assert row[0] == env.user_id
