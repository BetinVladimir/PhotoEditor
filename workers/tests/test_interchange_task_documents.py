from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_plan import InterchangePlan
from photo_worker.v2.interchange_task_documents import freeze_import_task_documents,publish_import_task_documents
from test_interchange_plan import fixture


def frozen():
    plan=InterchangePlan.model_validate_json(canonical_bytes(fixture()))
    return freeze_import_task_documents(uuid4(),ArtifactRef(artifact_id=uuid4(),sha256='d'*64),plan)


def test_documents_are_deterministic_owned_and_published_dependency_first():
    docs=frozen();calls=[]
    def publish(contract,ref,body):calls.append((contract,ref,body));return ref
    assert publish_import_task_documents(docs,publish,lambda:True)==docs.params_ref
    assert [c[0] for c in calls]==['InterchangePlan','V2InterchangeImportParams']
    assert calls[0][2]==docs.plan_jcs and calls[1][2]==docs.params_jcs
    ref=docs.plan_ref;ref.sha256='e'*64
    assert docs.plan_ref.sha256!='e'*64


def test_revocation_after_plan_publication_never_publishes_params():
    docs=frozen();checks=iter([True,False]);calls=[]
    def publish(*args):calls.append(args);return args[1]
    with pytest.raises(AdoptionInvalid,match='SOURCE_STALE'):
        publish_import_task_documents(docs,publish,lambda:next(checks))
    assert len(calls)==1


def test_mismatched_publisher_receipt_blocks_params():
    docs=frozen();calls=[]
    def publish(*args):
        calls.append(args);return ArtifactRef(artifact_id=uuid4(),sha256=args[1].sha256)
    with pytest.raises(AdoptionInvalid,match='PUBLICATION_MISMATCH'):
        publish_import_task_documents(docs,publish,lambda:True)
    assert len(calls)==1


def test_nonboolean_current_evidence_is_not_authority():
    with pytest.raises(AdoptionInvalid,match='SOURCE_STALE'):
        publish_import_task_documents(frozen(),lambda *a:pytest.fail('unexpected IO'),lambda:1)


def test_freezing_same_workflow_and_inputs_is_exact_replay():
    workflow=uuid4();parsed=ArtifactRef(artifact_id=uuid4(),sha256='d'*64)
    plan=InterchangePlan.model_validate_json(canonical_bytes(fixture()))
    first=freeze_import_task_documents(workflow,parsed,plan)
    second=freeze_import_task_documents(workflow,parsed,plan)
    assert first==second
    plan.entries[0].rating=5
    assert freeze_import_task_documents(workflow,parsed,plan).plan_ref.sha256!=first.plan_ref.sha256
    assert first.plan_ref.artifact_id==second.plan_ref.artifact_id
