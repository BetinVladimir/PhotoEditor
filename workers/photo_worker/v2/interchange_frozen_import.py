"""Execute the frozen server-prepared import plan without remapping in a worker."""
from hashlib import sha256

from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from .adoption import AdoptionInvalid
from .interchange_import_sources import ImportSources
from .interchange_mapping import MappingEntry, MappingField, publish_mapping_report
from .interchange_metadata import qualified_metadata_mapper
from .interchange_parse import ParsedInterchange, validate_parsed_inventory
from .interchange_plan import read_interchange_plan
from .render_recipe import EditingRecipeV3


def prepare_frozen_import_stage(*, plan_ref, plan_jcs, parsed_jcs, sources,
                                snapshot_digest, read_document, publish_document,
                                metadata_receipts=None, recipe_diagnostics=None,
                                qualify_recipe=None, check_active=lambda: None):
    """All readers/qualifiers must enforce the registered declared-input closure.

    Prepared recipes are immutable inputs, never outputs minted by this stage.
    The qualifier checks their complete typed dependency closure against the
    installed runtime and binding. Diagnostics for absent prepared recipes must
    come from frozen compiler evidence; storage failures are never unsupported.
    Final adoption independently rechecks current SQL authority and receipts.
    """
    check_active()
    if (not isinstance(plan_ref, ArtifactRef) or plan_ref.artifact_id.int == 0
            or not isinstance(plan_jcs, bytes) or not 2 <= len(plan_jcs) <= 8 << 20
            or sha256(plan_jcs).hexdigest() != plan_ref.sha256
            or not isinstance(parsed_jcs, bytes) or not 2 <= len(parsed_jcs) <= 8 << 20
            or not isinstance(sources, ImportSources)
            or not callable(read_document) or not callable(publish_document)):
        raise AdoptionInvalid('INTERCHANGE_FROZEN_INPUT')
    sources = ImportSources.model_validate_json(canonical_bytes(sources))
    if sha256(parsed_jcs).hexdigest() != sources.parsed.sha256:
        raise AdoptionInvalid('INTERCHANGE_FROZEN_PARSED_HASH')
    parsed = ParsedInterchange.model_validate_json(parsed_jcs)
    try:
        validate_parsed_inventory(parsed)
    except ValueError as error:
        raise AdoptionInvalid('INTERCHANGE_FROZEN_INVENTORY') from error
    plan = read_interchange_plan(plan_jcs, project_id=sources.project_id,
                                direction='import', snapshot_digest=snapshot_digest)
    if plan.format != sources.format or canonical_bytes(plan) != plan_jcs or canonical_bytes(parsed) != parsed_jcs:
        raise AdoptionInvalid('INTERCHANGE_FROZEN_CANONICAL')
    if len(plan.entries) != len(parsed.manifest.entries) or len(sources.entries) != len(plan.entries):
        raise AdoptionInvalid('INTERCHANGE_FROZEN_COUNT')
    parsed_files = {file.path: file for file in parsed.files}
    if len(parsed_files) != len(parsed.files):
        raise AdoptionInvalid('INTERCHANGE_FROZEN_DUPLICATE_FILE')
    files = {path: file.content for path, file in parsed_files.items()}
    diagnostics = dict(recipe_diagnostics or {})
    expected_diagnostics = set()
    for ordinal, (entry, original, source) in enumerate(zip(plan.entries, parsed.manifest.entries, sources.entries)):
        check_active()
        if (source.ordinal != ordinal or source.entry_id != original.entry_id
                or entry.entry_id != original.entry_id or entry.external_id != original.external_id
                or entry.path != original.image_path or entry.content != files.get(original.image_path)
                or entry.rating != original.rating or entry.pick != original.pick or entry.crop != original.crop
                or entry.label != original.label):
            raise AdoptionInvalid('INTERCHANGE_FROZEN_ENTRY')
        if source.state != 'adopted':
            if entry.binding is not None or entry.recipe is not None:
                raise AdoptionInvalid('INTERCHANGE_FROZEN_TERMINAL_BINDING')
        elif (entry.binding is None or entry.binding.asset_version_id != source.asset_version_id
              or entry.binding.source != source.master):
            raise AdoptionInvalid('INTERCHANGE_FROZEN_BINDING')
        elif entry.recipe is not None:
            if original.recipe_path is None or not callable(qualify_recipe):
                raise AdoptionInvalid('INTERCHANGE_FROZEN_RECIPE_QUALIFIER')
        elif original.recipe_path is not None:
            expected_diagnostics.add(entry.entry_id)
    if (set(diagnostics) != expected_diagnostics
            or any(not isinstance(reason, str) or not 1 <= len(reason) <= 200 for reason in diagnostics.values())):
        raise AdoptionInvalid('INTERCHANGE_FROZEN_DIAGNOSTICS')
    adopted = any(source.state == 'adopted' for source in sources.entries)
    if adopted and metadata_receipts is None:
        raise AdoptionInvalid('INTERCHANGE_METADATA_RECEIPTS_REQUIRED')
    map_metadata = qualified_metadata_mapper(metadata_receipts, sources) if adopted else None
    mappings = []

    def read(ref, limit):
        check_active()
        raw = read_document(ref.model_copy(deep=True), limit)
        check_active()
        if not isinstance(raw, bytes) or not 2 <= len(raw) <= limit or sha256(raw).hexdigest() != ref.sha256:
            raise AdoptionInvalid('INTERCHANGE_FROZEN_DOCUMENT_HASH')
        return raw

    for entry, original, source in zip(plan.entries, parsed.manifest.entries, sources.entries):
        check_active()
        if source.state != 'adopted':
            fields = [MappingField(name='image', status='rejected' if source.state == 'rejected' else 'unsupported', reason=source.reason)]
        else:
            image_raw = read(source.master, 1 << 20)
            image = WorkingImage.model_validate_json(image_raw)
            binding = VersionBinding(asset_version_id=source.asset_version_id, source=source.master,
                                     canvas_hash=canonical_hash(image.canvas))
            if (canonical_bytes(image) != image_raw or binding != entry.binding
                    or image.canvas.source_sha256 != original.image_sha256 or image.dtype != 'float32'
                    or image.color.profile_id != 'work_rec2020_gamma24_v1' or image.color.encoding != 'gamma24'):
                raise AdoptionInvalid('INTERCHANGE_FROZEN_MASTER')
            fields = [MappingField(name='image', status='exact', reason=None)]
            fields.extend(map_metadata(original.model_copy(deep=True), binding.model_copy(deep=True)))
            if entry.recipe is None:
                fields.append(MappingField(name='recipe', status='unsupported', reason=diagnostics.get(entry.entry_id, 'recipe_not_present')))
            else:
                raw = read(entry.recipe, 8 << 20)
                recipe = EditingRecipeV3.model_validate_json(raw)
                if canonical_bytes(recipe) != raw or recipe.source != binding or recipe.learning_version_id is not None:
                    raise AdoptionInvalid('INTERCHANGE_FROZEN_RECIPE_BINDING')
                if qualify_recipe(entry.recipe.model_copy(deep=True), recipe.model_copy(deep=True), binding.model_copy(deep=True)) is not True:
                    raise AdoptionInvalid('INTERCHANGE_FROZEN_RECIPE_CLOSURE')
                check_active()
                fields.append(MappingField(name='recipe', status='exact', reason=None))
        from .interchange_sidecar_mapping import sidecar_mapping_fields
        fields.extend(sidecar_mapping_fields(original, parsed_files,
            read, applied_label=source.state == 'adopted', check_active=check_active))
        mappings.append(MappingEntry(entry_id=entry.entry_id, external_id=entry.external_id,
            asset_version_id=source.asset_version_id, proposed_document=entry.recipe, fields=fields))
    check_active()
    return publish_mapping_report(mappings, [(e.entry_id, e.external_id) for e in parsed.manifest.entries],
                                  publish_document, check_active=check_active)


