from dataclasses import replace
import json
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_import_adoption import verify_import_output
from photo_worker.v2.interchange_import_review import prepare_import_review
from test_interchange_import_adoption import adoption_fixture


def fixture():
    args, _ = adoption_fixture()
    proof = verify_import_output(**args)
    params = json.loads(proof.params_jcs)
    plan_ref = ArtifactRef.model_validate_json(json.dumps(params['plan']))
    return proof, dict(workflow_id=args['workflow_id'], project_id=args['project_id'],
        snapshot_digest=args['snapshot_digest'], plan_jcs=args['read_input'](plan_ref, 8 << 20))


def test_review_packet_is_deterministic_and_never_implicitly_approves():
    proof, args = fixture()
    review = prepare_import_review(proof, **args)
    assert review == prepare_import_review(proof, **args)
    body = json.loads(review.body)
    assert len(body['targets']) == 4 and review.proposed_count == 0
    assert all(target['state'] in ('rejected', 'unsupported') for target in body['targets'])
    assert all(target['stream_id'] is None and target['document_id'] is None for target in body['targets'])
    assert body['report']['sha256'] == review.report_sha256


@pytest.mark.parametrize('change', ['plan', 'project', 'workflow', 'root', 'proposals'])
def test_review_packet_refuses_foreign_or_modified_evidence(change):
    proof, args = fixture()
    if change == 'plan':
        args['plan_jcs'] = b'{}'
    elif change == 'project':
        args['project_id'] = uuid4()
    elif change == 'workflow':
        args['workflow_id'] = uuid4()
    elif change == 'root':
        proof = replace(proof, root_sha256='0'*64)
    else:
        proof = replace(proof, proposal_pins=((uuid4(), 'a'*64),))
    with pytest.raises((AdoptionInvalid, ValueError)):
        prepare_import_review(proof, **args)
