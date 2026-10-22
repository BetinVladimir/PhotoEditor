"""Actual same-review targets, admitted preview bundles and one decision.

Synthetic execution/qualification/object metadata; no worker/physical QA claim.
"""
import json
from retouch_apply_documents_fixture import prepared_retouch_review_fixtures
from retouch_preview_bundle_fixture import retouch_bundle_fixture
from retouch_copy_apply_fixture import publish_retouch_decision_bundles


def multi_selection_fixture(db,*,with_alpha=False,target_count=2):
    reviews=prepared_retouch_review_fixtures(db,with_alpha=with_alpha,target_count=target_count)
    bundles=[]
    pin=None
    for review in reviews:
        if pin is not None:
            review[2]['retouch_preview_qualification']=pin
        bundle=retouch_bundle_fixture(db,admitted=True,review=review)
        pin=bundle['scope']['retouch_preview_qualification']
        bundles.append(bundle)
    request,artifact,body=publish_retouch_decision_bundles(db,bundles)
    first=bundles[0]
    args=first['args']
    revision=int(json.loads(args[5])['workflow_revision'])
    return bundles,[*args[:4],revision,request,artifact,body,first['scope']['review_documents']]
