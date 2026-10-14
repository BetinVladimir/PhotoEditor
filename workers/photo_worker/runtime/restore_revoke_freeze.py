"""Actual render-freeze denial observation, not standalone consent proof."""
from dataclasses import dataclass, field
from hashlib import sha256
from datetime import datetime, timezone
import http.client
import ssl
from urllib.parse import urlsplit
from uuid import UUID, uuid5, NAMESPACE_OID
import re
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.runtime.restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreFreezeDenialTarget:
    origin: str
    tenant_id: UUID
    asset_id: UUID
    document_id: UUID
    revision: int
    idempotency_key: str = field(repr=False)
    authorization: str = field(repr=False)


@dataclass(frozen=True)
class RestoreFreezeDenialEvidence:
    schema_version: str
    tenant_id: UUID
    asset_id: UUID
    document_id: UUID
    revision: int
    request_sha256: str
    response_sha256: str
    observed_at: datetime


@dataclass(frozen=True)
class RestoreFreezeBaselineBinding:
    actor_id: UUID
    asset_version_id: UUID
    source_artifact_id: UUID
    source_sha256: str
    canvas_hash: str


@dataclass(frozen=True)
class RestoreFreezeBaselineEvidence:
    schema_version: str
    frozen_document_id: UUID
    recipe_artifact_id: UUID
    recipe_sha256: str
    request_sha256: str
    response_sha256: str
    observed_at: datetime


def _freeze_document_id(target, actor):
    # Go uuid.NewSHA1 uses UUIDv5. Compute over UTF-8 explicitly (uuid5 accepts text).
    return uuid5(NAMESPACE_OID, f'v26-render-freeze/{target.tenant_id}/{actor}/{target.asset_id}/{target.idempotency_key}')


def observe_fixture_freeze_baseline(target, binding, *, tls_context, assert_current_fixture, check_active):
    if (not isinstance(binding, RestoreFreezeBaselineBinding)
            or any(not isinstance(v, UUID) or v.int == 0 for v in
                (binding.actor_id, binding.asset_version_id, binding.source_artifact_id))
            or any(not isinstance(v, str) or re.fullmatch('[0-9a-f]{64}', v) is None for v in
                (binding.source_sha256, binding.canvas_hash))):
        raise ValueError('RESTORE_FREEZE_BASELINE_BINDING')
    return _observe_fixture_freeze(target, tls_context=tls_context, assert_current_fixture=assert_current_fixture,
        check_active=check_active, baseline=binding)


def observe_fixture_freeze_denied(target, *, tls_context, assert_current_fixture, check_active):
    return _observe_fixture_freeze(target, tls_context=tls_context, assert_current_fixture=assert_current_fixture,
        check_active=check_active)