def prepare_frozen_import_output(*, publish_root, **stage_arguments):
    """Publish the final root only after all prepared inputs and report complete."""
    from .interchange_import_result import publish_import_result
    parsed = ParsedInterchange.model_validate_json(stage_arguments['parsed_jcs'])
    mapping = prepare_frozen_import_stage(**stage_arguments)
    return publish_import_result(parsed, mapping, publish_root,
        check_active=stage_arguments.get('check_active', lambda: None))


def prepare_import_from_evidence(*, evidence_ref, evidence_jcs, workflow_id,
        project_id, plan_ref, plan_jcs, parsed_jcs, snapshot_digest,
        read_document, publish_document, publish_root, qualify_recipe=None,
        check_active=lambda: None):
    """Caller reads evidence/plan/parsed through exact declared-input pins."""
    from .interchange_import_evidence import read_import_evidence
    check_active()
    if not callable(publish_root):
        raise AdoptionInvalid('INTERCHANGE_FROZEN_ROOT_PUBLISHER')
    plan = read_interchange_plan(plan_jcs, project_id=project_id,
        direction='import', snapshot_digest=snapshot_digest)
    evidence = read_import_evidence(evidence_ref, evidence_jcs, workflow_id=workflow_id,
        plan_ref=plan_ref, plan=plan, parsed_jcs=parsed_jcs, check_active=check_active)
    return prepare_frozen_import_output(publish_root=publish_root, plan_ref=plan_ref,
        plan_jcs=plan_jcs, parsed_jcs=parsed_jcs, sources=evidence.sources,
        snapshot_digest=snapshot_digest, read_document=read_document,
        publish_document=publish_document, metadata_receipts=evidence.metadata,
        recipe_diagnostics={item.entry_id: item.reason for item in evidence.recipe_diagnostics},
        qualify_recipe=qualify_recipe, check_active=check_active)


def prepare_import_from_params(*, params_ref, params_jcs, task_id, workflow_id,
        project_id, snapshot_digest, read_document, publish_document, publish_root,
        qualify_recipe=None, check_active=lambda: None):
    """Leased-worker entry point; ports enforce current attempt and input pins."""
    from uuid import UUID, uuid5
    from .interchange_plan import read_executable_import_params
    check_active()
    if (not isinstance(workflow_id, UUID) or workflow_id.int == 0
            or task_id != uuid5(workflow_id, 'v2.interchange.import:0')
            or not all(callable(port) for port in (read_document, publish_document, publish_root))):
        raise AdoptionInvalid('INTERCHANGE_EXECUTION_SCOPE')
    params = read_executable_import_params(params_ref, params_jcs, task_id=task_id)
    def read(ref):
        check_active()
        raw = read_document(ref.model_copy(deep=True), 8 << 20)
        check_active()
        if not isinstance(raw, bytes) or not 2 <= len(raw) <= 8 << 20 or sha256(raw).hexdigest() != ref.sha256:
            raise AdoptionInvalid('INTERCHANGE_EXECUTION_ROOT_HASH')
        return raw
    plan_raw, parsed_raw, evidence_raw = read(params.plan), read(params.parsed), read(params.evidence)
    return prepare_import_from_evidence(evidence_ref=params.evidence, evidence_jcs=evidence_raw,
        workflow_id=workflow_id, project_id=project_id, plan_ref=params.plan, plan_jcs=plan_raw,
        parsed_jcs=parsed_raw, snapshot_digest=snapshot_digest, read_document=read_document,
        publish_document=publish_document, publish_root=publish_root,
        qualify_recipe=qualify_recipe, check_active=check_active)
