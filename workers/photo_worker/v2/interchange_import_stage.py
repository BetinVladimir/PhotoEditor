"""Complete per-entry proposal preparation before mapping report publication."""
from hashlib import sha256
from uuid import UUID
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from .adoption import AdoptionInvalid
from .interchange_import_sources import ImportSources
from .interchange_mapping import MappingEntry, MappingField, publish_mapping_report
from .interchange_parse import ParsedInterchange, validate_parsed_inventory
from .interchange_recipe import ImportedRecipeProposal, compile_imported_recipe
from .interchange_remap import UnsupportedDependency


def publish_import_proposal(proposal, publish_proposal):
    """Expose a detached proposal while retaining the compiled output identity."""
    expected_ref = proposal.artifact.model_copy(deep=True)
    published = ImportedRecipeProposal(
        expected_ref.model_copy(deep=True), proposal.recipe.model_copy(deep=True), bytes(proposal.body))
    result = publish_proposal(published)
    if not isinstance(result, ArtifactRef) or result != expected_ref:
        raise AdoptionInvalid('INTERCHANGE_IMPORT_PROPOSAL_PUBLICATION')
    return expected_ref


def prepare_import_stage(*, tenant_id, parsed_jcs, sources, read_document,
                         resolve_dependency, publish_proposal,
                         publish_document, metadata_receipts=None, qualify_binding=None, check_active=lambda: None):
    """Ports must enforce declared inputs and attempt-owned output publication.

    Only UnsupportedDependency is converted into a terminal mapping field.
    Storage, cancellation and publication failures propagate to task retry.
    No document becomes approved here, and no existing asset is overwritten.
    """
    check_active()
    if (not isinstance(tenant_id, UUID) or tenant_id.int == 0 or not isinstance(sources, ImportSources)
            or not isinstance(parsed_jcs, bytes) or sha256(parsed_jcs).hexdigest() != sources.parsed.sha256
            or not all(callable(port) for port in (read_document, resolve_dependency,
                                                   publish_proposal, publish_document))):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_STAGE_INPUT')
    # Own all mutable caller models before any external IO.
    sources = ImportSources.model_validate_json(canonical_bytes(sources))
    parsed = ParsedInterchange.model_validate_json(parsed_jcs)
    try:
        validate_parsed_inventory(parsed)
    except ValueError as error:
        raise AdoptionInvalid('INTERCHANGE_IMPORT_PARSED_INVENTORY') from error
    if len(sources.entries) != len(parsed.manifest.entries):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_STAGE_CARDINALITY')
    files = {file.path:file for file in parsed.files}
    if len(files) != len(parsed.files):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_STAGE_DUPLICATE_FILE')
    expected = [(entry.entry_id, entry.external_id) for entry in parsed.manifest.entries]
    for ordinal,(entry,source) in enumerate(zip(parsed.manifest.entries,sources.entries)):
        if source.ordinal != ordinal or source.entry_id != entry.entry_id:
            raise AdoptionInvalid('INTERCHANGE_IMPORT_STAGE_IDENTITY')
    map_metadata = None
    if any(source.state == 'adopted' for source in sources.entries):
        if metadata_receipts is None:
            raise AdoptionInvalid('INTERCHANGE_METADATA_RECEIPTS_REQUIRED')
        from .interchange_metadata import qualified_metadata_mapper
        map_metadata = qualified_metadata_mapper(metadata_receipts, sources)
    mappings = []
    for entry,source in zip(parsed.manifest.entries,sources.entries):
        check_active()
        fields, proposal_ref = [], None
        if source.state != 'adopted':
            fields.append(MappingField(name='image',status='rejected' if source.state=='rejected' else 'unsupported',reason=source.reason))
        else:
            raw = read_document(source.master.model_copy(deep=True), 1 << 20)
            if not isinstance(raw,bytes) or not 2<=len(raw)<=1<<20 or sha256(raw).hexdigest()!=source.master.sha256:
                raise AdoptionInvalid('INTERCHANGE_IMPORT_MASTER_HASH')
            image = WorkingImage.model_validate_json(raw)
            if (canonical_bytes(image)!=raw or image.canvas.source_sha256!=entry.image_sha256
                    or image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1'):
                raise AdoptionInvalid('INTERCHANGE_IMPORT_MASTER_BINDING')
            binding = VersionBinding(asset_version_id=source.asset_version_id,source=source.master,
                                     canvas_hash=canonical_hash(image.canvas))
            fields.append(MappingField(name='image',status='exact',reason=None))
            metadata_fields = map_metadata(entry.model_copy(deep=True), binding.model_copy(deep=True))
            fields.extend(MappingField.model_validate_json(canonical_bytes(field)) for field in metadata_fields)
            if entry.recipe_path is None:
                fields.append(MappingField(name='recipe',status='unsupported',reason='recipe_not_present'))
            else:
                file = files.get(entry.recipe_path)
                if file is None or file.content.sha256!=entry.recipe_sha256:
                    raise AdoptionInvalid('INTERCHANGE_IMPORT_RECIPE_SOURCE')
                raw = read_document(file.content.model_copy(deep=True), 8 << 20)
                if not isinstance(raw,bytes) or not 2<=len(raw)<=8<<20 or sha256(raw).hexdigest()!=file.content.sha256:
                    raise AdoptionInvalid('INTERCHANGE_IMPORT_RECIPE_HASH')
                if not callable(qualify_binding):
                    raise AdoptionInvalid('INTERCHANGE_IMPORT_BINDING_QUALIFIER_REQUIRED')
                try:
                    from pydantic import ValidationError
                    from .render_recipe import EditingRecipeV3
                    try:
                        original_recipe = EditingRecipeV3.model_validate_json(raw)
                    except ValidationError as error:
                        raise UnsupportedDependency('INTERCHANGE_RECIPE_TYPED_DOCUMENT_INVALID') from error
                    if qualify_binding(original_recipe.source.model_copy(deep=True)) != binding:
                        raise UnsupportedDependency('INTERCHANGE_RECIPE_CANONICAL_BINDING_MISMATCH')
                    proposal = compile_imported_recipe(raw,file.content,tenant=tenant_id,
                        project=sources.project_id,job=sources.workflow_id,canonical_binding=binding,
                        resolve_dependency=resolve_dependency,check_active=check_active)
                except UnsupportedDependency as error:
                    fields.append(MappingField(name='recipe',status='unsupported',reason=str(error)[:200] or 'unsupported_dependency'))
                else:
                    check_active()
                    proposal_ref = publish_import_proposal(proposal, publish_proposal)
                    fields.append(MappingField(name='recipe',status='exact',reason=None))
        from .interchange_sidecar_mapping import sidecar_mapping_fields
        fields.extend(sidecar_mapping_fields(entry, files, read_document,
            applied_label=source.state == 'adopted', check_active=check_active))
        mappings.append(MappingEntry(entry_id=entry.entry_id,external_id=entry.external_id,
            asset_version_id=source.asset_version_id,proposed_document=proposal_ref,fields=fields))
    check_active()
    return publish_mapping_report(mappings,expected,publish_document,check_active=check_active)


def prepare_import_output(*, publish_root, **stage_arguments):
    """Finish proposals/report first, then publish the exact final import root."""
    from .interchange_import_result import publish_import_result
    parsed=ParsedInterchange.model_validate_json(stage_arguments['parsed_jcs'])
    mapping=prepare_import_stage(**stage_arguments)
    return publish_import_result(parsed,mapping,publish_root,
        check_active=stage_arguments.get('check_active',lambda:None))
