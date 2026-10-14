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


@pytest.mark.parametrize('change', ['valid', 'receipt', 'entry', 'recipe', 'override',
                                    'mask_ref', 'pixels', 'duplicate'])
def test_person_origin_mapping_binds_exact_inventory_and_recipe_occurrence(change):
    manifest, image = fixture()
    def add(contract):
        raw = contract.encode()
        path = f'dependencies/{uuid4()}.json'
        digest = sha256(raw).hexdigest()
        row = dict(artifact_id=str(uuid4()), path=path, sha256=digest, contract=contract)
        manifest['inventory'].append(row)
        return row, VerifiedArchiveFile(path, len(raw), digest)
    recipe, recipe_file = add('EditingRecipeV3')
    override, override_file = add('Override')
    mask, mask_file = add('MaskRef')
    pixels, pixels_file = add('MaskPixels')
    receipt, receipt_file = add('PersonOriginReceipt')
    entry = manifest['entries'][0]
    entry['recipe_path'], entry['recipe_sha256'] = recipe['path'], recipe['sha256']
    def ref(row):
        return dict(artifact_id=row['artifact_id'], sha256=row['sha256'])
    origin = dict(entry_id=entry['entry_id'], recipe=ref(recipe), override=ref(override),
                  mask_ref=ref(mask), mask_pixels=ref(pixels),
                  path=receipt['path'], sha256=receipt['sha256'])
    manifest['person_origins'] = [origin]
    if change == 'receipt': origin['path'] = override['path']
    if change == 'entry': origin['entry_id'] = str(uuid4())
    if change == 'recipe': origin['recipe']['sha256'] = 'f' * 64
    if change == 'override': origin['override']['artifact_id'] = str(uuid4())
    if change == 'mask_ref': origin['mask_ref']['sha256'] = 'f' * 64
    if change == 'pixels': origin['mask_pixels']['artifact_id'] = str(uuid4())
    if change == 'duplicate': manifest['person_origins'].append(deepcopy(origin))
    files = (recipe_file, override_file, mask_file, pixels_file, receipt_file)
    if change == 'valid':
        assert verify(manifest, image, files).person_origins[0].path == receipt['path']
    else:
        with pytest.raises(ValueError):
            verify(manifest, image, files)
