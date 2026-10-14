"""Canonical native consent revoke; SQL independently establishes its effects."""
from dataclasses import dataclass, field
import hashlib
import http.client
import ssl
from urllib.parse import urlsplit

from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.runtime.restore_revoke_probe import RestoreRevokeFixture, observe_fixture_learning_consent
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreConsentRevokeTarget:
    origin: str
    fixture: RestoreRevokeFixture
    idempotency_key: str = field(repr=False)
    authorization: str = field(repr=False)


@dataclass(frozen=True)
class RestoreConsentRevokeReceipt:
    schema_version: str
    consent_id: str
    epoch: int
    revision: int
    response_sha256: str
    replayed: bool


def request_fixture_consent_revoke(target, *, tls_context, assert_fixture_scope, check_active):
    if (not isinstance(target, RestoreConsentRevokeTarget)
            or not isinstance(target.fixture, RestoreRevokeFixture)
            or not isinstance(target.idempotency_key, str)
            or not 1 <= len(target.idempotency_key) <= 128
            or any(not 33 <= ord(c) <= 126 for c in target.idempotency_key)
            or not isinstance(target.authorization, str)
            or not target.authorization.startswith('Bearer ')
            or not 8 <= len(target.authorization) <= 8192
            or any(c in target.authorization for c in '\r\n')
            or not isinstance(tls_context, ssl.SSLContext)
            or tls_context.verify_mode != ssl.CERT_REQUIRED or not tls_context.check_hostname
            or tls_context.minimum_version < ssl.TLSVersion.TLSv1_2
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_REVOKE_REQUEST_INSTALLATION')
    f = target.fixture
    from uuid import UUID
    if (any(not isinstance(v, UUID) or v.int == 0 for v in
            (f.tenant_id, f.project_id, f.consent_id, f.actor_id, f.learning_version_id))
            or f.purpose not in ('learning_person', 'learning_photographer')
            or (f.purpose == 'learning_photographer') != (f.person_id is None)
            or (f.person_id is not None and (not isinstance(f.person_id, UUID) or f.person_id.int == 0))
            or any(type(n) is not int or not 1 <= n < 9007199254740991 for n in (f.epoch, f.revision))
            or not isinstance(f.policy_version, str) or not 1 <= len(f.policy_version) <= 80):
        raise ValueError('RESTORE_REVOKE_REQUEST_FIXTURE')
    origin = urlsplit(target.origin)
    if (origin.scheme != 'https' or not origin.hostname or origin.username is not None
            or origin.password is not None or origin.path not in ('', '/') or origin.query or origin.fragment):
        raise ValueError('RESTORE_REVOKE_REQUEST_ORIGIN')
    body = canonical_bytes(dict(expected_revision=str(f.revision), purpose=f.purpose,
        person_id=None if f.person_id is None else str(f.person_id), action='revoke', policy_version=f.policy_version))
    def guard():
        require_restore_fixture(check_active,assert_fixture_scope,target)
    guard()
    connection = http.client.HTTPSConnection(origin.hostname, origin.port or 443, context=tls_context, timeout=5)
    try:
        connection.request('POST', f'/api/v1/tenants/{f.tenant_id}/projects/{f.project_id}/consents', body=body,
            headers={'Authorization': target.authorization, 'X-Photo-Client': 'native',
                'Idempotency-Key': target.idempotency_key, 'Content-Type': 'application/json',
                'Accept': 'application/json', 'Accept-Encoding': 'identity'})
        response = connection.getresponse(); guard(); raw = response.read(16385)
        replayed = response.getheader('Idempotency-Replayed')
        if (response.status != 200 or len(raw) > 16384
                or response.getheader('Content-Encoding') not in (None, 'identity')
                or response.getheader('Content-Type', '').split(';')[0].strip().lower() != 'application/json'
                or replayed not in (None, 'true')):
            raise ValueError('RESTORE_REVOKE_REQUEST_REFUSED')
        value = parse_json(raw, max_bytes=16384)
        if (not isinstance(value, dict) or set(value) != {'id', 'revision', 'document', 'data'}
                or value['id'] != str(f.consent_id) or value['revision'] != str(f.revision + 1)
                or value['document'] is not None or not isinstance(value['data'], dict)):
            raise ValueError('RESTORE_REVOKE_REQUEST_RECEIPT')
        expected = dict(id=str(f.consent_id), tenant_id=str(f.tenant_id), project_id=str(f.project_id),
            resource_type='consent_grants', revision=str(f.revision + 1), epoch=str(f.epoch + 1),
            purpose=f.purpose, person_id=None if f.person_id is None else str(f.person_id),
            status='revoked', policy_version=f.policy_version, actor_id=str(f.actor_id))
        if any(value['data'].get(k) != v or k not in value['data'] for k, v in expected.items()):
            raise ValueError('RESTORE_REVOKE_REQUEST_RECEIPT')
        guard()
        return RestoreConsentRevokeReceipt('restore-consent-revoke/v1', str(f.consent_id),
            f.epoch + 1, f.revision + 1, hashlib.sha256(raw).hexdigest(), replayed == 'true')
    except (OSError, http.client.HTTPException):
        raise ValueError('RESTORE_REVOKE_REQUEST_OUTCOME_UNKNOWN') from None
    finally:
        connection.close()


def revoke_fixture_learning_consent(connection, target, *, expected_database, tls_context,
                                    assert_fixture_scope, check_active):
    """No retries: baseline, canonical CAS mutation, independent committed SQL proof."""
    if not isinstance(target, RestoreConsentRevokeTarget):
        raise ValueError('RESTORE_REVOKE_REQUEST_INSTALLATION')
    def scope(_):
        return assert_fixture_scope(target)
    require_restore_fixture(check_active,assert_fixture_scope,target)
    kwargs = dict(expected_database=expected_database, assert_fixture_scope=scope, check_active=check_active)
    baseline = observe_fixture_learning_consent(connection, target.fixture, revoked=False, **kwargs)
    receipt = request_fixture_consent_revoke(target, tls_context=tls_context,
        assert_fixture_scope=assert_fixture_scope, check_active=check_active)
    committed = observe_fixture_learning_consent(connection, target.fixture, revoked=True, **kwargs)
    require_restore_fixture(check_active,assert_fixture_scope,target)
    return baseline, receipt, committed
