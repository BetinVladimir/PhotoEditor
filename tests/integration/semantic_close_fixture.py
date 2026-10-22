"""Own close publication/commit following real explicit rejection decisions.

Uses original review sources and SQL authority, synthetic object versions only.
No preview execution, object-store proof or photographic qualification claim.
"""
from hashlib import sha256
import json
from uuid import uuid4
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes
from semantic_rejection_fixture import rejection_fixture, commit_rejection


def close_fixture(db, *, selected_count=2, parent_revision=None, decided=None):
    if decided is None:
        rejected = rejection_fixture(db, selected_count=selected_count)
        applied = commit_rejection(db, rejected)
    else:
        rejected, applied = decided
    args = rejected['args']
    revision = int(applied['revision'])
    if parent_revision is not None:
        revision = parent_revision
        db.execute('UPDATE photo.runs SET revision=%s WHERE tenant_id=%s AND id=%s',
                   (revision, args[0], rejected['parent']))
    plan = next(d for d in rejected['documents'] if d['contract']=='SemanticPlan')
    request = dict(run_id=str(rejected['parent']), request=dict(expected_revision=str(revision), plan_sha=plan['sha256']))
    body = dict(schema_version=1, kind='semantic_review_close', run_id=str(rejected['parent']),
                expected_revision=str(revision), plan_sha=plan['sha256'])
    request_raw, body_raw = canonical_bytes(request).decode(), canonical_bytes(body).decode()
    request_sha, body_sha = sha256(request_raw.encode()).hexdigest(), sha256(body_raw.encode()).hexdigest()
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        base = (*args[:4], revision, request_sha)
        reserved = db.execute('SELECT * FROM photo.reserve_semantic_decision_document('+','.join(['%s']*9)+')',
            (*base, body_sha, len(body_raw.encode()), 'fixture')).fetchone()
        assert reserved[0]=='created' and reserved[3] is False
        assert db.execute('SELECT photo.complete_semantic_decision_document('+','.join(['%s']*12)+')',
            (*base, reserved[1], reserved[2], body_sha, len(body_raw.encode()), 'fixture', 'close-fixture-v1')).fetchone()[0] is True
        db.execute('RESET ROLE')
    return dict(args=args, parent=rejected['parent'], rejected=rejected, revision=revision,
        request=request_raw, request_sha=request_sha, body=body_raw, artifact=reserved[1],
        documents=rejected['documents'], key=str(uuid4()))


def commit_close(db, f, *, reserve_receipt=True):
    args = f['args']
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(args[0]),))
        if reserve_receipt:
            db.execute('INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) '
                "VALUES(%s,'user',%s,'SemanticIntent.CloseReview',%s,%s,clock_timestamp()+interval '1 day')",
                (args[0],args[2],f['key'],sha256(f['request'].encode()).hexdigest()))
        result = db.execute('SELECT photo.commit_semantic_review_close('+','.join(['%s']*10)+')',
            (*args[:4], f['key'], f['revision'], f['request'], f['artifact'], f['body'], Jsonb(f['documents']))).fetchone()[0]
        db.execute('RESET ROLE')
        return result


def replay_close(db, f, *, digest=None, actor=None, session=None):
    args = f['args']
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        result = db.execute('SELECT outcome,response_body FROM photo.semantic_close_replay('+','.join(['%s']*6)+')',
            (args[0],args[1],args[2] if actor is None else actor,args[3] if session is None else session,
             f['key'],f['request_sha'] if digest is None else digest)).fetchone()
        db.execute('RESET ROLE')
        return result
