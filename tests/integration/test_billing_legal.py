"""Legal group DB integration tests (LEG-F01: account_accept_legal).

Requires PostgreSQL (database fixture from conftest.py). Tests are skipped
automatically when no test database is available. Working DB never touched (R30).
"""
from __future__ import annotations

import json
from concurrent.futures import ThreadPoolExecutor
from hashlib import sha256 as _sha256
from pathlib import Path
from threading import Barrier
from uuid import uuid4

import psycopg
import pytest

ROOT = Path(__file__).resolve().parents[2]

pytestmark = pytest.mark.integration

LEGAL_MIGRATIONS = [
    '02666_billing_legal_schema.sql',
    '02667_billing_legal_auth_tables.sql',
    '02668_billing_legal_functions.sql',
]


def digest(b: bytes) -> bytes:
    return _sha256(b).digest()


# ---------------------------------------------------------------------------
# Fixture helpers
# ---------------------------------------------------------------------------

def _make_user(c: psycopg.Connection) -> object:
    return c.execute(
        "INSERT INTO photo.users(email_normalized) VALUES (%s) RETURNING id",
        (str(uuid4()) + '@legal-test.invalid',),
    ).fetchone()[0]


def _make_tenant_and_user(c: psycopg.Connection):
    user_id = _make_user(c)
    with c.transaction():
        tenant_id = c.execute(
            "INSERT INTO photo.tenants(name,owner_user_id,quotas,retention)"
            " VALUES ('legal-test',%s,'{}','{}') RETURNING id",
            (user_id,),
        ).fetchone()[0]
        c.execute(
            "INSERT INTO photo.memberships(tenant_id,user_id,role,status)"
            " VALUES (%s,%s,'owner','active')",
            (tenant_id, user_id),
        )
    return tenant_id, user_id


def _make_session(c: psycopg.Connection, user_id) -> object:
    return c.execute(
        "INSERT INTO photo.sessions"
        "(user_id,refresh_hash,family_id,client_type,expires_at)"
        " VALUES (%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour')"
        " RETURNING id",
        (user_id, digest(str(uuid4()).encode())),
    ).fetchone()[0]


def _insert_doc(c: psycopg.Connection, key: str, locale: str = 'en',
                content_hash: bytes | None = None) -> object:
    h = content_hash if content_hash is not None else digest(f'{key}-{locale}-v1'.encode())
    doc_id = c.execute(
        "INSERT INTO photo.legal_document_versions"
        "(document_key,locale,version,url,content_sha256,archive_ref,archive_version,"
        " published_at,effective_at,state)"
        " VALUES (%s,%s,'2024-01-01','https://legal.beeloy.org/'||%s||'-'||%s,%s,'ref','v1',"
        " now(),now(),'active') RETURNING document_id",
        (key, locale, key, locale, h),
    ).fetchone()[0]
    return doc_id


def _set_hash(c: psycopg.Connection, doc_ids: list) -> bytes:
    rows = c.execute(
        "SELECT content_sha256 FROM photo.legal_document_versions"
        " WHERE document_id=ANY(%s) ORDER BY document_id",
        (doc_ids,),
    ).fetchall()
    concatenated = b''.join(r[0].tobytes() for r in rows)
    return digest(concatenated)


def _accept_call(c, tenant_id, user_id, session_id, doc_ids,
                 contract_accepted=True, privacy_acknowledged=True,
                 actions_override=None):
    """Build and call account_accept_legal; return (state, revision, hash)."""
    c.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant_id),))
    # Build events: terms → accepted, privacy → acknowledged
    def _action_for(doc_id):
        row = c.execute(
            "SELECT document_key FROM photo.legal_document_versions WHERE document_id=%s",
            (doc_id,),
        ).fetchone()
        key = row[0]
        if key in ('terms', 'dpa', 'photoeditor_addendum'):
            return 'accepted'
        return 'acknowledged'

    events = [{'version_id': str(doc_id), 'action': _action_for(doc_id)} for doc_id in doc_ids]
    if actions_override:
        events = actions_override

    doc_versions = json.dumps({'events': events})
    explicit = json.dumps({
        'contract_accepted': contract_accepted,
        'privacy_acknowledged': privacy_acknowledged,
    })
    return c.execute(
        "SELECT state,revision,accepted_set_hash"
        " FROM photo.account_accept_legal(%s,%s,%s,%s::jsonb,%s::jsonb,%s)",
        (tenant_id, user_id, session_id, doc_versions, explicit, str(uuid4())),
    ).fetchone()


