from hashlib import sha256
from uuid import uuid5
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_frozen_import import prepare_import_from_params
from photo_worker.v2.interchange_import_evidence import freeze_import_evidence
from photo_worker.v2.interchange_task_documents import freeze_import_execution_task_documents
from test_interchange_import_stage import adopted_fixture


@pytest.mark.parametrize('failure', ['none', 'params', 'evidence'])
def test_executable_params_drive_verified_report_and_root(failure):
    parsed, sources, metadata, image = adopted_fixture()
    from test_interchange_plan_compile import compile
    plan = compile(parsed, sources, lambda *unused: image)
    task = uuid5(sources.workflow_id, 'v2.interchange.import:0')
    plan_ref = ArtifactRef(artifact_id=uuid5(task, 'interchange:plan:1'), sha256=sha256(canonical_bytes(plan)).hexdigest())
    evidence = freeze_import_evidence(workflow_id=sources.workflow_id, plan_ref=plan_ref,
        plan=plan, parsed_jcs=parsed, sources=sources, metadata=metadata, recipe_diagnostics={})
    docs = freeze_import_execution_task_documents(sources.workflow_id, sources.parsed, plan, parsed, evidence)
    bodies = {docs.plan_ref.artifact_id: docs.plan_jcs, docs.evidence_ref.artifact_id: evidence.body,
              sources.parsed.artifact_id: parsed, sources.entries[0].master.artifact_id: image}
    reads, writes = [], []
    def read(ref, maximum):
        reads.append(ref)
        return b'{}' if failure == 'evidence' and ref == evidence.reference else bodies[ref.artifact_id]
    def publish(raw, *unused):
        writes.append(raw)
        from uuid import uuid4
        return ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    kwargs = dict(params_ref=docs.params_ref, params_jcs=b'{}' if failure == 'params' else docs.params_jcs,
        task_id=task, workflow_id=sources.workflow_id, project_id=sources.project_id,
        snapshot_digest='f'*64, read_document=read, publish_document=publish,
        publish_root=lambda document: publish(canonical_bytes(document)))
    if failure == 'none':
        result = prepare_import_from_params(**kwargs)
        assert result.document.bundle is None and len(writes) == 3
        assert result.document.imported_files
    else:
        with pytest.raises((AdoptionInvalid, ValueError)):
            prepare_import_from_params(**kwargs)
        assert writes == []
        if failure == 'params':
            assert reads == []
