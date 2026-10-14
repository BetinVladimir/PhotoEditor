"""Derive portable export files exclusively from qualified recipe receipts."""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from .interchange_export_prepare import ExportSource
from .interchange_plan import InterchangePlan
from .render_recipe import EditingRecipeV3


@dataclass(frozen=True)
class QualifiedExportInventory:
    sources: tuple[ExportSource, ...]
    # Immutable canonical refs grouped by entry ID. Copies are decoded on access.
    closures: tuple[tuple[str, tuple[bytes, ...]], ...]

    def recipe_closure(self, entry):
        for identifier, references in self.closures:
            if identifier == str(entry.entry_id):
                refs = tuple(ArtifactRef.model_validate_json(raw) for raw in references)
                if entry.recipe not in refs:
                    raise ValueError('INTERCHANGE_EXPORT_INVENTORY_RECIPE_CHANGED')
                return refs
        raise ValueError('INTERCHANGE_EXPORT_INVENTORY_ENTRY')


def compile_qualified_export_inventory(plan, reader, qualifier, *, active=lambda: None):
    active()
    plan = InterchangePlan.model_validate_json(canonical_bytes(plan))
    if (plan.direction != 'export' or plan.format != 'photoeditor_bundle'
            or not callable(qualifier)
            or not callable(getattr(qualifier, 'qualified_document_refs', None))):
        raise ValueError('INTERCHANGE_EXPORT_INVENTORY_CONFIGURATION')
    sources, closures, paths = {}, [], set()

    def add(reference, contract, path):
        reference = ArtifactRef.model_validate_json(canonical_bytes(reference))
        declared = reader.entries.get((reference.artifact_id, reference.sha256))
        if declared is None or declared.contract != contract:
            raise ValueError('INTERCHANGE_EXPORT_INVENTORY_CONTRACT')
        evidence = reader.evidence(reference.model_copy(deep=True))
        is_json = evidence.media_type == 'application/json'
        if (type(evidence.bytes) is not int or not 1 <= evidence.bytes <= plan.max_expanded_bytes
                or contract == 'EditingRecipeV3' and not is_json):
            raise ValueError('INTERCHANGE_EXPORT_INVENTORY_EVIDENCE')
        item = ExportSource(reference, path, contract, evidence.bytes, is_json)
        old = sources.get(reference.artifact_id)
        if old is not None:
            if old != item:
                raise ValueError('INTERCHANGE_EXPORT_INVENTORY_ALIAS')
            return
        if path.casefold() in paths or len(sources) >= min(plan.max_files - 1, 29999):
            raise ValueError('INTERCHANGE_EXPORT_INVENTORY_LIMIT')
        paths.add(path.casefold());sources[reference.artifact_id] = item

    for entry in sorted(plan.entries, key=lambda value: value.path):
        active()
        if entry.binding is None or entry.recipe is None:
            raise ValueError('INTERCHANGE_EXPORT_INVENTORY_BINDING')
        raw = reader.read_json(entry.recipe, min(8 << 20, reader.read_budget.remaining_json_bytes))
        recipe = EditingRecipeV3.model_validate_json(raw)
        if canonical_bytes(recipe) != raw or sha256(raw).hexdigest() != entry.recipe.sha256:
            raise ValueError('INTERCHANGE_EXPORT_INVENTORY_RECIPE_HASH')
        if qualifier(entry.recipe.model_copy(deep=True), recipe, entry.binding.model_copy(deep=True)) is not True:
            raise ValueError('INTERCHANGE_EXPORT_INVENTORY_QUALIFICATION')
        receipts = qualifier.qualified_document_refs(entry.recipe.model_copy(deep=True), entry.binding.model_copy(deep=True))
        if not isinstance(receipts, tuple) or len(receipts) > 29999:
            raise ValueError('INTERCHANGE_EXPORT_INVENTORY_RECEIPT')
        refs, seen = [entry.recipe.model_copy(deep=True)], {entry.recipe.artifact_id}
        add(entry.content, 'EncodedImage', entry.path)
        add(entry.recipe, 'EditingRecipeV3', f'recipes/{entry.recipe.artifact_id}.json')
        for receipt in receipts:
            reference = ArtifactRef(artifact_id=receipt['artifact_id'], sha256=receipt['sha256'])
            if reference.artifact_id in seen:
                raise ValueError('INTERCHANGE_EXPORT_INVENTORY_RECEIPT')
            seen.add(reference.artifact_id)
            declared = reader.entries.get((reference.artifact_id, reference.sha256))
            if declared is None:
                raise ValueError('INTERCHANGE_EXPORT_INVENTORY_UNDECLARED')
            evidence = reader.evidence(reference.model_copy(deep=True))
            suffix = '.json' if evidence.media_type == 'application/json' else '.bin'
            path = (f'recipes/{reference.artifact_id}.json' if declared.contract == 'EditingRecipeV3'
                    else f'dependencies/{reference.artifact_id}{suffix}')
            add(reference, declared.contract, path);refs.append(reference)
        closures.append((str(entry.entry_id), tuple(canonical_bytes(ref) for ref in refs)))
        active()
    reader.recheck();active()
    return QualifiedExportInventory(tuple(sorted(sources.values(), key=lambda item: item.path)), tuple(closures))