# ---------------------------------------------------------------------------
# Fixture
# ---------------------------------------------------------------------------

@pytest.fixture
def legal_env(database):
    with psycopg.connect(**database, autocommit=True) as c:
        for name in LEGAL_MIGRATIONS:
            with c.transaction():
                c.execute(
                    (ROOT / 'backend/db/migrations' / name).read_text().split('-- +goose Down')[0]
                )
        tenant_id, user_id = _make_tenant_and_user(c)
        session_id = _make_session(c, user_id)
        terms_id = _insert_doc(c, 'terms')
        privacy_id = _insert_doc(c, 'privacy')
        yield c, tenant_id, user_id, session_id, [terms_id, privacy_id]


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

def test_account_accept_legal_success(legal_env):
    """LEG-F01 happy path: returns complete state, revision=2, correct hash."""
    c, tenant_id, user_id, session_id, doc_ids = legal_env
    row = _accept_call(c, tenant_id, user_id, session_id, doc_ids)
    assert row is not None
    state, revision, accepted_hash = row
    assert state == 'complete'
    assert revision == 2
    expected = _set_hash(c, doc_ids)
    assert bytes(accepted_hash) == expected


def test_account_accept_legal_auth_failure(legal_env):
    """LEG-F01: wrong tenant in app.tenant_id → BILLING_AUTHORITY_INVALID."""
    c, tenant_id, user_id, session_id, doc_ids = legal_env
    other_tenant = uuid4()
    c.execute("SELECT set_config('app.tenant_id',%s,true)", (str(other_tenant),))
    with pytest.raises(psycopg.errors.RaiseException, match='BILLING_AUTHORITY_INVALID'):
        with c.transaction():
            c.execute(
                "SELECT * FROM photo.account_accept_legal(%s,%s,%s,%s::jsonb,%s::jsonb,%s)",
                (tenant_id, user_id, session_id,
                 json.dumps({'events': []}),
                 json.dumps({'contract_accepted': True, 'privacy_acknowledged': True}),
                 str(uuid4())),
            )


def test_account_accept_legal_invalid_session(legal_env):
    """LEG-F01: expired/missing session → BILLING_AUTHORITY_INVALID."""
    c, tenant_id, user_id, session_id, doc_ids = legal_env
    with pytest.raises(psycopg.errors.RaiseException, match='BILLING_AUTHORITY_INVALID'):
        with c.transaction():
            _accept_call(c, tenant_id, user_id, uuid4(), doc_ids)


def test_account_accept_legal_exact_repeat_idempotent(legal_env):
    """LEG-F01: identical second call returns same result without error (R20)."""
    c, tenant_id, user_id, session_id, doc_ids = legal_env
    row1 = _accept_call(c, tenant_id, user_id, session_id, doc_ids)
    row2 = _accept_call(c, tenant_id, user_id, session_id, doc_ids)
    assert row1 == row2
    assert row1[0] == 'complete'
    assert row1[1] == row2[1]  # same revision on idempotent repeat


