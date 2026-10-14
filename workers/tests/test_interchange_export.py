from contextlib import contextmanager
from hashlib import sha256
from io import BytesIO
from uuid import uuid4
import zipfile

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.interchange_archive import ArchiveLimits, verify_archive
from photo_worker.v2.interchange_export import ExportFile, export_bundle
from photo_worker.v2.interchange_manifest import BundleManifest, verify_bundle_manifest


def fixture():
    content = b"encoded-image-content"
    digest = sha256(content).hexdigest()
    manifest = BundleManifest.model_validate_json(canonical_bytes({
        "schema_version": 1, "renderer_version": "v2.2", "masks": [],
        "entries": [{"entry_id": str(uuid4()), "external_id": "photo", "image_path": "photo.jpg",
                     "image_sha256": digest, "recipe_path": None, "recipe_sha256": None,
                     "rating": 0, "pick": "unmarked", "crop": None}],
        "inventory": [{"artifact_id": str(uuid4()), "path": "photo.jpg",
                       "sha256": digest, "contract": "EncodedOriginal"}],
    }))
    return content, manifest, (ExportFile("photo.jpg", len(content), digest, False),)


LIMITS = ArchiveLimits(1 << 20, 1 << 20, 30)


def opener(value):
    @contextmanager
    def source(_path):
        yield BytesIO(value)
    return source


def test_export_deterministic_metadata_and_import_verification():
    content, manifest, files = fixture()
    first, second = BytesIO(), BytesIO()
    size = export_bundle(first, manifest, files, opener(content), LIMITS)
    export_bundle(second, manifest, files, opener(content), LIMITS)
    assert first.getvalue() == second.getvalue() and size == len(first.getvalue())
    inventory = verify_archive(first, LIMITS)
    with zipfile.ZipFile(first) as archive:
        assert archive.namelist() == ["manifest.json", "photo.jpg"]
        assert archive.getinfo("photo.jpg").compress_type == zipfile.ZIP_STORED
        assert archive.getinfo("manifest.json").compress_type == zipfile.ZIP_DEFLATED
        for info in archive.infolist():
            assert info.date_time == (1980, 1, 1, 0, 0, 0) and info.create_system == 0
        verified = verify_bundle_manifest(archive.read("manifest.json"), inventory)
        assert verified == manifest


@pytest.mark.parametrize("value", [b"wrong", b"encoded-image-contenx", b"encoded-image-content-extra"])
def test_source_substitution_never_returns_success(value):
    _, manifest, files = fixture()
    with pytest.raises(ValueError, match="source"):
        export_bundle(BytesIO(), manifest, files, opener(value), LIMITS)


def test_archive_quota_and_cancellation():
    content, manifest, files = fixture()
    with pytest.raises(ValueError, match="quota"):
        export_bundle(BytesIO(), manifest, files, opener(content), ArchiveLimits(100, 1 << 20, 30))

    def cancelled():
        raise InterruptedError("cancelled")

    with pytest.raises(InterruptedError):
        export_bundle(BytesIO(), manifest, files, opener(content), LIMITS, cancelled)


def test_preflight_refuses_unlisted_source_before_reading():
    content, manifest, files = fixture()
    def forbidden(_path):
        pytest.fail("source opened before inventory validation")
    with pytest.raises(ValueError):
        export_bundle(BytesIO(), manifest, files + (ExportFile("extra", 1, "a" * 64, False),), forbidden, LIMITS)
