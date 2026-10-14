"""Final import root contains only already-published proposals and report shards."""
from dataclasses import dataclass
from hashlib import sha256
from typing import Annotated,Literal
from pydantic import Field
from photo_worker.contracts.base import ArtifactRef,Contract,canonical_bytes
from .adoption import AdoptionInvalid
from .interchange_mapping import MappingReport,MappingShard,PreparedMappingReport
from .interchange_parse import ParsedInterchange,validate_parsed_inventory


class InterchangeResult(Contract):
    schema_version: Literal[1]
    report: ArtifactRef
    bundle: ArtifactRef | None
    draft_shards: Annotated[list[ArtifactRef],Field(max_length=50)]
    imported_files: Annotated[list[ArtifactRef],Field(max_length=30000)]


@dataclass(frozen=True)
class PreparedImportResult:
    document: InterchangeResult
    document_jcs: bytes
    root: ArtifactRef
    mapping: PreparedMappingReport


def publish_import_result(parsed,mapping,publish_root,*,check_active=lambda:None):
    check_active()
    if not isinstance(parsed,ParsedInterchange) or not isinstance(mapping,PreparedMappingReport) or not callable(publish_root):
        raise ValueError('INTERCHANGE_IMPORT_RESULT_CONFIGURATION')
    parsed=ParsedInterchange.model_validate_json(canonical_bytes(parsed))
    try:
        validate_parsed_inventory(parsed)
    except ValueError as error:
        raise AdoptionInvalid('INTERCHANGE_IMPORT_PARSED_INVENTORY') from error
    report=MappingReport.model_validate_json(mapping.report_jcs)
    if (canonical_bytes(report)!=mapping.report_jcs
            or sha256(mapping.report_jcs).hexdigest()!=mapping.report.sha256
            or tuple(report.shards)!=mapping.shards or report.entry_count!=len(parsed.manifest.entries)
            or len(mapping.shards)!=(report.entry_count+99)//100
            or len(mapping.shard_bodies)!=len(mapping.shards)):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_REPORT_MISMATCH')
    # Preserve all opaque parser outputs, including dependencies not directly
    # referenced by the report. They remain declared inputs at final adoption.
    imported=[file.content.model_copy(deep=True) for file in parsed.files]
    imported_ids={ref.artifact_id for ref in imported}
    outputs=[mapping.report,*mapping.shards,*mapping.proposals]
    if (len(imported_ids)!=len(imported) or any(ref.artifact_id.int==0 for ref in imported)
            or len({ref.artifact_id for ref in outputs})!=len(outputs)
            or imported_ids & {ref.artifact_id for ref in outputs}):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_RESULT_ALIAS')
    for ref,body in zip(mapping.shards,mapping.shard_bodies):
        if sha256(body).hexdigest()!=ref.sha256:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_SHARD_HASH')
    shard_entries=[]
    for ordinal,body in enumerate(mapping.shard_bodies):
        shard=MappingShard.model_validate_json(body)
        expected_count=min(100,report.entry_count-ordinal*100)
        if canonical_bytes(shard)!=body or len(shard.entries)!=expected_count:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_SHARD_CANONICAL')
        shard_entries.extend(shard.entries)
    if (len(shard_entries)!=len(parsed.manifest.entries)
            or [(entry.entry_id,entry.external_id) for entry in shard_entries]
                != [(entry.entry_id,entry.external_id) for entry in parsed.manifest.entries]
            or tuple(entry.proposed_document for entry in shard_entries if entry.proposed_document is not None)
                != mapping.proposals
            or sum(any(field.status=='rejected' for field in entry.fields) for entry in shard_entries)
                != report.rejected_count
            or sum(any(field.status=='unsupported' for field in entry.fields) for entry in shard_entries)
                != report.unsupported_count):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_REPORT_CONTENT')
    document=InterchangeResult(schema_version=1,report=mapping.report.model_copy(deep=True),bundle=None,
        draft_shards=[ref.model_copy(deep=True) for ref in mapping.shards],imported_files=imported)
    body=canonical_bytes(document)
    if len(body)>8<<20 or len(outputs)+1>5052:
        raise AdoptionInvalid('INTERCHANGE_IMPORT_RESULT_LIMIT')
    check_active()
    root=publish_root(document.model_copy(deep=True))
    if (not isinstance(root,ArtifactRef) or root.artifact_id.int==0 or root.artifact_id in imported_ids
            or root.artifact_id in {ref.artifact_id for ref in outputs} or root.sha256!=sha256(body).hexdigest()):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ROOT_PUBLICATION')
    check_active()
    return PreparedImportResult(document,body,root.model_copy(deep=True),mapping)
