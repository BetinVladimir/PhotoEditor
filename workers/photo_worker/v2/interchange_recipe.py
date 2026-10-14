"""Typed authored recipe rebinding after authoritative canonical image ingest."""
from dataclasses import dataclass
from hashlib import sha256
from uuid import UUID, uuid5

from photo_worker.contracts.base import ArtifactRef, canonical_bytes, parse_json
from photo_worker.operations.session.contracts import VersionBinding
from .interchange_remap import UnsupportedDependency
from .render_recipe import EditingRecipeV3


@dataclass(frozen=True)
class ImportedRecipeDependency:
    original: ArtifactRef
    replacement: ArtifactRef
    contract: str
    binding: VersionBinding | None


@dataclass(frozen=True)
class ImportedRecipeProposal:
    artifact: ArtifactRef
    recipe: EditingRecipeV3
    body: bytes


def compile_imported_recipe(raw, original, *, tenant: UUID, project: UUID,
                            job: UUID, canonical_binding: VersionBinding,
                            resolve_dependency, check_active=lambda: None):
    """Resolver must qualify/remap each dependency and its source binding.

    No masks/layers/overrides are silently dropped. Foreign learning is blocked;
    resolved scalar values already in parameter_delta are preserved verbatim.
    This returns a proposal, never an approval or a write to the current recipe.
    """
    check_active()
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 8 << 20
            or not isinstance(original, ArtifactRef) or original.artifact_id.int == 0
            or sha256(raw).hexdigest() != original.sha256
            or any(value.int == 0 for value in (tenant, project, job))
            or not isinstance(canonical_binding, VersionBinding)
            or canonical_binding.asset_version_id.int == 0 or canonical_binding.source.artifact_id.int == 0
            or not callable(resolve_dependency)):
        raise UnsupportedDependency("INTERCHANGE_RECIPE_INPUT")
    # Duplicate keys and unknown fields are rejected before remapping.
    value = parse_json(raw, max_bytes=8 << 20)
    recipe = EditingRecipeV3.model_validate_json(canonical_bytes(value))
    if canonical_bytes(recipe) != canonical_bytes(value):
        raise UnsupportedDependency("INTERCHANGE_RECIPE_FIELDS")
    if recipe.learning_version_id is not None:
        raise UnsupportedDependency("INTERCHANGE_FOREIGN_LEARNING")
    owned_binding = VersionBinding.model_validate_json(canonical_bytes(canonical_binding))
    seen = {}

    def resolve(reference, contract, source_bound):
        if reference is None:
            return None
        check_active()
        dependency = resolve_dependency(reference.model_copy(deep=True), contract, owned_binding.model_copy(deep=True))
        if (not isinstance(dependency, ImportedRecipeDependency) or dependency.original != reference
                or dependency.contract != contract or not isinstance(dependency.replacement, ArtifactRef)
                or dependency.replacement.artifact_id.int == 0
                or dependency.replacement.artifact_id == reference.artifact_id
                or source_bound and dependency.binding != owned_binding
                or not source_bound and dependency.binding is not None):
            raise UnsupportedDependency("INTERCHANGE_RECIPE_DEPENDENCY")
        key = reference.artifact_id
        evidence = (reference.sha256, contract, dependency.replacement.artifact_id, dependency.replacement.sha256)
        if key in seen and seen[key] != evidence:
            raise UnsupportedDependency("INTERCHANGE_RECIPE_DEPENDENCY_CONFLICT")
        seen[key] = evidence
        return dependency.replacement.model_copy(deep=True)

    proposed = EditingRecipeV3(
        schema_version=3, source=owned_binding,
        base_style=resolve(recipe.base_style, "StyleV2", False),
        session_corrections=resolve(recipe.session_corrections, "CorrectionPlan", True),
        learning_version_id=None,
        overrides=[resolve(reference, "Override", True) for reference in recipe.overrides],
        layer_stack=resolve(recipe.layer_stack, "LayerStack", True),
        protected_mask=resolve(recipe.protected_mask, "MaskRef", True),
        renderer_version=recipe.renderer_version,
        parameter_delta=recipe.parameter_delta.model_copy(deep=True),
    )
    body = canonical_bytes(proposed)
    namespace = uuid5(tenant, "interchange:" + str(project) + ":" + str(job))
    identifier = uuid5(namespace, "document:" + str(original.artifact_id))
    if identifier in {owned_binding.source.artifact_id, *(item[2] for item in seen.values())}:
        raise UnsupportedDependency("INTERCHANGE_RECIPE_OUTPUT_ALIAS")
    check_active()
    return ImportedRecipeProposal(ArtifactRef(artifact_id=identifier, sha256=sha256(body).hexdigest()), proposed, body)
