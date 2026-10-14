from contextlib import contextmanager
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import zipfile

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.interchange_archive import ArchiveLimits
from photo_worker.v2.interchange_sidecar_parse import stage_sidecar_archive
from photo_worker.v2.interchange_xmp import XMPMapping, export_xmp, CRS


@pytest.mark.parametrize('mode', ['success', 'orphan', 'bad_xml', 'hash', 'zero_id', 'duplicate_id', 'cancel_upload'])
def test_real_zip_scratch_sidecar_staging_lifecycle(tmp_path, mode):
    path = tmp_path / 'sidecars.zip'
    image = b'untrusted encoded image; canonical ingest still required'
    xmp = export_xmp(XMPMapping(3, 'picked', 1, None, ()))
    xmp = xmp.replace(b'rdf:about=""', b'rdf:about="" crs:Exposure2012="2"')
    if mode == 'bad_xml':
        xmp = b'<invalid'
    with zipfile.ZipFile(path, 'w', compression=zipfile.ZIP_STORED) as archive:
        archive.writestr('images/a.tiff', image)
        archive.writestr('images/orphan.xmp' if mode == 'orphan' else 'images/a.xmp', xmp)
    bundle = ArtifactRef(artifact_id=uuid4(), sha256=sha256(path.read_bytes()).hexdigest())
    uploads = []
    cancelled = False
    fixed_id = uuid4()

    def active():
        if cancelled:
            raise RuntimeError('fixture lease lost')

    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 1 << 20, min_free_bytes=0) as scratch:
        class Artifacts:
            @contextmanager
            def input(self, candidate, *, suffix):
                assert candidate == bundle and suffix == '.zip'
                yield SimpleNamespace(path=path)

            def stage(self, local, reservation, role, media_type):
                nonlocal cancelled
                reservation.validate_file(local)
                assert role == 'imported_file' and media_type == 'application/octet-stream'
                raw = local.read_bytes()
                uploads.append(raw)
                if mode == 'cancel_upload':
                    cancelled = True
                identity = fixed_id if mode == 'duplicate_id' else uuid4()
                if mode == 'zero_id':
                    from uuid import UUID
                    identity = UUID(int=0)
                return ArtifactRef(artifact_id=identity,
                    sha256='f' * 64 if mode == 'hash' else sha256(raw).hexdigest())

        artifacts = Artifacts()
        artifacts.scratch = scratch
        call = lambda: stage_sidecar_archive(artifacts, bundle,
            ArchiveLimits(1 << 20, 1 << 20, 30), format='captureone_sidecar', active=active)
        if mode == 'success':
            result = call()
            assert uploads == [image, xmp]
            assert result.bytes_written == len(image) + len(xmp)
            assert len(result.outputs) == 2
            assert [item.content for item in result.document.files] == list(result.outputs)
            assert result.document.manifest.entries[0].rating == 3
            assert result.unsupported == (('images/a.tiff', 1, sha256(canonical_bytes([CRS + '}Exposure2012'])).hexdigest()),)
            assert all(output.artifact_id != record.artifact_id
                for output, record in zip(result.outputs, result.document.manifest.inventory))
        else:
            with pytest.raises(RuntimeError if mode == 'cancel_upload' else ValueError):
                call()
            assert len(uploads) == (0 if mode in ('orphan', 'bad_xml') else 2 if mode == 'duplicate_id' else 1)
