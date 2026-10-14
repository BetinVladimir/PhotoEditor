import json
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_import_evidence import freeze_import_evidence
from photo_worker.v2.interchange_task_documents import freeze_import_execution_task_documents, publish_import_task_documents
from test_interchange_import_evidence import evidence_arguments


def test_execution_documents_publish_plan_evidence_params_in_order():
    args = evidence_arguments();evidence = freeze_import_evidence(**args)
    docs = freeze_import_execution_task_documents(args['workflow_id'], args['sources'].parsed,
        args['plan'], args['parsed_jcs'], evidence)
    assert json.loads(docs.params_jcs)['evidence'] == evidence.reference.model_dump(mode='json')
    calls = []
    def publish(contract, ref, body):
        calls.append(contract)
        return ref
    assert publish_import_task_documents(docs, publish, lambda: True) == docs.params_ref
    assert calls == ['InterchangePlan', 'FrozenInterchangeImportEvidence', 'V2InterchangeImportParams']


def test_execution_document_revocation_after_evidence_never_publishes_params():
    args = evidence_arguments();evidence = freeze_import_evidence(**args)
    docs = freeze_import_execution_task_documents(args['workflow_id'], args['sources'].parsed,
        args['plan'], args['parsed_jcs'], evidence)
    calls = [];checks = iter([True, True, True, False])
    def publish(contract, ref, body):
        calls.append(contract)
        return ref
    with pytest.raises(AdoptionInvalid, match='SOURCE_STALE'):
        publish_import_task_documents(docs, publish, lambda: next(checks))
    assert calls == ['InterchangePlan', 'FrozenInterchangeImportEvidence']
