"""Deterministic scheduler-owned import plan/Params publication."""
from dataclasses import dataclass
from hashlib import sha256
from uuid import UUID, uuid5
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from .adoption import AdoptionInvalid
from .interchange_plan import InterchangePlan, InterchangeImportParams


@dataclass(frozen=True)
class ImportTaskDocuments:
    task_id: UUID
    plan_id: UUID
    plan_jcs: bytes
    params_id: UUID
    params_jcs: bytes
    evidence_id: UUID | None = None
    evidence_jcs: bytes | None = None

    @property
    def plan_ref(self):
        return ArtifactRef(artifact_id=self.plan_id, sha256=sha256(self.plan_jcs).hexdigest())

    @property
    def params_ref(self):
        return ArtifactRef(artifact_id=self.params_id, sha256=sha256(self.params_jcs).hexdigest())

    @property
    def evidence_ref(self):
        if self.evidence_id is None or self.evidence_jcs is None:
            return None
        return ArtifactRef(artifact_id=self.evidence_id, sha256=sha256(self.evidence_jcs).hexdigest())


def freeze_import_task_documents(workflow_id, parsed_ref, plan):
    """Caller must resolve and reserve the entire input closure before sealing."""
    if (not isinstance(workflow_id, UUID) or workflow_id.int == 0
            or not isinstance(parsed_ref, ArtifactRef) or parsed_ref.artifact_id.int == 0
            or not isinstance(plan, InterchangePlan) or plan.direction != 'import'):
        raise AdoptionInvalid('INTERCHANGE_TASK_DOCUMENT_INPUT')
    plan_jcs = canonical_bytes(plan)
    if not 2 <= len(plan_jcs) <= 8 << 20:
        raise AdoptionInvalid('INTERCHANGE_TASK_PLAN_SIZE')
    # Revalidate mutable model instances before using their current values.
    InterchangePlan.model_validate_json(plan_jcs)
    task_id = uuid5(workflow_id, 'v2.interchange.import:0')
    plan_id = uuid5(task_id, 'interchange:plan:1')
    params_id = uuid5(task_id, 'interchange:params:1')
    plan_ref = ArtifactRef(artifact_id=plan_id, sha256=sha256(plan_jcs).hexdigest())
    params = InterchangeImportParams(schema_version=1, parsed=parsed_ref.model_copy(deep=True),
                                    plan=plan_ref, max_report_bytes=8 << 20)
    if parsed_ref.artifact_id in (plan_id, params_id):
        raise AdoptionInvalid('INTERCHANGE_TASK_DOCUMENT_ALIAS')
    return ImportTaskDocuments(task_id, plan_id, plan_jcs, params_id, canonical_bytes(params))


def freeze_import_execution_task_documents(workflow_id, parsed_ref, plan, parsed_jcs, evidence):
    from .interchange_import_evidence import PreparedImportEvidence, read_import_evidence
    if not isinstance(evidence, PreparedImportEvidence):
        raise AdoptionInvalid('INTERCHANGE_TASK_EVIDENCE_REQUIRED')
    documents = freeze_import_task_documents(workflow_id, parsed_ref, plan)
    checked = read_import_evidence(evidence.reference, evidence.body, workflow_id=workflow_id,
        plan_ref=documents.plan_ref, plan=plan, parsed_jcs=parsed_jcs)
    if checked.parsed != parsed_ref:
        raise AdoptionInvalid('INTERCHANGE_TASK_EVIDENCE_PARSED')
    params = InterchangeImportParams(schema_version=1, parsed=parsed_ref.model_copy(deep=True),
        plan=documents.plan_ref, evidence=evidence.reference, max_report_bytes=8 << 20)
    return ImportTaskDocuments(documents.task_id, documents.plan_id, documents.plan_jcs,
        documents.params_id, canonical_bytes(params), evidence.artifact_id, evidence.body)


def publish_import_task_documents(documents, publish_document, recheck_current, *, check_active=lambda: None):
    """Publish plan before Params, with current-source checks around object IO.

    Publisher must implement reserve/complete and exact-replay ownership. Objects
    left by cancellation are handled by ordinary retention/GC; this never creates
    runnable tasks or treats publication as final admission.
    """
    if (not isinstance(documents, ImportTaskDocuments) or not callable(publish_document)
            or not callable(recheck_current)):
        raise AdoptionInvalid('INTERCHANGE_TASK_PUBLICATION_INPUT')
    plan = InterchangePlan.model_validate_json(documents.plan_jcs)
    params = InterchangeImportParams.model_validate_json(documents.params_jcs)
    if (plan.direction != 'import' or params.plan != documents.plan_ref
            or documents.plan_id != uuid5(documents.task_id, 'interchange:plan:1')
            or documents.params_id != uuid5(documents.task_id, 'interchange:params:1')
            or canonical_bytes(plan) != documents.plan_jcs or canonical_bytes(params) != documents.params_jcs):
        raise AdoptionInvalid('INTERCHANGE_TASK_DOCUMENT_MISMATCH')
    publications = [('InterchangePlan', documents.plan_ref, documents.plan_jcs)]
    if documents.evidence_ref is not None:
        from .interchange_import_evidence import FrozenInterchangeImportEvidence
        evidence = FrozenInterchangeImportEvidence.model_validate_json(documents.evidence_jcs)
        if (documents.evidence_id != uuid5(documents.task_id, 'interchange:evidence:1')
                or evidence.plan != documents.plan_ref or evidence.parsed != params.parsed
                or canonical_bytes(evidence) != documents.evidence_jcs or params.evidence != documents.evidence_ref):
            raise AdoptionInvalid('INTERCHANGE_TASK_EVIDENCE_MISMATCH')
        publications.append(('FrozenInterchangeImportEvidence', documents.evidence_ref, documents.evidence_jcs))
    elif params.evidence is not None or documents.evidence_id is not None or documents.evidence_jcs is not None:
        raise AdoptionInvalid('INTERCHANGE_TASK_EVIDENCE_MISMATCH')
    publications.append(('V2InterchangeImportParams', documents.params_ref, documents.params_jcs))
    for contract, reference, body in publications:
        check_active()
        if recheck_current() is not True:
            raise AdoptionInvalid('INTERCHANGE_TASK_SOURCE_STALE')
        actual = publish_document(contract, reference.model_copy(deep=True), body)
        check_active()
        if not isinstance(actual, ArtifactRef) or actual != reference:
            raise AdoptionInvalid('INTERCHANGE_TASK_PUBLICATION_MISMATCH')
        if recheck_current() is not True:
            raise AdoptionInvalid('INTERCHANGE_TASK_SOURCE_STALE')
    return documents.params_ref
