"""Compile a complete import plan from verified parse and current ingest evidence."""
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from .adoption import AdoptionInvalid
from .interchange_import_sources import ImportSources
from .interchange_parse import ParsedInterchange, validate_parsed_inventory
from .interchange_plan import InterchangeEntry, InterchangePlan
from .interchange_recipe import ImportedRecipeProposal


def compile_import_plan(*, parsed_jcs, sources, snapshot_digest, limits,
                        prepared_recipes, read_document, check_active=lambda: None):
    """Prepared recipe receipts are server-owned; publication is a later step.

    Caller must recheck SQL authority before task materialization. This compiler
    does not approve recipes, publish objects, or manufacture missing bindings.
    Terminal reasons remain in the frozen ImportSources document.
    """
    check_active()
    if (not isinstance(parsed_jcs, bytes) or not 2 <= len(parsed_jcs) <= 8 << 20
            or not isinstance(sources, ImportSources) or not callable(read_document)
            or not isinstance(prepared_recipes, dict)):
        raise AdoptionInvalid('INTERCHANGE_PLAN_COMPILE_INPUT')
    sources = ImportSources.model_validate_json(canonical_bytes(sources))
    if sha256(parsed_jcs).hexdigest() != sources.parsed.sha256:
        raise AdoptionInvalid('INTERCHANGE_PLAN_PARSED_HASH')
    parsed = ParsedInterchange.model_validate_json(parsed_jcs)
    try:
        validate_parsed_inventory(parsed)
    except ValueError as error:
        raise AdoptionInvalid('INTERCHANGE_PLAN_INVENTORY') from error
    limits.validate()
    entries = parsed.manifest.entries
    if len(entries) != len(sources.entries):
        raise AdoptionInvalid('INTERCHANGE_PLAN_SOURCE_COUNT')
    files = {file.path: file for file in parsed.files}
    if len(files) != len(parsed.files):
        raise AdoptionInvalid('INTERCHANGE_PLAN_FILE_DUPLICATE')
    adopted = {s.entry_id for s in sources.entries if s.state == 'adopted'}
    if not set(prepared_recipes).issubset(adopted):
        raise AdoptionInvalid('INTERCHANGE_PLAN_FOREIGN_PROPOSAL')
    result = []
    for ordinal, (entry, source) in enumerate(zip(entries, sources.entries)):
        check_active()
        if source.ordinal != ordinal or source.entry_id != entry.entry_id:
            raise AdoptionInvalid('INTERCHANGE_PLAN_SOURCE_IDENTITY')
        file = files.get(entry.image_path)
        if file is None or file.content.sha256 != entry.image_sha256:
            raise AdoptionInvalid('INTERCHANGE_PLAN_ORIGINAL_FILE')
        binding, recipe = None, None
        if source.state == 'adopted':
            raw = read_document(source.master.model_copy(deep=True), 1 << 20)
            check_active()
            if not isinstance(raw, bytes) or not 2 <= len(raw) <= 1 << 20 or sha256(raw).hexdigest() != source.master.sha256:
                raise AdoptionInvalid('INTERCHANGE_PLAN_MASTER_HASH')
            image = WorkingImage.model_validate_json(raw)
            if (canonical_bytes(image) != raw or image.canvas.source_sha256 != entry.image_sha256
                    or image.dtype != 'float32' or image.color.profile_id != 'work_rec2020_gamma24_v1'):
                raise AdoptionInvalid('INTERCHANGE_PLAN_MASTER_BINDING')
            binding = VersionBinding(asset_version_id=source.asset_version_id,
                                     source=source.master, canvas_hash=canonical_hash(image.canvas))
            proposal = prepared_recipes.get(entry.entry_id)
            if proposal is not None:
                if (not isinstance(proposal, ImportedRecipeProposal) or entry.recipe_path is None
                        or proposal.artifact.artifact_id.int == 0
                        or sha256(proposal.body).hexdigest() != proposal.artifact.sha256
                        or canonical_bytes(proposal.recipe) != proposal.body
                        or proposal.recipe.source != binding):
                    raise AdoptionInvalid('INTERCHANGE_PLAN_PROPOSAL_BINDING')
                recipe = ArtifactRef.model_validate_json(canonical_bytes(proposal.artifact))
        result.append(InterchangeEntry(entry_id=entry.entry_id, external_id=entry.external_id,
            path=entry.image_path, content=file.content, binding=binding, recipe=recipe,
            rating=entry.rating, pick=entry.pick, crop=entry.crop, label=entry.label))
    plan = InterchangePlan(schema_version=1, project_id=sources.project_id, direction='import',
        format=sources.format, snapshot_digest=snapshot_digest, entries=result,
        max_archive_bytes=limits.archive_bytes, max_expanded_bytes=limits.expanded_bytes,
        max_files=limits.files)
    if len(canonical_bytes(plan)) > 8 << 20:
        raise AdoptionInvalid('INTERCHANGE_PLAN_SIZE')
    check_active()
    return plan
