"""Normalize verified image/XMP pairs without granting canonical ingest authority."""
from dataclasses import dataclass
from collections.abc import Mapping
from hashlib import sha256
from pathlib import PurePosixPath
from uuid import uuid5

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.images import NormalizedRect
from .interchange_archive import VerifiedArchiveFile, interchange_path
from .interchange_manifest import BundleEntry, BundleInventoryEntry, BundleManifest, verify_bundle_manifest
from .interchange_xmp import parse_xmp


@dataclass(frozen=True)
class NormalizedSidecars:
    manifest: BundleManifest
    # Compact summaries: image path, property count, canonical full-list SHA.
    # Original properties remain in pinned XMP; never retain all XML property
    # lists for an entire 5000-image batch in process memory.
    unsupported: tuple[tuple[str, int, str], ...]


def normalize_sidecar_inventory(bundle: ArtifactRef, files: tuple[VerifiedArchiveFile, ...],
                                sidecars: Mapping[str, bytes], *, format: str,
                                active=lambda: None, manifest_raw: bytes | None = None) -> NormalizedSidecars:
    active()
    bundle = bundle.model_copy(deep=True)
    if bundle.artifact_id.int == 0 or format not in ('xmp', 'captureone_sidecar'):
        raise ValueError('INTERCHANGE_SIDECAR_FORMAT')
    if not 2 <= len(files) <= 10001:
        raise ValueError('INTERCHANGE_SIDECAR_INVENTORY')
    files = tuple(files)
    supplied_manifest = None
    if manifest_raw is not None:
        if not isinstance(manifest_raw, bytes) or not 2 <= len(manifest_raw) <= 8 << 20:
            raise ValueError('INTERCHANGE_SIDECAR_MANIFEST_BUDGET')
        supplied_manifest = verify_bundle_manifest(manifest_raw, files)
        files = tuple(item for item in files if item.path != 'manifest.json')
    sidecar_paths = tuple(sidecars)
    physical, folded, images = {}, set(), []
    for item in files:
        active()
        if not isinstance(item, VerifiedArchiveFile):
            raise ValueError('INTERCHANGE_SIDECAR_INVENTORY')
        path = interchange_path(item.path)
        if path.casefold() in folded or type(item.bytes) is not int or not 1 <= item.bytes <= 128 << 30:
            raise ValueError('INTERCHANGE_SIDECAR_INVENTORY')
        ArtifactRef(artifact_id=bundle.artifact_id, sha256=item.sha256)
        folded.add(path.casefold())
        physical[path] = item
        suffix = PurePosixPath(path).suffix.lower()
        if suffix == '.xmp':
            if PurePosixPath(path).suffix != '.xmp':
                raise ValueError('INTERCHANGE_SIDECAR_PAIRING')
            continue
        if suffix not in ('.jpg', '.jpeg', '.png', '.tif', '.tiff') or format == 'captureone_sidecar' and suffix not in ('.tif', '.tiff'):
            raise ValueError('INTERCHANGE_SIDECAR_IMAGE_FORMAT')
        images.append(path)
    if not 1 <= len(images) <= 5000:
        raise ValueError('INTERCHANGE_SIDECAR_INVENTORY')
    paired = [str(PurePosixPath(path).with_suffix('.xmp')) for path in images]
    if len(set(paired)) != len(paired) or set(sidecar_paths) != set(paired) or set(physical) != set(images) | set(paired):
        raise ValueError('INTERCHANGE_SIDECAR_PAIRING')
    entries, inventory, unsupported = [], [], []
    namespace = uuid5(bundle.artifact_id, 'sidecar-import:' + bundle.sha256)
    for image_path in sorted(images):
        active()
        sidecar_path = str(PurePosixPath(image_path).with_suffix('.xmp'))
        raw = sidecars[sidecar_path]
        record = physical[sidecar_path]
        if not isinstance(raw, bytes) or not 1 <= len(raw) <= 1 << 20 or len(raw) != record.bytes or sha256(raw).hexdigest() != record.sha256:
            raise ValueError('INTERCHANGE_SIDECAR_CONTENT')
        mapping = parse_xmp(raw)
        active()
        image = physical[image_path]
        entry_id = uuid5(namespace, 'entry:' + image_path)
        entries.append(BundleEntry(entry_id=entry_id, external_id=image_path,
            image_path=image_path, image_sha256=image.sha256, recipe_path=None, recipe_sha256=None,
            rating=mapping.rating if mapping.rating is not None else 0,
            pick=mapping.pick or 'unmarked',
            crop=None if mapping.crop is None else NormalizedRect(x0=mapping.crop[0], y0=mapping.crop[1], x1=mapping.crop[2], y1=mapping.crop[3]),
            label=mapping.label))
        reported = mapping.unsupported
        if reported:
            unsupported.append((image_path, len(reported),
                sha256(canonical_bytes(list(reported))).hexdigest()))
        for path, contract in ((image_path, 'EncodedImage'), (sidecar_path, 'XMP')):
            inventory.append(BundleInventoryEntry(artifact_id=uuid5(namespace, 'file:' + path),
                path=path, sha256=physical[path].sha256, contract=contract))
    manifest = BundleManifest(schema_version=1, renderer_version='v2.2', entries=entries,
        masks=[], inventory=sorted(inventory, key=lambda item: item.path))
    if len(canonical_bytes(manifest)) > 8 << 20:
        raise ValueError('INTERCHANGE_SIDECAR_MANIFEST_BUDGET')
    if supplied_manifest is not None:
        originals = {entry.image_path: entry for entry in supplied_manifest.entries}
        if (len(originals) != len(manifest.entries) or supplied_manifest.masks or supplied_manifest.backgrounds
                or {item.path: (item.sha256, item.contract) for item in supplied_manifest.inventory}
                != {item.path: (item.sha256, item.contract) for item in manifest.inventory}):
            raise ValueError('INTERCHANGE_SIDECAR_MANIFEST_MISMATCH')
        for entry in manifest.entries:
            active()
            original = originals.get(entry.image_path)
            if (original is None or original.recipe_path is not None or original.recipe_sha256 is not None
                    or original.rating != entry.rating or original.pick != entry.pick
                    or original.label != entry.label
                    or (original.crop is None) != (entry.crop is None)):
                raise ValueError('INTERCHANGE_SIDECAR_MANIFEST_MISMATCH')
            if entry.crop is not None and any(abs(getattr(original.crop, key)-getattr(entry.crop, key)) > 1e-12
                    for key in ('x0','y0','x1','y1')):
                raise ValueError('INTERCHANGE_SIDECAR_MANIFEST_MISMATCH')
        manifest = supplied_manifest
    active()
    return NormalizedSidecars(manifest, tuple(unsupported))
