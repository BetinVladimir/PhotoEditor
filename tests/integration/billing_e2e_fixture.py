"""Shared fixture for test_billing_e2e_*.py tests.

Provides:
- A disposable PostgreSQL database with all migrations applied (via `database` fixture).
- Two tenants: primary (with billing accounting opt-in and an active period + trial grant)
  and secondary (bare, for RLS isolation checks).
- A FakeProvider stub that records calls and simulates provider events without network.
- Clock helpers: insert_period() accepts explicit timestamps so tests control timing.

No secrets are embedded or logged (T8). No physical/GPU/network operations.
"""
from __future__ import annotations

import dataclasses
from hashlib import sha256
from pathlib import Path
from typing import Any
from uuid import UUID, uuid4

import psycopg
import pytest

ROOT = Path(__file__).resolve().parents[2]


# ---------------------------------------------------------------------------
# FakeProvider — Stripe stub for e2e tests
# ---------------------------------------------------------------------------

class FakeProvider:
    """Minimal Stripe-like provider stub.

    Records all outbound calls; can be configured to return specific responses
    before real Stripe integration (OPEN-14) is wired up.
    """

    def __init__(self) -> None:
        self.calls: list[dict[str, Any]] = []
        self._responses: dict[str, Any] = {}

    def configure(self, method: str, response: Any) -> None:
        self._responses[method] = response

    def call(self, method: str, **kwargs: Any) -> Any:
        entry = {"method": method, **kwargs}
        self.calls.append(entry)
        if method in self._responses:
            return self._responses[method]
        raise NotImplementedError(
            f"FakeProvider: no configured response for '{method}'. "
            "Add one with configure(method, response) before calling."
        )

    def reset(self) -> None:
        self.calls.clear()
        self._responses.clear()


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _make_user(c: psycopg.Connection) -> UUID:
    return c.execute(
        "INSERT INTO photo.users(email_normalized) VALUES (%s) RETURNING id",
        (str(uuid4()) + "@e2e.invalid",),
    ).fetchone()[0]


def _make_tenant(c: psycopg.Connection, name: str = "e2e tenant") -> tuple[UUID, UUID]:
    """Return (tenant_id, user_id)."""
    user_id = _make_user(c)
    with c.transaction():
        tenant_id = c.execute(
            "INSERT INTO photo.tenants(name,owner_user_id,quotas,retention)"
            " VALUES (%s,%s,'{}','{}') RETURNING id",
            (name, user_id),
        ).fetchone()[0]
        c.execute(
            "INSERT INTO photo.memberships(tenant_id,user_id,role,status)"
            " VALUES (%s,%s,'owner','active')",
            (tenant_id, user_id),
        )
    return tenant_id, user_id


def _make_session(c: psycopg.Connection, user_id: UUID) -> UUID:
    return c.execute(
        "INSERT INTO photo.sessions"
        "(user_id,refresh_hash,family_id,client_type,expires_at)"
        " VALUES (%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour')"
        " RETURNING id",
        (user_id, sha256(str(uuid4()).encode()).digest()),
    ).fetchone()[0]


def insert_period(
    c: psycopg.Connection,
    tenant_id: UUID,
    *,
    starts_offset: str = "-1 hour",
    ends_offset: str = "+1 day",
    anchor_kind: str = "trial_calendar",
) -> UUID:
    """Create a billing period with explicit server-relative offsets.

    Tests that need to control timing pass different offsets, e.g.
    ends_offset="-1 second" to create an already-expired period.
    """
    return c.execute(
        "INSERT INTO photo.billing_periods"
        "(tenant_id,starts_at,ends_at,anchor_kind)"
        " VALUES (%s, clock_timestamp() + %s::interval,"
        "        clock_timestamp() + %s::interval, %s)"
        " RETURNING id",
        (tenant_id, starts_offset, ends_offset, anchor_kind),
    ).fetchone()[0]


def _trial_grant(
    c: psycopg.Connection,
    tenant_id: UUID,
    quantity: int = 20,
    resource: str = "standard_photo",
) -> tuple[UUID, UUID]:
    """Insert a bootstrap grant and call billing_grant_bootstrap; return (grant_id, bucket_id)."""
    policy_hash = sha256(b"e2e-bootstrap").digest()
    grant_id = c.execute(
        "INSERT INTO photo.billing_bootstrap_grants"
        "(tenant_id,resource_code,reason,quantity,starts_at,expires_at,expiry_policy,policy_snapshot_hash)"
        " VALUES (%s,%s,'trial_once',%s,"
        "  clock_timestamp()-interval '1 hour', NULL,'never',%s)"
        " RETURNING id",
        (tenant_id, resource, quantity, policy_hash),
    ).fetchone()[0]
    bucket_id = c.execute(
        "SELECT photo.billing_grant_bootstrap(%s,%s)",
        (tenant_id, grant_id),
    ).fetchone()[0]
    return grant_id, bucket_id


# ---------------------------------------------------------------------------
# Dataclass returned by the fixture
# ---------------------------------------------------------------------------

@dataclasses.dataclass
class BillingE2EEnv:
    """Everything a billing e2e test needs."""

    conn: psycopg.Connection       # autocommit connection; each test owns its transaction
    db_config: dict                # psycopg connect kwargs (dbname is disposable)

    tenant_id: UUID                # primary tenant with billing opt-in
    user_id: UUID                  # owner of primary tenant
    session_id: UUID               # active session for primary user
    period_id: UUID                # current trial period for primary tenant

    secondary_tenant_id: UUID      # bare tenant for RLS isolation
    secondary_user_id: UUID

    provider: FakeProvider         # Stripe-like stub, reset between tests


# ---------------------------------------------------------------------------
# Fixture
# ---------------------------------------------------------------------------

@pytest.fixture
def billing_e2e_env(database) -> BillingE2EEnv:  # type: ignore[return]
    """Function-scoped billing e2e environment.

    Yields a BillingE2EEnv; the underlying disposable database is torn down
    automatically by the `database` fixture after this fixture's scope ends.
    """
    with psycopg.connect(**database, autocommit=True) as conn:
        # Primary tenant with billing opt-in
        tenant_id, user_id = _make_tenant(conn, "e2e primary")
        conn.execute(
            "INSERT INTO photo.billing_accounting_controls(tenant_id) VALUES (%s)",
            (tenant_id,),
        )
        period_id = insert_period(conn, tenant_id)
        _trial_grant(conn, tenant_id)

        session_id = _make_session(conn, user_id)

        # Secondary tenant — bare, for RLS cross-tenant isolation checks
        secondary_tenant_id, secondary_user_id = _make_tenant(conn, "e2e secondary")

        provider = FakeProvider()

        yield BillingE2EEnv(
            conn=conn,
            db_config=dict(database),
            tenant_id=tenant_id,
            user_id=user_id,
            session_id=session_id,
            period_id=period_id,
            secondary_tenant_id=secondary_tenant_id,
            secondary_user_id=secondary_user_id,
            provider=provider,
        )
        provider.reset()
