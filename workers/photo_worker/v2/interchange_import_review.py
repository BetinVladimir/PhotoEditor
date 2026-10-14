"""Owned deterministic review materialization from independently verified mapping.

This packet is not approval authority. Its consumer must atomically bind the
same task/result/report and current source metadata before creating draft rows.
"""
from dataclasses import dataclass
from hashlib import sha256
from uuid import UUID, uuid5
from photo_worker.contracts.base import canonical_bytes
from .adoption import AdoptionInvalid
from .interchange_import_adoption import VerifiedImportOutput
from .interchange_import_result import InterchangeResult
from .interchange_mapping import MappingReport, MappingShard
from .interchange_plan import read_interchange_plan


@dataclass(frozen=True)
class PreparedImportReview:
    workflow_id: UUID
    project_id: UUID
    report_id: UUID
    report_sha256: str
    body: bytes
    proposed_count: int


def prepare_import_review(verified, *, workflow_id, project_id, plan_jcs,
                          snapshot_digest, check_active=lambda: None):
    check_active()
    if (not isinstance(verified, VerifiedImportOutput)
            or not isinstance(workflow_id, UUID) or workflow_id.int == 0
            or not isinstance(project_id, UUID) or project_id.int == 0):
        raise AdoptionInvalid('INTERCHANGE_REVIEW_CONFIGURATION')
    from .interchange_plan import read_executable_import_params
    task = uuid5(workflow_id, 'v2.interchange.import:0')
    from photo_worker.contracts.base import ArtifactRef
    params = read_executable_import_params(ArtifactRef(artifact_id=verified.params_id,
        sha256=verified.params_sha256), verified.params_jcs, task_id=task)
    if sha256(plan_jcs).hexdigest() != params.plan.sha256:
        raise AdoptionInvalid('INTERCHANGE_REVIEW_PLAN_HASH')
    plan = read_interchange_plan(plan_jcs, project_id=project_id,
        direction='import', snapshot_digest=snapshot_digest)
    root = InterchangeResult.model_validate_json(verified.root_jcs)
    report = MappingReport.model_validate_json(verified.report_jcs)
    if (sha256(verified.root_jcs).hexdigest() != verified.root_sha256
            or sha256(verified.report_jcs).hexdigest() != root.report.sha256
            or root.bundle is not None or root.draft_shards != report.shards
            or len(report.shards) != len(verified.shard_bodies)
            or report.entry_count != len(plan.entries)):
        raise AdoptionInvalid('INTERCHANGE_REVIEW_REPORT_SCOPE')
    mappings = []
    for ref, raw in zip(report.shards, verified.shard_bodies):
        check_active()
        shard = MappingShard.model_validate_json(raw)
        if sha256(raw).hexdigest() != ref.sha256 or canonical_bytes(shard) != raw:
            raise AdoptionInvalid('INTERCHANGE_REVIEW_SHARD_HASH')
        mappings.extend(shard.entries)
    if len(mappings) != len(plan.entries):
        raise AdoptionInvalid('INTERCHANGE_REVIEW_CARDINALITY')
    targets, proposed, pins = [], 0, []
    for ordinal, (entry, mapping) in enumerate(zip(plan.entries, mappings)):
        check_active()
        binding = entry.binding
        if (mapping.entry_id != entry.entry_id or mapping.external_id != entry.external_id
                or mapping.proposed_document != entry.recipe
                or mapping.asset_version_id != (binding.asset_version_id if binding is not None else None)):
            raise AdoptionInvalid('INTERCHANGE_REVIEW_MAPPING_BINDING')
        proposal = mapping.proposed_document
        state = 'proposed' if proposal is not None else ('rejected' if any(
            field.status == 'rejected' for field in mapping.fields) else 'unsupported')
        stream = uuid5(workflow_id, 'interchange:draft-stream:'+str(entry.entry_id)) if proposal is not None else None
        document = uuid5(workflow_id, 'interchange:draft-document:'+str(entry.entry_id)) if proposal is not None else None
        if proposal is not None:
            proposed += 1
            pins.append((proposal.artifact_id, proposal.sha256))
        targets.append(dict(ordinal=ordinal, entry_id=str(entry.entry_id), external_id=entry.external_id,
            asset_version_id=str(mapping.asset_version_id) if mapping.asset_version_id is not None else None,
            state=state, stream_id=str(stream) if stream is not None else None,
            document_id=str(document) if document is not None else None,
            recipe=proposal.model_dump(mode='json') if proposal is not None else None,
            fields=[field.model_dump(mode='json') for field in mapping.fields]))
    if tuple(pins) != verified.proposal_pins:
        raise AdoptionInvalid('INTERCHANGE_REVIEW_PROPOSALS_CHANGED')
    body = canonical_bytes(dict(schema_version=1, workflow_id=str(workflow_id), project_id=str(project_id),
        task_id=str(task), root=dict(artifact_id=str(verified.root_id), sha256=verified.root_sha256),
        report=root.report.model_dump(mode='json'), targets=targets))
    if len(body) > 8 << 20:
        raise AdoptionInvalid('INTERCHANGE_REVIEW_SIZE')
    check_active()
    return PreparedImportReview(workflow_id, project_id, root.report.artifact_id, root.report.sha256, body, proposed)
