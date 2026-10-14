"""Package qualified encoded images and inline XMP into a normalized ZIP64."""
from contextlib import contextmanager
from hashlib import sha256
from io import BytesIO
from uuid import uuid5

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from .interchange_archive import ArchiveLimits, interchange_path
from .interchange_export import ExportFile, export_bundle
from .interchange_export_sidecars import PreparedSidecarExport
from .interchange_manifest import BundleManifest, BundleEntry, BundleInventoryEntry
from .interchange_plan import InterchangePlan
from .interchange_xmp import parse_xmp


def write_sidecar_archive(output, plan, prepared, open_image, *, tenant_archive_bytes,
                          active=lambda: None):
    if (not isinstance(prepared, PreparedSidecarExport) or not callable(open_image)
            or not callable(active) or type(tenant_archive_bytes) is not int
            or not 1 <= tenant_archive_bytes <= 64 << 30):
        raise ValueError('INTERCHANGE_SIDECAR_ARCHIVE_CONFIGURATION')
    plan = InterchangePlan.model_validate_json(canonical_bytes(plan))
    if (plan.direction != 'export' or plan.format not in ('xmp', 'captureone_sidecar')
            or prepared.plan_sha256 != sha256(canonical_bytes(plan)).hexdigest()):
        raise ValueError('INTERCHANGE_SIDECAR_ARCHIVE_FORMAT')
    entries = sorted(plan.entries, key=lambda entry: entry.path)
    if len(prepared.images) != len(entries) or len(prepared.sidecars) != len(entries):
        raise ValueError('INTERCHANGE_SIDECAR_ARCHIVE_INVENTORY')
    inventory, records, manifest_entries, sources = [], [], [], {}
    from pathlib import PurePosixPath
    for entry, image, sidecar in zip(entries, prepared.images, prepared.sidecars):
        reference = ArtifactRef.model_validate_json(canonical_bytes(image.reference))
        image_path, sidecar_path = interchange_path(image.path), interchange_path(sidecar.path)
        if (reference != entry.content or image_path != entry.path or image.sidecar
                or image.contract != 'EncodedImage' or type(image.bytes) is not int or image.bytes < 1
                or sidecar_path != str(PurePosixPath(entry.path).with_suffix('.xmp'))
                or not isinstance(sidecar.payload, bytes)):
            raise ValueError('INTERCHANGE_SIDECAR_ARCHIVE_INVENTORY')
        metadata = parse_xmp(sidecar.payload)
        crop = entry.crop
        expected_crop = None if crop is None else (crop.x0, crop.y0, crop.x1, crop.y1)
        if (metadata.unsupported or metadata.rating != entry.rating or metadata.pick != entry.pick
                or metadata.label != entry.label
                or metadata.orientation is None or (metadata.crop is None) != (expected_crop is None)
                or expected_crop is not None and any(abs(a-b) > 1e-12 for a,b in zip(metadata.crop, expected_crop))):
            raise ValueError('INTERCHANGE_SIDECAR_ARCHIVE_METADATA')
        digest = sha256(sidecar.payload).hexdigest()
        sidecar_id = uuid5(entry.entry_id, 'interchange:xmp:' + digest)
        inventory.extend([BundleInventoryEntry(artifact_id=reference.artifact_id, path=image_path,
            sha256=reference.sha256, contract='EncodedImage'), BundleInventoryEntry(artifact_id=sidecar_id,
            path=sidecar_path, sha256=digest, contract='XMP')])
        records.extend([ExportFile(image_path, image.bytes, reference.sha256, False),
                        ExportFile(sidecar_path, len(sidecar.payload), digest, True)])
        manifest_entries.append(BundleEntry(entry_id=entry.entry_id, external_id=entry.external_id,
            image_path=image_path, image_sha256=reference.sha256, recipe_path=None, recipe_sha256=None,
            rating=entry.rating, pick=entry.pick, crop=entry.crop, label=entry.label))
        sources[image_path] = reference
        sources[sidecar_path] = bytes(sidecar.payload)
    manifest = BundleManifest(schema_version=1, renderer_version='v2.2', entries=manifest_entries,
                              masks=[], inventory=sorted(inventory, key=lambda item: item.path))
    limits = ArchiveLimits(min(plan.max_archive_bytes, tenant_archive_bytes),
                           plan.max_expanded_bytes, plan.max_files)

    @contextmanager
    def open_source(path):
        source = sources[path]
        if isinstance(source, bytes):
            with BytesIO(source) as stream:
                yield stream
        else:
            with open_image(source.model_copy(deep=True)) as stream:
                yield stream
    return export_bundle(output, manifest, tuple(records), open_source, limits, active)
