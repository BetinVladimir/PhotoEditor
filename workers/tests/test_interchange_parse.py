from contextlib import contextmanager
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import zipfile

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.interchange_archive import ArchiveLimits
from photo_worker.v2.interchange_parse import stage_photoeditor_bundle


def make_archive(path, *, wrong=False):
    image = b"image-bytes"
    digest = sha256(image).hexdigest()
    manifest = {"schema_version": 1, "renderer_version": "v2.2", "masks": [],
        "entries": [{"entry_id": str(uuid4()), "external_id": "photo", "image_path": "photo.jpg",
                     "image_sha256": digest, "recipe_path": None, "recipe_sha256": None,
                     "rating": 0, "pick": "unmarked", "crop": None}],
        "inventory": [{"artifact_id": str(uuid4()), "path": "photo.jpg",
                       "sha256": "0" * 64 if wrong else digest, "contract": "EncodedOriginal"}]}
    with zipfile.ZipFile(path, "w") as archive:
        archive.writestr("photo.jpg", image)
        archive.writestr("manifest.json", canonical_bytes(manifest))
    return image


class Artifacts:
    def __init__(self, scratch, path, corrupt=False):
        self.scratch, self.path, self.corrupt, self.uploads = scratch, path, corrupt, []

    @contextmanager
    def input(self, ref, *, suffix):
        assert suffix == ".zip"
        yield SimpleNamespace(path=self.path)

    def stage(self, path, reservation, role, media_type):
        reservation.validate_file(path)
        assert role == "imported_file" and media_type == "application/octet-stream"
        raw = path.read_bytes()
        self.uploads.append(raw)
        return ArtifactRef(artifact_id=uuid4(), sha256="0" * 64 if self.corrupt else sha256(raw).hexdigest())


@pytest.mark.parametrize("wrong,corrupt", [(False, False), (True, False), (False, True)])
def test_bundle_staging_hash_gate_and_owned_output(tmp_path, wrong, corrupt):
    path = tmp_path / "bundle.zip"
    image = make_archive(path, wrong=wrong)
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 1 << 20, min_free_bytes=0) as scratch:
        artifacts = Artifacts(scratch, path, corrupt)
        ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(path.read_bytes()).hexdigest())
        call = lambda: stage_photoeditor_bundle(artifacts, ref, ArchiveLimits(1 << 20, 1 << 20, 30), lambda: None)
        if wrong or corrupt:
            with pytest.raises(ValueError):
                call()
            assert len(artifacts.uploads) == (0 if wrong else 1)
        else:
            result = call()
            assert artifacts.uploads == [image]
            assert result.bytes_written == len(image)
            assert result.document.files[0].content == result.outputs[0]
            assert str(result.outputs[0].artifact_id) != str(result.document.manifest.inventory[0].artifact_id)
