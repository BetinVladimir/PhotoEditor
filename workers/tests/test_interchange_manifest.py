from copy import deepcopy
from hashlib import sha256
from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.interchange_archive import VerifiedArchiveFile
from photo_worker.v2.interchange_manifest import verify_bundle_manifest


def fixture():
    image = b"encoded-image"
    digest = sha256(image).hexdigest()
    manifest = {
        "schema_version": 1, "renderer_version": "v2.2",
        "entries": [{"entry_id": str(uuid4()), "external_id": "photo-1",
                     "image_path": "image.jpg", "image_sha256": digest,
                     "recipe_path": None, "recipe_sha256": None,
                     "rating": 4, "pick": "picked", "crop": None}],
        "masks": [], "inventory": [{"artifact_id": str(uuid4()), "path": "image.jpg",
                                      "sha256": digest, "contract": "EncodedOriginal"}],
    }
    return manifest, VerifiedArchiveFile("image.jpg", len(image), digest)


def verify(manifest, image, extra=()):
    raw = canonical_bytes(manifest)
    metadata = VerifiedArchiveFile("manifest.json", len(raw), sha256(raw).hexdigest())
    return verify_bundle_manifest(raw, (metadata, image, *extra))


def test_exact_inventory_and_owned_manifest():
    manifest, image = fixture()
    result = verify(manifest, image)
    manifest["entries"][0]["rating"] = 0
    assert result.entries[0].rating == 4
    assert result.inventory[0].sha256 == image.sha256


@pytest.mark.parametrize("change", [
    lambda m: m["entries"][0].update(image_path="missing.jpg"),
    lambda m: m["entries"][0].update(image_sha256="0" * 64),
    lambda m: m["entries"][0].update(recipe_path="recipe.json"),
    lambda m: m["inventory"][0].update(sha256="0" * 64),
    lambda m: m["entries"].append(deepcopy(m["entries"][0])),
    lambda m: m["inventory"].append(deepcopy(m["inventory"][0])),
    lambda m: m["entries"][0].update(crop={"x0": .8, "y0": 0, "x1": .2, "y1": 1}),
    lambda m: m.update(extra=True),
])
def test_substitution_duplicates_and_invalid_recipe_pairs(change):
    manifest, image = fixture()
    change(manifest)
    with pytest.raises(ValueError):
        verify(manifest, image)


def test_unlisted_file_and_manifest_byte_substitution():
    manifest, image = fixture()
    with pytest.raises(ValueError, match="unlisted"):
        verify(manifest, image, (VerifiedArchiveFile("extra", 1, "a" * 64),))
    raw = canonical_bytes(manifest)
    with pytest.raises(ValueError, match="manifest physical"):
        verify_bundle_manifest(raw, (image, VerifiedArchiveFile("manifest.json", len(raw), "0" * 64)))


def test_duplicate_json_keys_rejected_before_model_validation():
    with pytest.raises(ValueError, match="Duplicate"):
        verify_bundle_manifest(b'{"schema_version":1,"schema_version":1}', ())
