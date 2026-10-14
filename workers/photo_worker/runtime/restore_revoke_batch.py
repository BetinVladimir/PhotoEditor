"""One-frame render-batch refusal transport; not standalone consent evidence."""
from dataclasses import dataclass, field
from datetime import datetime, timezone
from hashlib import sha256
from uuid import UUID, uuid5
from urllib.parse import urlsplit
import re
import ssl
import http.client
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.runtime.restore_revoke_freeze import RestoreFreezeBaselineBinding, RestoreFreezeBaselineEvidence
from photo_worker.runtime.restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreRenderBatchTarget:
    origin: str
    tenant_id: UUID
    project_id: UUID
    snapshot_id: UUID
    policy_sha256: str
    idempotency_key: str = field(repr=False)
    authorization: str = field(repr=False)


@dataclass(frozen=True)
class RestoreRenderBatchRefusal:
    schema_version: str
    request_sha256: str
    response_sha256: str
    code: str
    observed_at: datetime


@dataclass(frozen=True)
class RestoreRenderBatchBaseline:
    schema_version: str
    batch_id: UUID
    child_run_id: UUID
    state: str
    request_sha256: str
    response_sha256: str
    observed_at: datetime


def render_batch_request_body(target,binding,frozen):
    return canonical_bytes(dict(input_snapshot_id=str(target.snapshot_id), policy_sha=target.policy_sha256,
        selection=[dict(source=dict(asset_version_id=str(binding.asset_version_id),
            source=dict(artifact_id=str(binding.source_artifact_id), sha256=binding.source_sha256),
            canvas_hash=binding.canvas_hash), recipe=dict(artifact_id=str(frozen.recipe_artifact_id), sha256=frozen.recipe_sha256))]))


def observe_fixture_render_batch_baseline(target,binding,frozen,*,tls_context,assert_current_fixture,check_active):
    return _observe_fixture_render_batch(target,binding,frozen,tls_context=tls_context,
        assert_current_fixture=assert_current_fixture,check_active=check_active,positive=True)


def observe_fixture_render_batch_refused(target,binding,frozen,*,tls_context,assert_current_fixture,check_active):
    return _observe_fixture_render_batch(target,binding,frozen,tls_context=tls_context,
        assert_current_fixture=assert_current_fixture,check_active=check_active)


def _observe_fixture_render_batch(target, binding, frozen, *, tls_context,
                                          assert_current_fixture, check_active, positive=False):
    """Submit exact positive frozen recipe using a fresh independently scoped key.

    Require explicit consent refusal; generic validation cannot prove cause.
    Snapshot liveness, qualification, registration and no task/run adoption
    must be proven independently. This diagnostic never issues acceptance.
    """
    if (not isinstance(target, RestoreRenderBatchTarget) or not isinstance(binding, RestoreFreezeBaselineBinding)
            or not isinstance(frozen, RestoreFreezeBaselineEvidence)
            or any(not isinstance(v, UUID) or v.int == 0 for v in
                (target.tenant_id, target.project_id, target.snapshot_id, binding.actor_id, binding.asset_version_id,
                 binding.source_artifact_id, frozen.recipe_artifact_id))
            or binding.source_artifact_id == frozen.recipe_artifact_id
            or any(not isinstance(v, str) or re.fullmatch('[0-9a-f]{64}', v) is None for v in
                (target.policy_sha256, binding.source_sha256, binding.canvas_hash, frozen.recipe_sha256))
            or not isinstance(target.idempotency_key, str) or not 1 <= len(target.idempotency_key) <= 128
            or any(not 33 <= ord(c) <= 126 for c in target.idempotency_key)
            or not isinstance(target.authorization, str) or not target.authorization.startswith('Bearer ')
            or not 8 <= len(target.authorization) <= 8192 or any(c in target.authorization for c in '\r\n')
            or not isinstance(tls_context, ssl.SSLContext) or tls_context.verify_mode != ssl.CERT_REQUIRED
            or not tls_context.check_hostname or tls_context.minimum_version < ssl.TLSVersion.TLSv1_2
            or not callable(assert_current_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_RENDER_BATCH_INSTALLATION')
    origin = urlsplit(target.origin)
    if (origin.scheme != 'https' or not origin.hostname or origin.username is not None or origin.password is not None
            or origin.path not in ('', '/') or origin.query or origin.fragment):
        raise ValueError('RESTORE_RENDER_BATCH_ORIGIN')
    body = render_batch_request_body(target,binding,frozen)
    def guard():
        require_restore_fixture(check_active, assert_current_fixture, target, binding, frozen)
    guard()
    conn = http.client.HTTPSConnection(origin.hostname, origin.port or 443, context=tls_context, timeout=5)
    try:
        conn.request('POST', f'/api/v1/tenants/{target.tenant_id}/projects/{target.project_id}/render-batches', body=body,
            headers={'Authorization':target.authorization, 'X-Photo-Client':'native',
                'Idempotency-Key':target.idempotency_key, 'Content-Type':'application/json',
                'Accept':'application/json', 'Accept-Encoding':'identity'})
        response=conn.getresponse(); guard(); raw=response.read(16385)
        if (response.status != (202 if positive else 409) or len(raw)>16384
                or response.getheader('Content-Encoding') not in (None,'identity')
                or response.getheader('Content-Type','').split(';')[0].strip().lower()!='application/json'
                or response.getheader('Idempotency-Replayed') is not None):
            raise ValueError('RESTORE_RENDER_BATCH_REFUSAL')
        value=parse_json(raw,max_bytes=16384)
        if positive:
            batch=uuid5(target.tenant_id,'V2.Admit:'+str(binding.actor_id)+':'+target.idempotency_key)
            child=uuid5(batch,'render.batch:0')
            if (not isinstance(value,dict) or set(value)!={'run_id','state','child_run_ids'}
                    or value['run_id']!=str(batch) or value['child_run_ids']!=[str(child)]
                    or value['state'] not in ('queued','running','waiting_review')):
                raise ValueError('RESTORE_RENDER_BATCH_BASELINE_TICKET')
            guard()
            return RestoreRenderBatchBaseline('restore-render-batch-baseline/v1',batch,child,value['state'],
                sha256(body).hexdigest(),sha256(raw).hexdigest(),datetime.now(timezone.utc))
        if not isinstance(value,dict) or value.get('code')!='PROFILE_CONSENT_REVOKED':
            raise ValueError('RESTORE_RENDER_BATCH_CODE')
        guard()
        return RestoreRenderBatchRefusal('restore-render-batch-refusal/v1',sha256(body).hexdigest(),
            sha256(raw).hexdigest(),'PROFILE_CONSENT_REVOKED',datetime.now(timezone.utc))
    except (OSError,http.client.HTTPException):
        raise ValueError('RESTORE_RENDER_BATCH_OUTCOME_UNKNOWN') from None
    finally:
        conn.close()
