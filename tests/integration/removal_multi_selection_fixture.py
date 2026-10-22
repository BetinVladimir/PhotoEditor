"""Own multi-target removal preview/decision, synthetic result/object metadata.

Actual review/admission/receipt/decision SQL; no physical or quality claims.
"""
import hashlib
import json
from removal_apply_documents_fixture import prepared_removal_review_fixtures
from removal_preview_receipt_fixture import receipt_fixture


def multi_selection_fixture(db, *, target_count=2):
    reviews = prepared_removal_review_fixtures(db,target_count=target_count)
    from semantic_qualification_fixture import frozen_qualification
    from photo_worker.v2.removal_qualification_report import REMOVAL_REQUIRED_CHECKS
    first_args, _, first_scope, _ = reviews[0]
    pin = frozen_qualification(db,first_args[0],first_scope['project'],first_scope['actor'],
        operation='semantic.preview_remove_object',backend='cpu',required_checks=sorted(REMOVAL_REQUIRED_CHECKS))
    bundles = []
    for review in reviews:
        review[2]['removal_preview_qualification'] = pin
        data = receipt_fixture(db,review=review)
        bundles.append(dict(data=data,args=data[0],run=data[1],scope=data[2],target=data[0][4]))
    assert all(bundle['scope']['preview_profile']['qualification']==pin for bundle in bundles)
    request,artifact,body = publish_removal_decision_bundles(db,bundles)
    first = bundles[0]
    params = json.loads(first['args'][5])
    return bundles,[*first['args'][:4],int(params['workflow_revision']),request,artifact,body,first['scope']['review_documents']]


def publish_removal_decision_bundles(db,bundles,*,reverse_targets=False,decisions=None):
    """Publish one decision for genuinely admitted targets in one review.

    This does not create additional targets or project synthetic producer IDs.
    Approved targets require their own admitted preview bundle; rejected targets
    require only the current review source and no preview. Scope and decision
    inventory are checked before any receipt/publication write.
    """
    from removal_preview_receipt_fixture import seal
    if type(reverse_targets) is not bool:
        raise ValueError('reverse_targets must be boolean')
    if not bundles or len(bundles)>100:
        raise ValueError('removal decision requires 1..100 admitted bundles')
    first=bundles[0]
    args,scope=first['args'],first['scope']
    params=json.loads(args[5])
    documents=scope['review_documents']
    plan=next(d for d in documents if d['contract']=='SemanticPlan')
    intent=next(d for d in documents if d['contract']=='EditIntent')
    shared=(args[:4],params['workflow_id'],params['workflow_revision'],scope['snapshot'],documents)
    seen=set()
    ordered=sorted(bundles,key=lambda bundle:str(bundle['target']),reverse=reverse_targets)
    for bundle in ordered:
        other_args,other_scope=bundle['args'],bundle['scope']
        other=json.loads(other_args[5])
        identity=(other_args[:4],other['workflow_id'],other['workflow_revision'],other_scope['snapshot'],other_scope['review_documents'])
        if identity!=shared or bundle['target'] in seen or str(bundle['target'])!=other['target_id'] or str(other_args[4])!=other['target_id']:
            raise ValueError('removal decision bundles must be distinct targets of one review')
        if other['plan']!={k:plan[k] for k in ('artifact_id','sha256')} or other['intent']!={k:intent[k] for k in ('artifact_id','sha256')}:
            raise ValueError('removal decision plan/intent mismatch')
        seen.add(bundle['target'])
    choices_by_id = {str(bundle['target']): 'approve' for bundle in bundles} if decisions is None else {
        str(target): decision for target, decision in decisions.items()}
    if (decisions is not None and len(choices_by_id) != len(decisions)
            or set(choices_by_id) != {str(target) for target in seen}
            or any(decision not in ('approve', 'reject') for decision in choices_by_id.values())):
        raise ValueError('removal decisions must name exactly the selected bundles')
    parent=db.execute('SELECT w.run_id FROM photo.semantic_intents i JOIN photo.v2_workflows w '
        'ON w.id=i.workflow_id AND w.tenant_id=i.tenant_id WHERE i.tenant_id=%s AND i.id=%s',args[:2]).fetchone()[0]
    choices=[];targets=[]
    for bundle in ordered:
        current=json.loads(bundle['args'][5])
        candidate=json.loads(bundle['scope']['candidate_body'])
        decision = choices_by_id[str(bundle['target'])]
        choice = dict(target_id=str(bundle['target']), decision=decision)
        target = dict(target_id=str(bundle['target']), target_revision=current['target_revision'],
            decision=decision, source=candidate['target'], candidate=current['candidate'])
        if decision == 'approve':
            receipt=seal(db,bundle["data"])
            selection=dict(render=receipt['working_image'],evidence=receipt['presentation'])
            choice['preview'] = selection
            target['preview'] = selection
        choices.append(choice)
        targets.append(target)
    request=dict(run_id=str(parent),request=dict(expected_revision=params['workflow_revision'],plan_sha=plan['sha256'],target_decisions=choices))
    body=dict(schema_version=1,kind='semantic_review',run_id=str(parent),expected_revision=params['workflow_revision'],
        plan={k:plan[k] for k in ('artifact_id','sha256')},intent={k:intent[k] for k in ('artifact_id','sha256')},
        snapshot_id=str(scope['snapshot']),targets=targets)
    encode=lambda value:json.dumps(value,sort_keys=True,separators=(',', ':'))
    request_raw,body_raw=encode(request),encode(body)
    request_sha=hashlib.sha256(request_raw.encode()).hexdigest()
    sha=hashlib.sha256(body_raw.encode()).hexdigest()
    base=(*args[:4],int(params['workflow_revision']),request_sha)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        reserved=db.execute('SELECT * FROM photo.reserve_semantic_decision_document('+','.join(['%s']*9)+')',
            (*base,sha,len(body_raw.encode()),'fixture')).fetchone()
        assert reserved[0]=='created' and reserved[3] is False
        assert db.execute('SELECT photo.complete_semantic_decision_document('+','.join(['%s']*12)+')',
            (*base,reserved[1],reserved[2],sha,len(body_raw.encode()),'fixture','decision-fixture-v1')).fetchone()[0] is True
        db.execute('RESET ROLE')
    return request_raw,reserved[1],body_raw



def multi_apply_task_fixture(db, *, target_count=2):
    """Shared decision/child and one frozen CPU copy policy; no activation."""
    from uuid import uuid4
    from removal_apply_documents_fixture import approved_params_fixture, admission_task_fixture
    bundles, gate = multi_selection_fixture(db, target_count=target_count)
    child = uuid4()
    tasks = []
    values = []
    pin = None
    for bundle in bundles:
        approved = approved_params_fixture(db,
            published=(bundle['data'], gate[5], gate[6], gate[7]), child=child)
        value, task = admission_task_fixture(db, approved=approved, qualification=pin)
        pin = task['node']['execution_profile']['qualification']
        values.append(value)
        tasks.append(task)
    assert len({task['node']['id'] for task in tasks}) == target_count
    assert all(value[4] == child for value in values)
    return bundles, gate, child, tasks
