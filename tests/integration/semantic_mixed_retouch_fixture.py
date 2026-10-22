"""Own mixed shown-retouch batch: approved/rejected/omitted targets of one review.

Only approved targets receive preview, copy documents, task or execution quota.
SQL authority is real; qualified report and object metadata remain synthetic.
"""
import json
from hashlib import sha256
from uuid import uuid4
from psycopg.types.json import Jsonb
from retouch_apply_documents_fixture import prepared_retouch_review_fixtures
from retouch_copy_apply_fixture import approved_params_fixture, admission_task_fixture, publish_retouch_decision_bundles
from retouch_preview_bundle_fixture import retouch_bundle_fixture


def mixed_decision_fixture(db, *, approved_count=1, rejected_count=1, omitted_count=1, reverse=False):
    assert approved_count in (1, 2) and rejected_count == 1 and omitted_count == 1
    reviews = prepared_retouch_review_fixtures(db,
        target_count=approved_count+rejected_count+omitted_count)
    selected = []
    preview_pin = None
    for index, review in enumerate(reviews[:approved_count+rejected_count]):
        if index < approved_count:
            if preview_pin is not None:
                review[2]['retouch_preview_qualification'] = preview_pin
            bundle = retouch_bundle_fixture(db, admitted=True, review=review)
            args, scope = bundle['args'], bundle['scope']
            preview_pin = scope['retouch_preview_qualification']
            bodies, refs = bundle['bodies'], bundle['refs']
            data = (args, bundle['run'], scope, bundle['presentation'].model_dump(mode='json'),
                bodies['preview_presentation'].decode(), bodies['preview_evidence'].decode(),
                bodies['working_image'].decode(), bundle['task'], bundle['result'],
                refs['preview_presentation'].artifact_id, refs['preview_presentation'].sha256)
            bundle['data'] = data
        else:
            bundle = dict(data=None, args=review[0], scope=review[2], target=review[3])
        selected.append(bundle)
    decisions = {bundle['target']: 'approve' if index < approved_count else 'reject'
        for index, bundle in enumerate(selected)}
    request, artifact, body = publish_retouch_decision_bundles(db, selected,
        decisions=decisions, reverse_targets=reverse)
    child = uuid4()
    tasks = []
    copy_pin = None
    for bundle in selected[:approved_count]:
        approved = approved_params_fixture(db,
            published=(bundle['data'], request, artifact, body), child=child)
        _, task = admission_task_fixture(db, approved=approved, qualification=copy_pin)
        copy_pin = task['node']['execution_profile']['qualification']
        tasks.append(task)
    args = selected[0]['args']
    return dict(args=args, reviews=reviews, selected=selected, child=child, tasks=tasks,
        request=request, request_sha=sha256(request.encode()).hexdigest(), artifact=artifact, body=body, documents=selected[0]['scope']['review_documents'],
        revision=int(json.loads(request)['request']['expected_revision']), key=str(uuid4()),
        approved=[review[3] for review in reviews[:approved_count]],
        rejected=[review[3] for review in reviews[approved_count:approved_count+rejected_count]],
        omitted=[review[3] for review in reviews[approved_count+rejected_count:]])


def commit_mixed_decision(db, fixture, *, tasks=None, key=None, reserve_receipt=True):
    f = fixture
    args = f['args']
    with db.transaction():
        db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(args[0]),))
        db.execute('SET LOCAL ROLE photo_api')
        if reserve_receipt:
            db.execute('INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) '
                "VALUES(%s,'user',%s,'SemanticIntent.Apply',%s,%s,clock_timestamp()+interval '1 day')",
                (args[0], args[2], f['key'] if key is None else key, sha256(f['request'].encode()).hexdigest()))
        db.execute('RESET ROLE')
        db.execute('SET LOCAL ROLE photo_semantic_admission')
        result = db.execute('SELECT photo.commit_semantic_shown_retouch_apply_review('
            + ','.join(['%s']*12) + ')',
            (*args[:4], f['key'] if key is None else key, f['revision'], f['request'], f['artifact'],
             f['body'], Jsonb(f['documents']), f['child'], Jsonb(f['tasks'] if tasks is None else tasks))).fetchone()[0]
        db.execute('RESET ROLE')
        return result
