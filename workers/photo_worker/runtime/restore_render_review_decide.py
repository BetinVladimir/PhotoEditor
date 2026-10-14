"""One-shot native decision on an independently pinned render review fixture."""
from dataclasses import dataclass,field
from hashlib import sha256
import http.client
import ssl
from urllib.parse import urlsplit
from uuid import UUID
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,parse_json
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreRenderReviewTarget:
    origin: str
    tenant_id: UUID
    child_run_id: UUID
    review_id: UUID
    expected_revision: int
    candidate: ArtifactRef
    authorization: str=field(repr=False)


@dataclass(frozen=True)
class RestoreRenderReviewDecision:
    review_id: UUID
    revision: int
    response_sha256: str


def render_review_decision_body(target):
    return canonical_bytes({'expected_revision':target.expected_revision,
        'candidate_hash':target.candidate.sha256,'decision':'accept'})


def validate_fixture_render_review_decision(target,*,tls_context,assert_fixture_scope,check_active):
    if (not isinstance(target,RestoreRenderReviewTarget)
            or any(not isinstance(v,UUID) or v.int==0 for v in
                   (target.tenant_id,target.child_run_id,target.review_id))
            or type(target.expected_revision) is not int
            or not 1<=target.expected_revision<9007199254740991
            or not isinstance(target.candidate,ArtifactRef)
            or target.candidate.artifact_id.int==0
            or not isinstance(target.authorization,str)
            or not target.authorization.startswith('Bearer ')
            or not 8<=len(target.authorization)<=8192
            or any(c in target.authorization for c in '\r\n')
            or not isinstance(tls_context,ssl.SSLContext)
            or tls_context.verify_mode!=ssl.CERT_REQUIRED
            or not tls_context.check_hostname
            or tls_context.minimum_version<ssl.TLSVersion.TLSv1_2
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_RENDER_REVIEW_DECISION_INSTALLATION')
    origin=urlsplit(target.origin)
    if (origin.scheme!='https' or not origin.hostname or origin.username is not None
            or origin.password is not None or origin.path not in ('','/')
            or origin.query or origin.fragment
            or origin.port is not None and not 1<=origin.port<=65535):
        raise ValueError('RESTORE_RENDER_REVIEW_DECISION_ORIGIN')
    return origin


def request_fixture_render_review_decision(target,*,tls_context,assert_fixture_scope,check_active):
    """No automatic retry: reconcile review state after an unknown POST outcome."""
    origin=validate_fixture_render_review_decision(target,tls_context=tls_context,
        assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    raw=render_review_decision_body(target)
    def guard():require_restore_fixture(check_active,assert_fixture_scope)
    guard()
    connection=http.client.HTTPSConnection(origin.hostname,origin.port or 443,
                                           context=tls_context,timeout=5)
    try:
        connection.request('POST',
            f'/api/v1/tenants/{target.tenant_id}/reviews/{target.review_id}/decision',
            body=raw,headers={'Authorization':target.authorization,
                'X-Photo-Client':'native','Content-Type':'application/json',
                'Accept':'application/json','Accept-Encoding':'identity'})
        response=connection.getresponse();guard();body=response.read(16385)
        if (response.status!=200 or len(body)>16384
                or response.getheader('Content-Encoding') not in (None,'identity')
                or response.getheader('Content-Type','').split(';')[0].strip().lower()!='application/json'):
            raise ValueError('RESTORE_RENDER_REVIEW_DECISION_REFUSED')
        value=parse_json(body,max_bytes=16384)
        if (not isinstance(value,dict)
                or set(value)!={'id','run_id','kind','state','revision','candidate_hash',
                                 'decision','decision_route','reasons','created_at'}
                or value['id']!=str(target.review_id)
                or value['run_id']!=str(target.child_run_id)
                or value['kind']!='render.compose' or value['state']!='decided'
                or type(value['revision']) is not int
                or value['revision']!=target.expected_revision+1
                or value['candidate_hash']!=target.candidate.sha256
                or value['decision']!='accept' or value['decision_route']!='candidate'
                or not isinstance(value['reasons'],(dict,list))
                or not isinstance(value['created_at'],str)):
            raise ValueError('RESTORE_RENDER_REVIEW_DECISION_RESPONSE')
        guard()
        return RestoreRenderReviewDecision(target.review_id,value['revision'],
                                           sha256(body).hexdigest())
    except (OSError,http.client.HTTPException):
        raise ValueError('RESTORE_RENDER_REVIEW_DECISION_OUTCOME_UNKNOWN') from None
    finally:connection.close()
