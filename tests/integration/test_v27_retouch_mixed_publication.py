"""Own retouch decision publication; authored SQL scenarios, no physical QA."""
import json

import pytest

from retouch_apply_documents_fixture import prepared_retouch_review_fixtures
from retouch_copy_apply_fixture import publish_retouch_decision_bundles
from retouch_preview_bundle_fixture import retouch_bundle_fixture


@pytest.mark.parametrize('reverse', [False, True])
def test_mixed_retouch_seals_only_approved(db, monkeypatch, reverse):
    reviews = prepared_retouch_review_fixtures(db, target_count=2)
    bundles = []
    pin = None
    for review in reviews:
        if pin is not None:
            review[2]['retouch_preview_qualification'] = pin
        bundle = retouch_bundle_fixture(db, admitted=True, review=review)
        pin = bundle['scope']['retouch_preview_qualification']
        bundles.append(bundle)
    import test_v27_retouch_preview_receipt as receipts
    original = receipts.seal
    sealed = []

    def record_seal(connection, bundle):
        sealed.append(bundle['target'])
        return original(connection, bundle)

    monkeypatch.setattr(receipts, 'seal', record_seal)
    decisions = {bundles[0]['target']: 'approve', bundles[1]['target']: 'reject'}
    request, _, body = publish_retouch_decision_bundles(
        db, bundles, decisions=decisions, reverse_targets=reverse)
    assert sealed == [bundles[0]['target']]
    for targets in (json.loads(request)['request']['target_decisions'], json.loads(body)['targets']):
        by_id = {item['target_id']: item for item in targets}
        assert by_id[str(bundles[0]['target'])]['decision'] == 'approve'
        assert 'preview' in by_id[str(bundles[0]['target'])]
        assert by_id[str(bundles[1]['target'])]['decision'] == 'reject'
        assert 'preview' not in by_id[str(bundles[1]['target'])]
    assert db.execute('SELECT v27_retouch_preview_receipt FROM photo.runs WHERE id=%s',
                      (bundles[1]['run'],)).fetchone()[0] is None


@pytest.mark.parametrize('invalid', ['missing', 'extra', 'unknown', 'duplicate_string_key'])
def test_retouch_decision_inventory_refused_before_io(invalid):
    """The decision inventory is validated before SQL or receipt access."""
    from uuid import uuid4
    target, extra = uuid4(), uuid4()
    params = dict(workflow_id=str(uuid4()), workflow_revision='1', target_id=str(target),
                  plan=dict(artifact_id=str(uuid4()), sha256='a'*64),
                  intent=dict(artifact_id=str(uuid4()), sha256='b'*64))
    documents = [dict(contract=contract, **params[key]) for contract, key in
                 [('SemanticPlan', 'plan'), ('EditIntent', 'intent')]]
    bundle = dict(target=target, args=[uuid4(), uuid4(), uuid4(), uuid4(), target, json.dumps(params)],
                  scope=dict(snapshot=uuid4(), review_documents=documents))
    decisions = {
        'missing': {},
        'extra': {target: 'approve', extra: 'reject'},
        'unknown': {target: 'skip'},
        'duplicate_string_key': {target: 'approve', str(target): 'reject'},
    }[invalid]

    class NoIO:
        def execute(self, *args, **kwargs):
            raise AssertionError('invalid inventory reached SQL')

    with pytest.raises(ValueError, match='exactly the selected bundles'):
        publish_retouch_decision_bundles(NoIO(), [bundle], decisions=decisions)
