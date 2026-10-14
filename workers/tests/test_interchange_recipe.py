from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.operations.session.contracts import VersionBinding, CorrectionVector
from photo_worker.v2.render_recipe import EditingRecipeV3
from photo_worker.v2.interchange_recipe import ImportedRecipeDependency, compile_imported_recipe
from photo_worker.v2.interchange_remap import UnsupportedDependency


def ref():
    return ArtifactRef(artifact_id=uuid4(), sha256="a" * 64)


def binding():
    return VersionBinding(asset_version_id=uuid4(), source=ref(), canvas_hash="b" * 64)


def fixture():
    recipe = EditingRecipeV3(schema_version=3, source=binding(), base_style=ref(),
        session_corrections=ref(), learning_version_id=None, overrides=[ref()], layer_stack=ref(),
        protected_mask=ref(), renderer_version="v2.2",
        parameter_delta=CorrectionVector(ev=0.25, log_r=0.01, log_g=0.0, log_b=-0.01,
                                         saturation_delta=0.05, retouch_delta=0.0))
    raw = canonical_bytes(recipe)
    original = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    return recipe, raw, original, binding(), (uuid4(), uuid4(), uuid4())


def test_recipe_preserves_authored_values_and_requires_every_dependency():
    recipe, raw, original, target, scope = fixture()
    calls = []
    def resolve(reference, contract, canonical):
        calls.append(contract)
        return ImportedRecipeDependency(reference, ref(), contract, None if contract == "StyleV2" else canonical)
    proposal = compile_imported_recipe(raw, original, tenant=scope[0], project=scope[1], job=scope[2], canonical_binding=target, resolve_dependency=resolve)
    assert calls == ["StyleV2", "CorrectionPlan", "Override", "LayerStack", "MaskRef"]
    assert proposal.recipe.source == target
    assert proposal.recipe.parameter_delta == recipe.parameter_delta
    assert proposal.recipe.layer_stack is not None and proposal.recipe.protected_mask is not None
    assert proposal.artifact.sha256 == sha256(proposal.body).hexdigest()
    assert proposal.artifact.artifact_id != original.artifact_id
    assert recipe.source != target  # original owned recipe remains unchanged


@pytest.mark.parametrize("mode", ["foreign-binding", "wrong-type", "same-id", "missing-layer"])
def test_recipe_dependency_substitution_blocks_proposal(mode):
    _, raw, original, target, scope = fixture()
    def resolve(reference, contract, canonical):
        if mode == "missing-layer" and contract == "LayerStack":
            raise UnsupportedDependency("unsupported layer operation")
        return ImportedRecipeDependency(reference, reference if mode == "same-id" else ref(),
            "unexpected" if mode == "wrong-type" else contract,
            None if contract == "StyleV2" else binding() if mode == "foreign-binding" else canonical)
    with pytest.raises(UnsupportedDependency):
        compile_imported_recipe(raw, original, tenant=scope[0], project=scope[1], job=scope[2], canonical_binding=target, resolve_dependency=resolve)


def test_recipe_foreign_learning_is_not_implicitly_imported():
    recipe, _, original, target, scope = fixture()
    recipe.learning_version_id = uuid4()
    raw = canonical_bytes(recipe)
    original.sha256 = sha256(raw).hexdigest()
    def resolver(*args):
        raise AssertionError("foreign learning must fail before dependency effects")
    with pytest.raises(UnsupportedDependency, match="FOREIGN_LEARNING"):
        compile_imported_recipe(raw, original, tenant=scope[0], project=scope[1], job=scope[2], canonical_binding=target, resolve_dependency=resolver)
