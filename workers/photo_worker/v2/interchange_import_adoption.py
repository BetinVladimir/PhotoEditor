"""Independent reconstruction of import outputs before fenced SQL adoption.

Readers must enforce the scheduler's exact declared-input and staged-output
inventories and immutable object versions. This module performs no mutations;
SQL commit must recheck current lease/result, source metadata and permissions.
"""
from dataclasses import dataclass
from hashlib import sha256
from uuid import UUID

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from .adoption import AdoptionInvalid, require_adoption_active
from .interchange_frozen_import import prepare_import_from_params
from .interchange_import_result import InterchangeResult
from .interchange_mapping import MappingReport


@dataclass(frozen=True)
class VerifiedImportOutput:
    params_id: UUID
    params_sha256: str
    params_jcs: bytes
    root_id: UUID
    root_sha256: str
    root_jcs: bytes
    report_jcs: bytes
    shard_bodies: tuple[bytes, ...]
    proposal_pins: tuple[tuple[UUID, str], ...]
    staged_pins: tuple[tuple[UUID, str, str], ...]
    proposal_closures_jcs: bytes = b'[]'


def verify_import_output(*, root, staged_entries, params_ref, params_jcs,
        task_id, workflow_id, project_id, snapshot_digest, read_input,
        read_output, qualify_recipe=None, max_output_bytes,
        check_active=lambda: None):
    """Rebuild every report byte, never accepting worker-supplied status claims.

    staged_entries is an authoritative sequence of (ArtifactRef, contract),
    including root. Unreachable/duplicate/extra staged outputs are rejected.
    Recipes remain declared inputs, never additional staged outputs.
    """
    if not callable(check_active):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_CONFIGURATION')
    supplied_check_active = check_active
    def check_active():
        require_adoption_active(supplied_check_active)
    check_active()
    if (not isinstance(root, ArtifactRef) or root.artifact_id.int == 0
            or type(max_output_bytes) is not int or not 1 <= max_output_bytes <= 2**53-1
            or not all(callable(p) for p in (read_input, read_output, check_active))):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_CONFIGURATION')
    root = root.model_copy(deep=True)
    inventory = {}
    for entry in staged_entries:
        check_active()
        if not isinstance(entry, (tuple, list)) or len(entry) != 2:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_INVENTORY')
        ref, contract = entry
        if (not isinstance(ref, ArtifactRef) or ref.artifact_id.int == 0
                or contract not in ('InterchangeResult', 'MappingReport', 'MappingShard')
                or ref.artifact_id in inventory or len(inventory) >= 52):
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_INVENTORY')
        inventory[ref.artifact_id] = (ref.model_copy(deep=True), contract)
    if not isinstance(params_ref, ArtifactRef) or params_ref.artifact_id in inventory:
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_INPUT_OUTPUT_ALIAS')
    consumed, total = set(), 0
    input_total = 0

    def declared_input(ref, limit):
        nonlocal input_total
        check_active()
        if ref.artifact_id in inventory:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_INPUT_OUTPUT_ALIAS')
        bound = min(limit, (128 << 20)-input_total)
        if bound < 2:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_INPUT_BUDGET')
        raw = read_input(ref.model_copy(deep=True), bound)
        check_active()
        if (not isinstance(raw, bytes) or not 2 <= len(raw) <= bound
                or sha256(raw).hexdigest() != ref.sha256):
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_INPUT_HASH')
        input_total += len(raw)
        return raw

    def read(ref, contract):
        nonlocal total
        check_active()
        if inventory.get(ref.artifact_id) != (ref, contract) or ref.artifact_id in consumed:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_OUTPUT_SCOPE')
        remaining = min(8 << 20, max_output_bytes-total)
        if remaining < 2:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_OUTPUT_BUDGET')
        raw = read_output(ref.model_copy(deep=True), remaining)
        check_active()
        if (not isinstance(raw, bytes) or not 2 <= len(raw) <= remaining
                or sha256(raw).hexdigest() != ref.sha256):
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_OUTPUT_HASH')
        consumed.add(ref.artifact_id)
        total += len(raw)
        return raw

    root_raw = read(root, 'InterchangeResult')
    result = InterchangeResult.model_validate_json(root_raw)
    if canonical_bytes(result) != root_raw or result.bundle is not None:
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_ROOT')
    report_raw = read(result.report, 'MappingReport')
    report = MappingReport.model_validate_json(report_raw)
    if (canonical_bytes(report) != report_raw or result.draft_shards != report.shards
            or len(report.shards) != (report.entry_count+99)//100):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_REPORT')
    shard_bodies = tuple(read(ref, 'MappingShard') for ref in report.shards)
    if consumed != set(inventory):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_UNREACHABLE_OUTPUT')
    publications = set()

    def compare(raw, contract, ordinal):
        check_active()
        key = (contract, ordinal)
        if key in publications:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_DUPLICATE_PUBLICATION')
        if contract == 'MappingShard' and type(ordinal) is int and 0 <= ordinal < len(shard_bodies):
            expected, reference = shard_bodies[ordinal], report.shards[ordinal]
        elif contract == 'MappingReport' and ordinal == 0:
            expected, reference = report_raw, result.report
        else:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_PUBLICATION_SCOPE')
        if raw != expected:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_MAPPING_CHANGED')
        publications.add(key)
        return reference.model_copy(deep=True)

    def compare_root(document):
        check_active()
        if canonical_bytes(document) != root_raw:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_ROOT_CHANGED')
        return root.model_copy(deep=True)

    recipe_closures = []
    closure_count = 0
    def qualify_with_receipt(ref, recipe, binding):
        nonlocal closure_count
        check_active()
        receipt = getattr(qualify_recipe, 'qualified_document_refs', None)
        if not callable(qualify_recipe) or not callable(receipt):
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_CLOSURE_RECEIPT_REQUIRED')
        if qualify_recipe(ref, recipe, binding) is not True:
            return False
        refs = receipt(ref, binding)
        if not isinstance(refs, tuple) or len(refs) > 30000:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_RECIPE_REFS')
        seen = set()
        owned = []
        for ordinal, item in enumerate(refs):
            check_active()
            if not isinstance(item, dict) or set(item) != {'artifact_id', 'sha256', 'purpose', 'ordinal'}:
                raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_RECIPE_REFS')
            child = ArtifactRef.model_validate_json(canonical_bytes({name: item[name] for name in ('artifact_id', 'sha256')}))
            purpose = sha256(('interchange/recipe/'+str(ref.artifact_id)+'/dependency/'+str(child.artifact_id)).encode()).hexdigest()
            if (child.artifact_id in seen or child.artifact_id == ref.artifact_id
                    or child.artifact_id in inventory or item['purpose'] != purpose
                    or type(item['ordinal']) is not int or item['ordinal'] != ordinal):
                raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_RECIPE_REFS')
            seen.add(child.artifact_id)
            owned.append(dict(artifact_id=str(child.artifact_id), sha256=child.sha256, purpose=purpose, ordinal=ordinal))
        closure_count += len(owned)
        if closure_count > 1000000:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_RECIPE_REF_LIMIT')
        recipe_closures.append(dict(recipe=ref.model_dump(mode='json'), refs=owned))
        return True

    reconstructed = prepare_import_from_params(params_ref=params_ref, params_jcs=params_jcs,
        task_id=task_id, workflow_id=workflow_id, project_id=project_id,
        snapshot_digest=snapshot_digest, read_document=declared_input,
        publish_document=compare, publish_root=compare_root,
        qualify_recipe=qualify_with_receipt, check_active=check_active)
    if publications != {('MappingReport', 0)} | {('MappingShard', n) for n in range(len(shard_bodies))}:
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_INCOMPLETE_MAPPING')
    closures_raw = canonical_bytes(recipe_closures)
    if len(closures_raw) > 128 << 20:
        raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_RECIPE_REF_LIMIT')
    check_active()
    return VerifiedImportOutput(params_ref.artifact_id, params_ref.sha256, bytes(params_jcs),
        root.artifact_id, root.sha256, root_raw, report_raw,
        shard_bodies, tuple((ref.artifact_id, ref.sha256) for ref in reconstructed.mapping.proposals),
        tuple(sorted(((ref.artifact_id, ref.sha256, contract) for ref, contract in inventory.values()),
                     key=lambda item: str(item[0]))), closures_raw)
