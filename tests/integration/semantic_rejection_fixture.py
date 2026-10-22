"""Own reject-only review SQL fixture; no preview/copy/Apply child is admitted.

An explicit approve decision may be published only as negative-test input to
the public reject-only SQL entry. Decision publication uses synthetic versioned object metadata. This is not
physical object-store or photographic qualification evidence.
"""
from hashlib import sha256
import json
from uuid import uuid4
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes
from removal_apply_documents_fixture import prepared_removal_review_fixtures


def rejection_fixture(db, *, selected_count=1, parent_revision=None, decision='reject', reviews=None):
    reviews = prepared_removal_review_fixtures(db, target_count=2) if reviews is None else reviews
    assert selected_count in (1, 2) and selected_count <= len(reviews)
    assert decision in ('reject', 'approve')
    args, parent, scope, _ = reviews[0]
    params = json.loads(args[5])
    if parent_revision is not None:
        db.execute('UPDATE photo.runs SET revision=%s WHERE tenant_id=%s AND id=%s',
            (parent_revision, args[0], parent))
        params['workflow_revision'] = str(parent_revision)
    plan = next(d for d in scope['review_documents'] if d['contract'] == 'SemanticPlan')
    intent = next(d for d in scope['review_documents'] if d['contract'] == 'EditIntent')
    selected = sorted(reviews[:selected_count], key=lambda r: str(r[3]))
    decisions = [dict(target_id=str(review[3]), decision=decision) for review in selected]
    request = dict(run_id=str(parent), request=dict(expected_revision=params['workflow_revision'],
        plan_sha=plan['sha256'], target_decisions=decisions))
    targets = []
    for own_args, _, own, target in selected:
        own_params = json.loads(own_args[5])
        candidate = json.loads(own['candidate_body'])
        targets.append(dict(target_id=str(target), target_revision=own_params['target_revision'],
            decision=decision, source=candidate['target'], candidate=own_params['candidate']))
    body = dict(schema_version=1, kind='semantic_review', run_id=str(parent),
        expected_revision=params['workflow_revision'],
        plan={k: plan[k] for k in ('artifact_id', 'sha256')},
        intent={k: intent[k] for k in ('artifact_id', 'sha256')},
        snapshot_id=str(scope['snapshot']), targets=targets)
    request_raw = canonical_bytes(request).decode()
    body_raw = canonical_bytes(body).decode()
    request_sha = sha256(request_raw.encode()).hexdigest()
    body_sha = sha256(body_raw.encode()).hexdigest()
    revision = int(params['workflow_revision'])
    # This positive publication fixture owns usage initialization, not the
    # generic tenant/source builder. Preserve existing accounting and quotas
    # when reviews are supplied by another fixture (including mixed flows).
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) '
        'ON CONFLICT(tenant_id) DO NOTHING', (args[0],))
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        reserved = db.execute('SELECT * FROM photo.reserve_semantic_decision_document('
            + ','.join(['%s'] * 9) + ')',
            (*args[:4], revision, request_sha, body_sha, len(body_raw.encode()), 'fixture')).fetchone()
        assert reserved[0] == 'created' and reserved[3] is False, dict(outcome=reserved[0], ready=reserved[3], artifact_id=str(reserved[1]))
        assert db.execute('SELECT photo.complete_semantic_decision_document('
            + ','.join(['%s'] * 12) + ')',
            (*args[:4], revision, request_sha, reserved[1], reserved[2], body_sha,
             len(body_raw.encode()), 'fixture', 'rejection-fixture-v1')).fetchone()[0] is True
        db.execute('RESET ROLE')
    return dict(args=args, parent=parent, reviews=reviews, scope=scope, revision=revision,
        request=request_raw, request_sha=request_sha, body=body_raw, artifact=reserved[1],
        documents=scope['review_documents'], key=str(uuid4()), selected=[review[3] for review in selected])


def commit_rejection(db, fixture, *, key=None, request=None, body=None, reserve_receipt=True):
    """Real fresh mutation boundary, including its pending receipt and rollback.

    Saved receipt replay is tested through semantic_apply_replay separately.
    """
    f = fixture
    args = f['args']
    key = f['key'] if key is None else key
    request = f['request'] if request is None else request
    body = f['body'] if body is None else body
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(args[0]),))
        if reserve_receipt:
            db.execute('INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) '
                "VALUES(%s,'user',%s,'SemanticIntent.Apply',%s,%s,clock_timestamp()+interval '1 day')",
                (args[0], args[2], key, sha256(request.encode()).hexdigest()))
        result = db.execute('SELECT photo.commit_semantic_rejections('
            + ','.join(['%s'] * 10) + ')',
            (*args[:4], key, f['revision'], request, f['artifact'], body, Jsonb(f['documents']))).fetchone()[0]
        db.execute('RESET ROLE')
        return result


def replay_rejection(db, fixture, *, digest=None, actor=None, session=None):
    f = fixture
    args = f['args']
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        result = db.execute('SELECT outcome,response_body FROM photo.semantic_apply_replay('
            + ','.join(['%s'] * 6) + ')',
            (args[0], args[1], args[2] if actor is None else actor,
             args[3] if session is None else session, f['key'],
             f['request_sha'] if digest is None else digest)).fetchone()
        db.execute('RESET ROLE')
        return result