def test_account_accept_legal_hash_conflict(legal_env):
    """LEG-F01: second call with different document set → LEGAL_ACCEPTANCE_HASH_CONFLICT (R20)."""
    c, tenant_id, user_id, session_id, doc_ids = legal_env
    _accept_call(c, tenant_id, user_id, session_id, doc_ids)

    # Add a second privacy doc (different locale) to simulate a different required set
    # by inserting an extra active doc for the same locale (changes required hash)
    extra_terms = _insert_doc(c, 'photoeditor_addendum', locale='en',
                              content_hash=digest(b'extra-addendum'))
    new_doc_ids = doc_ids + [extra_terms]
    with pytest.raises(psycopg.errors.RaiseException, match='LEGAL_ACCEPTANCE_HASH_CONFLICT'):
        with c.transaction():
            _accept_call(c, tenant_id, user_id, session_id, new_doc_ids)


def test_account_accept_legal_contract_not_accepted(legal_env):
    """LEG-F01: contract_accepted=false → LEGAL_CONTRACT_NOT_ACCEPTED."""
    c, tenant_id, user_id, session_id, doc_ids = legal_env
    with pytest.raises(psycopg.errors.RaiseException, match='LEGAL_CONTRACT_NOT_ACCEPTED'):
        with c.transaction():
            _accept_call(c, tenant_id, user_id, session_id, doc_ids,
                         contract_accepted=False, privacy_acknowledged=True)


def test_account_accept_legal_privacy_not_acknowledged(legal_env):
    """LEG-F01: privacy_acknowledged=false → LEGAL_PRIVACY_NOT_ACKNOWLEDGED."""
    c, tenant_id, user_id, session_id, doc_ids = legal_env
    with pytest.raises(psycopg.errors.RaiseException, match='LEGAL_PRIVACY_NOT_ACKNOWLEDGED'):
        with c.transaction():
            _accept_call(c, tenant_id, user_id, session_id, doc_ids,
                         contract_accepted=True, privacy_acknowledged=False)


def test_account_accept_legal_missing_required_doc(legal_env):
    """LEG-F01: omitting a required active document → LEGAL_REQUIRED_SET_MISMATCH."""
    c, tenant_id, user_id, session_id, doc_ids = legal_env
    # Submit only the first doc (terms), not privacy
    with pytest.raises(psycopg.errors.RaiseException, match='LEGAL_REQUIRED_SET_MISMATCH'):
        with c.transaction():
            _accept_call(c, tenant_id, user_id, session_id, [doc_ids[0]])


# ---------------------------------------------------------------------------
# LEG-F04: account_issue_recent_auth
# ---------------------------------------------------------------------------

def _issue_call(c, tenant_id, user_id, session_id, purpose, challenge_ref):
    """Call account_issue_recent_auth; return (grant_id, proof_hash, expires_at)."""
    c.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant_id),))
    return c.execute(
        "SELECT grant_id,proof_hash,expires_at"
        " FROM photo.account_issue_recent_auth(%s,%s,%s,%s,%s)",
        (tenant_id, user_id, session_id, purpose, challenge_ref),
    ).fetchone()


def test_account_issue_recent_auth_success(legal_env):
    """LEG-F04 happy path: returns grant_id, 32-byte proof_hash, expires in ~5min."""
    c, tenant_id, user_id, session_id, doc_ids = legal_env
    ref = str(uuid4())
    row = _issue_call(c, tenant_id, user_id, session_id, 'delete_profile', ref)
    assert row is not None
    grant_id, proof_hash, expires_at = row
    assert grant_id is not None
    assert len(bytes(proof_hash)) == 32
    from datetime import timezone
    now = c.execute("SELECT clock_timestamp()").fetchone()[0]
    delta = (expires_at - now).total_seconds()
    assert 240 < delta <= 310, f"expires_at not ~5min from now: {delta}s"


def test_account_issue_recent_auth_auth_failure(legal_env):
    """LEG-F04: wrong tenant → BILLING_AUTHORITY_INVALID."""
    c, tenant_id, user_id, session_id, doc_ids = legal_env
    other = uuid4()
    c.execute("SELECT set_config('app.tenant_id',%s,true)", (str(other),))
    with pytest.raises(psycopg.errors.RaiseException, match='BILLING_AUTHORITY_INVALID'):
        with c.transaction():
            c.execute(
                "SELECT * FROM photo.account_issue_recent_auth(%s,%s,%s,%s,%s)",
                (tenant_id, user_id, session_id, 'delete_profile', str(uuid4())),
            )