def _observe_fixture_freeze(target, *, tls_context, assert_current_fixture, check_active, baseline=None):
    """POST canonical freeze with a fresh key; require its current409 contract.

    Caller must independently prove current draft revision, revoked lineage,
    qualified runtime and unused key. REVISION_CONFLICT alone is ambiguous;
    this receipt cannot satisfy a complete processing-denied acceptance gate.
    No retry: an unexpected success may have committed a frozen document.
    """
    if (not isinstance(target, RestoreFreezeDenialTarget)
            or any(not isinstance(v, UUID) or v.int == 0 for v in
                (target.tenant_id, target.asset_id, target.document_id))
            or type(target.revision) is not int or not 1 <= target.revision <= 2**63-1
            or not isinstance(target.idempotency_key, str) or not 1 <= len(target.idempotency_key) <= 128
            or any(not 33 <= ord(c) <= 126 for c in target.idempotency_key)
            or not isinstance(target.authorization, str) or not target.authorization.startswith('Bearer ')
            or not 8 <= len(target.authorization) <= 8192 or any(c in target.authorization for c in '\r\n')
            or not isinstance(tls_context, ssl.SSLContext) or tls_context.verify_mode != ssl.CERT_REQUIRED
            or not tls_context.check_hostname or tls_context.minimum_version < ssl.TLSVersion.TLSv1_2
            or not callable(assert_current_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_FREEZE_DENIAL_INSTALLATION')
    origin = urlsplit(target.origin)
    if (origin.scheme != 'https' or not origin.hostname or origin.username is not None
            or origin.password is not None or origin.path not in ('', '/') or origin.query or origin.fragment):
        raise ValueError('RESTORE_FREEZE_DENIAL_ORIGIN')
    raw_request = canonical_bytes(dict(document_id=str(target.document_id),
        expected_revision=str(target.revision), assume_profile=None))
    def guard():
        require_restore_fixture(check_active, assert_current_fixture, target)
    guard()
    connection = http.client.HTTPSConnection(origin.hostname, origin.port or 443, context=tls_context, timeout=5)
    try:
        connection.request('POST', f'/api/v1/tenants/{target.tenant_id}/assets/{target.asset_id}/v2-draft/freeze',
            body=raw_request, headers={'Authorization': target.authorization, 'X-Photo-Client': 'native',
                'Idempotency-Key': target.idempotency_key, 'Content-Type': 'application/json',
                'Accept': 'application/json', 'Accept-Encoding': 'identity'})
        response = connection.getresponse(); guard(); raw = response.read(16385)
        if (response.status != (200 if baseline is not None else 409) or len(raw) > (4096 if baseline is not None else 16384)
                or response.getheader('Content-Encoding') not in (None, 'identity')
                or response.getheader('Content-Type', '').split(';')[0].strip().lower() != 'application/json'
                or response.getheader('Idempotency-Replayed') is not None):
            raise ValueError('RESTORE_FREEZE_DENIAL_REFUSED')
        value = parse_json(raw, max_bytes=16384)
        if baseline is not None:
            frozen = _freeze_document_id(target, baseline.actor_id)
            source = dict(asset_version_id=str(baseline.asset_version_id),
                source=dict(artifact_id=str(baseline.source_artifact_id), sha256=baseline.source_sha256),
                canvas_hash=baseline.canvas_hash)
            if (not isinstance(value, dict) or set(value) != {'document_id','draft_document_id','draft_revision','source','recipe'}
                    or value['document_id'] != str(frozen) or value['draft_document_id'] != str(target.document_id)
                    or value['draft_revision'] != str(target.revision) or value['source'] != source
                    or not isinstance(value['recipe'], dict) or set(value['recipe']) != {'artifact_id','sha256'}):
                raise ValueError('RESTORE_FREEZE_BASELINE_RECEIPT')
            try:
                recipe = UUID(value['recipe']['artifact_id'])
            except (ValueError, TypeError, AttributeError):
                raise ValueError('RESTORE_FREEZE_BASELINE_RECEIPT') from None
            pin = value['recipe']['sha256']
            if (recipe.int == 0 or str(recipe) != value['recipe']['artifact_id']
                    or not isinstance(pin, str) or re.fullmatch('[0-9a-f]{64}', pin) is None):
                raise ValueError('RESTORE_FREEZE_BASELINE_RECEIPT')
            guard()
            return RestoreFreezeBaselineEvidence('restore-freeze-baseline/v1', frozen, recipe, pin,
                sha256(raw_request).hexdigest(), sha256(raw).hexdigest(), datetime.now(timezone.utc))
        if not isinstance(value, dict) or value.get('code') != 'REVISION_CONFLICT':
            raise ValueError('RESTORE_FREEZE_DENIAL_CODE')
        guard()
        return RestoreFreezeDenialEvidence('restore-freeze-denial/v1', target.tenant_id, target.asset_id,
            target.document_id, target.revision, sha256(raw_request).hexdigest(), sha256(raw).hexdigest(),
            datetime.now(timezone.utc))
    except (OSError, http.client.HTTPException):
        raise ValueError('RESTORE_FREEZE_DENIAL_OUTCOME_UNKNOWN') from None
    finally:
        connection.close()
