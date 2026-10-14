"""Prepare deterministic XMP subset exports and explicit fidelity reports."""
from dataclasses import dataclass
from hashlib import sha256
from pathlib import PurePosixPath

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from .interchange_archive import interchange_path
from .interchange_export_prepare import ExportSource
from .interchange_mapping import MappingEntry, MappingField
from .interchange_plan import InterchangePlan
from .interchange_xmp import XMPMapping, export_xmp


@dataclass(frozen=True)
class ExportImageEvidence:
    reference: ArtifactRef
    bytes: int
    media_type: str
    orientation: int


@dataclass(frozen=True)
class FrozenSidecar:
    path: str
    payload: bytes


@dataclass(frozen=True)
class PreparedSidecarExport:
    images: tuple[ExportSource, ...]
    sidecars: tuple[FrozenSidecar, ...]
    mapping_jcs: tuple[bytes, ...]
    plan_sha256: str


def prepare_export_sidecars(plan, *, qualify_image, active=lambda: None):
    """Image qualification must prove encoded metadata and frozen binding.

    This serializes rating, PhotoEditor pick, the fixed color-label palette,
    EXIF orientation and crop.
    Full recipe and RAW development transfer remain explicitly unsupported.
    Capture One compatibility means TIFF plus this XMP subset, never .cos.
    """
    if not callable(qualify_image) or not callable(active):
        raise ValueError('INTERCHANGE_SIDECAR_CONFIGURATION')
    active()
    plan = InterchangePlan.model_validate_json(canonical_bytes(plan))
    if plan.direction != 'export' or plan.format not in ('xmp', 'captureone_sidecar'):
        raise ValueError('INTERCHANGE_SIDECAR_FORMAT')
    images, sidecars, mappings, paths, used_ids = [], [], [], set(), set()
    for entry in sorted(plan.entries, key=lambda item: item.path):
        active()
        evidence = qualify_image(entry.model_copy(deep=True))
        if not isinstance(evidence, ExportImageEvidence):
            raise ValueError('INTERCHANGE_SIDECAR_IMAGE_QUALIFICATION')
        reference = ArtifactRef.model_validate_json(canonical_bytes(evidence.reference))
        if (reference != entry.content or reference.artifact_id in used_ids
                or type(evidence.bytes) is not int or not 1 <= evidence.bytes <= plan.max_expanded_bytes
                or type(evidence.orientation) is not int or not 1 <= evidence.orientation <= 8
                or evidence.media_type not in ('image/tiff', 'image/jpeg', 'image/png')):
            raise ValueError('INTERCHANGE_SIDECAR_IMAGE_QUALIFICATION')
        suffix = PurePosixPath(entry.path).suffix.lower()
        if (plan.format == 'captureone_sidecar'
                and (evidence.media_type != 'image/tiff' or suffix not in ('.tif', '.tiff'))):
            raise ValueError('INTERCHANGE_CAPTUREONE_REQUIRES_TIFF')
        sidecar_path = interchange_path(str(PurePosixPath(entry.path).with_suffix('.xmp')))
        for path in (entry.path, sidecar_path):
            if path.casefold() in paths:
                raise ValueError('INTERCHANGE_SIDECAR_PATH_COLLISION')
            paths.add(path.casefold())
        used_ids.add(reference.artifact_id)
        crop = entry.crop
        rectangle = None if crop is None else (crop.x0, crop.y0, crop.x1, crop.y1)
        payload = export_xmp(XMPMapping(entry.rating, entry.pick, evidence.orientation, rectangle, (), entry.label))
        images.append(ExportSource(reference, entry.path, 'EncodedImage', evidence.bytes, False))
        sidecars.append(FrozenSidecar(sidecar_path, payload))
        fields = [MappingField(name=name, status='exact', reason=None)
                  for name in ('rating', 'pick', 'orientation')]
        if crop is not None:
            fields.append(MappingField(name='crop', status='exact', reason=None))
        if entry.label is not None:
            fields.append(MappingField(name='label', status='exact', reason=None))
        fields.extend([
            MappingField(name='recipe', status='unsupported', reason='xmp_subset_has_no_recipe_contract'),
            MappingField(name='raw_development', status='unsupported', reason='renderer_specific_parameters_not_portable')])
        mappings.append(canonical_bytes(MappingEntry(entry_id=entry.entry_id,
            external_id=entry.external_id, asset_version_id=entry.binding.asset_version_id,
            proposed_document=None, fields=fields)))
        active()
    if (len(images) + len(sidecars) + 1 > plan.max_files
            or sum(item.bytes for item in images) + sum(len(item.payload) for item in sidecars) > plan.max_expanded_bytes):
        raise ValueError('INTERCHANGE_SIDECAR_QUOTA')
    return PreparedSidecarExport(tuple(images), tuple(sidecars), tuple(mappings), sha256(canonical_bytes(plan)).hexdigest())