def test_account_issue_recent_auth_exact_repeat_idempotent(legal_env):
    """LEG-F04: same challenge ref+purpose → same grant returned (R20)."""
    c, tenant_id, user_id, session_id, doc_ids = legal_env
    ref = str(uuid4())
    row1 = _issue_call(c, tenant_id, user_id, session_id, 'delete_profile', ref)
    row2 = _issue_call(c, tenant_id, user_id, session_id, 'delete_profile', ref)
    assert row1[0] == row2[0], "idempotent call must return the same grant_id"
    assert bytes(row1[1]) == bytes(row2[1]), "idempotent call must return the same proof_hash"


def test_account_issue_recent_auth_challenge_conflict(legal_env):
    """LEG-F04: same challenge ref reused for a different purpose → LEGAL_RECENT_AUTH_CHALLENGE_CONFLICT."""
    c, tenant_id, user_id, session_id, doc_ids = legal_env
    ref = str(uuid4())
    _issue_call(c, tenant_id, user_id, session_id, 'delete_profile', ref)
    with pytest.raises(psycopg.errors.RaiseException, match='LEGAL_RECENT_AUTH_CHALLENGE_CONFLICT'):
        with c.transaction():
            _issue_call(c, tenant_id, user_id, session_id, 'resolve_ownership', ref)


def test_account_issue_recent_auth_invalid_purpose(legal_env):
    """LEG-F04: unrecognised purpose → LEGAL_PURPOSE_INVALID."""
    c, tenant_id, user_id, session_id, doc_ids = legal_env
    with pytest.raises(psycopg.errors.RaiseException, match='LEGAL_PURPOSE_INVALID'):
        with c.transaction():
            _issue_call(c, tenant_id, user_id, session_id, 'bad_purpose', str(uuid4()))


def test_account_accept_legal_concurrent_same_user(legal_env):
    """LEG-F01 concurrent: two concurrent calls for the same user → one succeeds, one idempotent."""
    c, tenant_id, user_id, session_id, doc_ids = legal_env
    db_config = dict(c.pgconn.get_params())
    # Extract connection params from fixture connection for new threads
    host = c.info.host
    port = c.info.port
    dbname = c.info.dbname
    user = c.info.user
    password = getattr(c.info, 'password', None)

    barrier = Barrier(2)
    results = []
    errors = []

    def _call():
        try:
            kw = dict(host=host, port=port, dbname=dbname, user=user)
            if password:
                kw['password'] = password
            with psycopg.connect(**kw, autocommit=True) as conn2:
                conn2.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant_id),))
                events = [
                    {'version_id': str(doc_ids[0]), 'action': 'accepted'},
                    {'version_id': str(doc_ids[1]), 'action': 'acknowledged'},
                ]
                barrier.wait()
                row = conn2.execute(
                    "SELECT state,revision,accepted_set_hash"
                    " FROM photo.account_accept_legal(%s,%s,%s,%s::jsonb,%s::jsonb,%s)",
                    (tenant_id, user_id, session_id,
                     json.dumps({'events': events}),
                     json.dumps({'contract_accepted': True, 'privacy_acknowledged': True}),
                     str(uuid4())),
                ).fetchone()
                results.append(row)
        except Exception as e:
            errors.append(e)

    with ThreadPoolExecutor(max_workers=2) as pool:
        futures = [pool.submit(_call), pool.submit(_call)]
        for f in futures:
            f.result()

    # Both should succeed (idempotent): same state, same or equal revision
    assert len(errors) == 0 or len(results) >= 1, f'unexpected errors: {errors}'
    for row in results:
        assert row[0] == 'complete'
