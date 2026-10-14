from hashlib import sha256
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.v2.interchange_archive import VerifiedArchiveFile
from photo_worker.v2.interchange_sidecar_import import normalize_sidecar_inventory
from photo_worker.v2.interchange_xmp import XMPMapping, export_xmp, CRS


def fixture(image_path='images/a.tiff', *, unsupported=False):
    mapping = XMPMapping(4, 'picked', 6, (0.1, 0.2, 0.7, 0.8), ())
    raw = export_xmp(mapping)
    if unsupported:
        raw = raw.replace(b'rdf:about=""', b'rdf:about="" crs:Exposure2012="1.5"')
    sidecar_path = image_path.rsplit('.', 1)[0] + '.xmp'
    files = (VerifiedArchiveFile(image_path, 8, sha256(b'image123').hexdigest()),
             VerifiedArchiveFile(sidecar_path, len(raw), sha256(raw).hexdigest()))
    bundle = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
    return bundle, files, {sidecar_path: raw}


def test_normalization_orientation_mapping_inventory_and_determinism():
    bundle, files, sidecars = fixture()
    original = dict(sidecars)
    first = normalize_sidecar_inventory(bundle, files, sidecars, format='captureone_sidecar')
    second = normalize_sidecar_inventory(bundle, tuple(reversed(files)), sidecars, format='captureone_sidecar')
    assert canonical_bytes(first.manifest) == canonical_bytes(second.manifest)
    entry = first.manifest.entries[0]
    assert entry.rating == 4 and entry.pick == 'picked'
    assert [entry.crop.x0, entry.crop.y0, entry.crop.x1, entry.crop.y1] == pytest.approx([0.1, 0.2, 0.7, 0.8])
    assert entry.recipe_path is None and entry.recipe_sha256 is None
    assert first.unsupported == ()
    assert {item.path for item in first.manifest.inventory} == {item.path for item in files}
    assert len({item.artifact_id for item in first.manifest.inventory}) == 2
    assert sidecars == original
    changed = bundle.model_copy(update={'sha256': 'b' * 64})
    third = normalize_sidecar_inventory(changed, files, sidecars, format='captureone_sidecar')
    assert third.manifest.entries[0].entry_id != entry.entry_id


def test_unknown_development_setting_is_retained_for_unsupported_report():
    bundle, files, sidecars = fixture(unsupported=True)
    result = normalize_sidecar_inventory(bundle, files, sidecars, format='xmp')
    assert result.unsupported == (('images/a.tiff', 1, sha256(canonical_bytes([CRS + '}Exposure2012'])).hexdigest()),)


def test_standard_label_is_preserved_in_portable_manifest():
    bundle, files, sidecars = fixture()
    path = next(iter(sidecars))
    raw = sidecars[path].replace(b'rdf:about=""', b'rdf:about="" xmp:Label="Blue"')
    files = (files[0], VerifiedArchiveFile(path, len(raw), sha256(raw).hexdigest()))
    result = normalize_sidecar_inventory(bundle, files, {path: raw}, format='xmp')
    assert result.manifest.entries[0].label == 'Blue'
    assert result.unsupported == ()


@pytest.mark.parametrize('kind', ['hash', 'size', 'missing', 'extra', 'duplicate', 'cos', 'capture_jpeg', 'shared_stem', 'uppercase_xmp'])
def test_bad_sidecar_inventory_refuses_normalization(kind):
    bundle, files, sidecars = fixture('images/a.jpg' if kind == 'capture_jpeg' else 'images/a.tiff')
    image, sidecar = files
    if kind == 'hash': files = (image, VerifiedArchiveFile(sidecar.path, sidecar.bytes, 'f' * 64))
    elif kind == 'size': files = (image, VerifiedArchiveFile(sidecar.path, sidecar.bytes + 1, sidecar.sha256))
    elif kind == 'missing': sidecars = {}
    elif kind == 'extra': sidecars['extra.xmp'] = b'unknown'
    elif kind == 'duplicate': files = (*files, image)
    elif kind == 'cos': files = (*files, VerifiedArchiveFile('photo.cos', 1, 'b' * 64))
    elif kind == 'shared_stem': files = (*files, VerifiedArchiveFile('images/a.jpg', 8, image.sha256))
    elif kind == 'uppercase_xmp':
        upper = sidecar.path[:-4] + '.XMP'
        files = (image, VerifiedArchiveFile(upper, sidecar.bytes, sidecar.sha256))
        sidecars = {upper: next(iter(sidecars.values()))}
    with pytest.raises(ValueError):
        normalize_sidecar_inventory(bundle, files, sidecars, format='captureone_sidecar' if kind == 'capture_jpeg' else 'xmp')


def test_cancelled_normalization_returns_no_manifest():
    bundle, files, sidecars = fixture()
    calls = 0

    def active():
        nonlocal calls
        calls += 1
        if calls == 4:
            raise RuntimeError('lease lost')

    with pytest.raises(RuntimeError, match='lease lost'):
        normalize_sidecar_inventory(bundle, files, sidecars, format='xmp', active=active)


def test_many_unknown_properties_are_compact_summaries_without_losing_source_pin():
    bundle, files, sidecars = fixture()
    path = next(iter(sidecars))
    properties = ' '.join(f'crs:Unknown{index}="value"' for index in range(1000))
    raw = sidecars[path].replace(b'rdf:about=""', ('rdf:about="" ' + properties).encode())
    image = files[0]
    files = (image, VerifiedArchiveFile(path, len(raw), sha256(raw).hexdigest()))
    result = normalize_sidecar_inventory(bundle, files, {path:raw}, format='xmp')
    summary = result.unsupported[0]
    expected = sorted(CRS + '}Unknown' + str(index) for index in range(1000))
    assert summary == (image.path, 1000, sha256(canonical_bytes(expected)).hexdigest())
    assert len(canonical_bytes(list(summary))) < 512
    sidecar = next(item for item in result.manifest.inventory if item.contract=='XMP')
    assert sidecar.sha256 == sha256(raw).hexdigest()
