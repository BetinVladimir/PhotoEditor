"""Publish export mapping and root only after a verified bundle is staged."""
from dataclasses import dataclass
from hashlib import sha256

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from .interchange_import_result import InterchangeResult
from .interchange_mapping import MappingEntry, publish_mapping_report
from .interchange_plan import InterchangePlan


@dataclass(frozen=True)
class PreparedExportResult:
    root: ArtifactRef
    document_jcs: bytes
    bundle: ArtifactRef
    report: ArtifactRef
    shards: tuple[ArtifactRef, ...]
    output_json_bytes: int


def publish_export_result(plan, bundle, bundle_sha256, mappings, input_refs,
                          publish_document, publish_root, *, max_report_bytes=8 << 20,
                          check_active=lambda: None):
    """Publishers enforce current lease/quota and declared output roles.

    bundle_sha256 comes from physical scratch hashing before bundle upload.
    Source closure/SQL authority must be rechecked before final adoption.
    """
    if (not callable(publish_document) or not callable(publish_root) or not callable(check_active)
            or type(max_report_bytes) is not int or not 2 <= max_report_bytes <= 8 << 20
            or not isinstance(mappings, tuple) or len(mappings) > 5000
            or not isinstance(input_refs, tuple) or len(input_refs) > 30000):
        raise ValueError('INTERCHANGE_EXPORT_RESULT_CONFIGURATION')
    plan = InterchangePlan.model_validate_json(canonical_bytes(plan))
    bundle = ArtifactRef.model_validate_json(canonical_bytes(bundle))
    if plan.direction != 'export' or bundle.artifact_id.int == 0 or bundle.sha256 != bundle_sha256:
        raise ValueError('INTERCHANGE_EXPORT_BUNDLE_RECEIPT')
    forbidden = set()
    for ref in input_refs:
        ref = ArtifactRef.model_validate_json(canonical_bytes(ref))
        if ref.artifact_id.int == 0:
            raise ValueError('INTERCHANGE_EXPORT_INPUT_IDENTITY')
        forbidden.add(ref.artifact_id)
    ordered = sorted(plan.entries, key=lambda entry: entry.path)
    for entry in ordered:
        forbidden.update((entry.content.artifact_id, entry.recipe.artifact_id, entry.binding.source.artifact_id))
    if bundle.artifact_id in forbidden:
        raise ValueError('INTERCHANGE_EXPORT_OUTPUT_ALIAS')
    entries = tuple(MappingEntry.model_validate_json(canonical_bytes(item)) for item in mappings)
    if len(entries) != len(ordered):
        raise ValueError('INTERCHANGE_EXPORT_MAPPING_INVENTORY')
    for entry, mapped in zip(ordered, entries):
        if (entry.entry_id != mapped.entry_id or entry.external_id != mapped.external_id
                or entry.binding.asset_version_id != mapped.asset_version_id
                or mapped.proposed_document is not None):
            raise ValueError('INTERCHANGE_EXPORT_MAPPING_INVENTORY')
    forbidden.add(bundle.artifact_id)
    used, written = set(), 0
    check_active()

    def publish(body, contract, ordinal):
        nonlocal written
        check_active()
        if written + len(body) > max_report_bytes:
            raise ValueError('INTERCHANGE_EXPORT_REPORT_QUOTA')
        result = publish_document(body, contract, ordinal)
        if not isinstance(result, ArtifactRef):
            raise ValueError('INTERCHANGE_EXPORT_PUBLICATION_RECEIPT')
        result = ArtifactRef.model_validate_json(canonical_bytes(result))
        if (result.artifact_id.int == 0 or result.artifact_id in forbidden or result.artifact_id in used
                or result.sha256 != sha256(body).hexdigest()):
            raise ValueError('INTERCHANGE_EXPORT_PUBLICATION_RECEIPT')
        used.add(result.artifact_id);written += len(body)
        check_active()
        return result

    mapping = publish_mapping_report(entries, tuple((entry.entry_id, entry.external_id) for entry in ordered),
                                     publish, check_active=check_active)
    document = InterchangeResult(schema_version=1, report=mapping.report.model_copy(deep=True),
        bundle=bundle.model_copy(deep=True), draft_shards=[], imported_files=[])
    body = canonical_bytes(document)
    check_active()
    # The callback receives a fresh document so it cannot rewrite committed bytes.
    root = publish_root(InterchangeResult.model_validate_json(body))
    if not isinstance(root, ArtifactRef):
        raise ValueError('INTERCHANGE_EXPORT_ROOT_RECEIPT')
    root = ArtifactRef.model_validate_json(canonical_bytes(root))
    if (root.artifact_id.int == 0 or root.artifact_id in forbidden or root.artifact_id in used
            or root.sha256 != sha256(body).hexdigest()):
        raise ValueError('INTERCHANGE_EXPORT_ROOT_RECEIPT')
    check_active()
    return PreparedExportResult(root, body, bundle.model_copy(deep=True),
        mapping.report.model_copy(deep=True), tuple(ref.model_copy(deep=True) for ref in mapping.shards),
        written + len(body))
