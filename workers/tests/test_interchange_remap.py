from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.interchange_parse import ParsedInterchange
from photo_worker.v2.interchange_remap import BundleRemapper, UnsupportedDependency


def fixture(extra=None):
    image_id, document_id, entity = uuid4(), uuid4(), uuid4()
    image = b"encoded-source"
    image_sha = sha256(image).hexdigest()
    document = {"schema_version": 1, "entity_id": str(entity), "input": {"artifact_id": str(image_id), "sha256": image_sha}, "gain": 1.25}
    document.update(extra or {})
    raw = canonical_bytes(document)
    doc_sha = sha256(raw).hexdigest()
    inventory = [{"artifact_id": str(image_id), "path": "photo.jpg", "sha256": image_sha, "contract": "EncodedOriginal"},
                 {"artifact_id": str(document_id), "path": "recipe.json", "sha256": doc_sha, "contract": "TestRecipe"}]
    parsed = ParsedInterchange.model_validate_json(canonical_bytes({"schema_version": 1,
        "manifest": {"schema_version": 1, "renderer_version": "v2.2", "masks": [], "inventory": inventory,
        "entries": [{"entry_id": str(uuid4()), "external_id": "photo", "image_path": "photo.jpg", "image_sha256": image_sha,
                     "recipe_path": "recipe.json", "recipe_sha256": doc_sha, "rating": 0, "pick": "unmarked", "crop": None}]},
        "files": [{"path": item["path"], "contract": item["contract"], "content": {"artifact_id": str(uuid4()), "sha256": item["sha256"]}} for item in inventory]}))
    scope = (uuid4(), uuid4(), uuid4())
    return parsed, document_id, entity, raw, scope


def test_remap_rehashes_parent_and_binds_binary_to_gateway_identity():
    parsed, root, entity, raw, scope = fixture()
    canonical_entity = uuid4()
    remapper = BundleRemapper(parsed, *scope, lambda ref, limit: raw,
        {"TestRecipe": lambda value: value}, {"EncodedOriginal"}, canonical_ids={entity: canonical_entity})
    result = remapper.remap(root)
    document = remapper.documents[root]
    assert result["artifact_id"] != str(root)
    assert result["sha256"] == sha256(document.body).hexdigest()
    assert result["sha256"] != sha256(raw).hexdigest()
    assert str(canonical_entity).encode() in document.body
    assert str(parsed.files[0].content.artifact_id).encode() in document.body
    result["sha256"] = "0" * 64
    assert remapper.remap(root)["sha256"] == document.sha256
    other = BundleRemapper(parsed, scope[0], uuid4(), scope[2], lambda ref, limit: raw,
        {"TestRecipe": lambda value: value}, {"EncodedOriginal"})
    assert other.remap(root)["artifact_id"] != str(document.artifact_id)


@pytest.mark.parametrize("extra", [{"learning_version_id": str(uuid4())}, {"approval": True}, {"tenant_id": str(uuid4())}])
def test_foreign_authority_blocks_document(extra):
    parsed, root, _, raw, scope = fixture(extra)
    remapper = BundleRemapper(parsed, *scope, lambda ref, limit: raw,
        {"TestRecipe": lambda value: value}, {"EncodedOriginal"})
    with pytest.raises(UnsupportedDependency):
        remapper.remap(root)
    assert root not in remapper.documents


def test_unknown_contract_hash_and_budget_fail_closed():
    parsed, root, _, raw, scope = fixture()
    with pytest.raises(UnsupportedDependency):
        BundleRemapper(parsed, *scope, lambda ref, limit: raw, {}, {"EncodedOriginal"}).remap(root)
    with pytest.raises(UnsupportedDependency):
        BundleRemapper(parsed, *scope, lambda ref, limit: b"substituted", {"TestRecipe": lambda value: value}, {"EncodedOriginal"}).remap(root)
    with pytest.raises(UnsupportedDependency):
        BundleRemapper(parsed, *scope, lambda ref, limit: raw, {"TestRecipe": lambda value: value}, {"EncodedOriginal"}, max_json_bytes=1).remap(root)
    with pytest.raises(ValueError):
        BundleRemapper(parsed, *scope, lambda ref, limit: raw, {"SigningPolicy": lambda value: value}, set())
