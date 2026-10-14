"""Freeze a qualified export inventory before opening any source streams."""
from dataclasses import dataclass
from hashlib import sha256

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from .interchange_archive import ArchiveLimits, VerifiedArchiveFile, interchange_path
from .interchange_export import ExportFile, export_bundle
from .interchange_manifest import (BundleEntry, BundleInventoryEntry, BundleManifest,
                                  BundleMask, verify_bundle_manifest)
from .interchange_manifest import BundleBackground
from .interchange_plan import InterchangePlan


def _verify_export_compression_roles(manifest, sources):
    """Compression is part of the portable archive contract, not caller input."""
    roles = {}

    def assign(path, sidecar):
        previous = roles.get(path)
        if previous is not None and previous != sidecar:
            raise ValueError('INTERCHANGE_EXPORT_PREPARED_CHANGED')
        roles[path] = sidecar

    for entry in manifest.entries:
        assign(entry.image_path, False)
        if entry.recipe_path is not None:
            assign(entry.recipe_path, True)
    for mask in manifest.masks:
        assign(mask.path, False)
    for background in manifest.backgrounds or ():
        assign(background.path, False)
    for source in sources:
        role = roles.get(source.path)
        if role is None and source.path.startswith('dependencies/'):
            role = source.path.endswith('.json')
        if role is not None and source.sidecar is not role:
            raise ValueError('INTERCHANGE_EXPORT_PREPARED_CHANGED')


@dataclass(frozen=True)
class ExportSource:
    reference: ArtifactRef
    path: str
    contract: str
    bytes: int
    sidecar: bool


@dataclass(frozen=True)
class PreparedExportBundle:
    manifest_jcs: bytes
    sources: tuple[ExportSource, ...]
    limits: ArchiveLimits
    plan_sha256: str

    def write(self, output, open_source, *, active=lambda: None):
        # Return fresh wire identities to callbacks; do not expose owned refs.
        manifest = BundleManifest.model_validate_json(self.manifest_jcs)
        if canonical_bytes(manifest) != self.manifest_jcs:
            raise ValueError('INTERCHANGE_EXPORT_PREPARED_CHANGED')
        self.limits.validate()
        inventory = {item.path: item for item in manifest.inventory}
        by_path = {}
        for source in self.sources:
            expected = inventory.get(source.path)
            if expected is None or source.reference != ArtifactRef(
                    artifact_id=expected.artifact_id, sha256=expected.sha256) or source.contract != expected.contract \
                    or type(source.bytes) is not int or source.bytes < 1 or type(source.sidecar) is not bool \
                    or source.path in by_path:
                raise ValueError('INTERCHANGE_EXPORT_PREPARED_CHANGED')
            by_path[source.path] = ArtifactRef(artifact_id=expected.artifact_id, sha256=expected.sha256)
        if len(by_path) != len(inventory):
            raise ValueError('INTERCHANGE_EXPORT_PREPARED_CHANGED')
        _verify_export_compression_roles(manifest, self.sources)
        records = tuple(VerifiedArchiveFile(item.path, item.bytes, item.reference.sha256)
                        for item in self.sources)
        verify_bundle_manifest(self.manifest_jcs, (*records, VerifiedArchiveFile('manifest.json',
            len(self.manifest_jcs), sha256(self.manifest_jcs).hexdigest())))
        if (len(records) + 1 > self.limits.files
                or sum(item.bytes for item in records) + len(self.manifest_jcs) > self.limits.expanded_bytes):
            raise ValueError('INTERCHANGE_EXPORT_PREPARED_CHANGED')
        files = tuple(ExportFile(item.path, item.bytes, item.reference.sha256, item.sidecar)
                      for item in self.sources)
        return export_bundle(output, manifest, files,
            lambda path: open_source(by_path[path].model_copy(deep=True)), self.limits, active)


def prepare_export_bundle(plan, sources, masks, *, qualify_recipe,
                          qualify_mask, tenant_archive_bytes, active=lambda: None, backgrounds=()):
    """Qualification ports prove frozen source/recipe/portable dependencies.

    qualify_recipe(entry) returns its complete verified closure including the
    recipe root. qualify_mask(mask, source) proves the canvas and binary type.
    The caller rechecks current SQL authority before publishing and adoption.
    """
    if (not callable(qualify_recipe) or not callable(qualify_mask) or not callable(active)
            or type(tenant_archive_bytes) is not int or not 1 <= tenant_archive_bytes <= 64 << 30
            or not isinstance(sources, tuple) or not 1 <= len(sources) <= 29999
            or not isinstance(masks, tuple) or len(masks) > 5000):
        raise ValueError('INTERCHANGE_EXPORT_CONFIGURATION')
    active()
    plan = InterchangePlan.model_validate_json(canonical_bytes(plan))
    if plan.direction != 'export' or plan.format != 'photoeditor_bundle':
        raise ValueError('INTERCHANGE_EXPORT_FORMAT')
    limits = ArchiveLimits(min(plan.max_archive_bytes, tenant_archive_bytes),
                           plan.max_expanded_bytes, plan.max_files)
    limits.validate()
    owned, by_id, folded = [], {}, set()
    for item in sources:
        if not isinstance(item, ExportSource):
            raise ValueError('INTERCHANGE_EXPORT_SOURCE')
        ref = ArtifactRef.model_validate_json(canonical_bytes(item.reference))
        path = interchange_path(item.path)
        if (ref.artifact_id.int == 0 or ref.artifact_id in by_id or path.casefold() in folded
                or path.casefold() == 'manifest.json' or type(item.bytes) is not int
                or not 1 <= item.bytes <= limits.expanded_bytes or type(item.sidecar) is not bool
                or not isinstance(item.contract, str) or not 1 <= len(item.contract) <= 120):
            raise ValueError('INTERCHANGE_EXPORT_SOURCE')
        clone = ExportSource(ref, path, item.contract, item.bytes, item.sidecar)
        owned.append(clone);by_id[ref.artifact_id] = clone;folded.add(path.casefold())

    def require(ref):
        source = by_id.get(ref.artifact_id)
        if source is None or source.reference != ref:
            raise ValueError('INTERCHANGE_EXPORT_CLOSURE_INVENTORY')
        return source

    used, entries = set(), []
    for entry in sorted(plan.entries, key=lambda value: value.path):
        active()
        image, recipe = require(entry.content), require(entry.recipe)
        if image.path != entry.path or image.sidecar or recipe.contract != 'EditingRecipeV3' or not recipe.sidecar:
            raise ValueError('INTERCHANGE_EXPORT_ENTRY')
        closure = qualify_recipe(entry.model_copy(deep=True))
        if not isinstance(closure, tuple) or not 1 <= len(closure) <= 30000:
            raise ValueError('INTERCHANGE_EXPORT_RECIPE_QUALIFICATION')
        seen = set()
        for ref in closure:
            if not isinstance(ref, ArtifactRef) or ref.artifact_id in seen:
                raise ValueError('INTERCHANGE_EXPORT_RECIPE_QUALIFICATION')
            source = require(ref)
            seen.add(ref.artifact_id);used.add(source.reference.artifact_id)
        if entry.recipe.artifact_id not in seen:
            raise ValueError('INTERCHANGE_EXPORT_RECIPE_QUALIFICATION')
        used.add(image.reference.artifact_id)
        entries.append(BundleEntry(entry_id=entry.entry_id, external_id=entry.external_id,
            image_path=image.path, image_sha256=image.reference.sha256,
            recipe_path=recipe.path, recipe_sha256=recipe.reference.sha256,
            rating=entry.rating, pick=entry.pick, crop=entry.crop, label=entry.label))
        active()
    by_path = {item.path: item for item in owned}
    owned_masks = []
    for value in masks:
        mask = BundleMask.model_validate_json(canonical_bytes(value))
        source = by_path.get(mask.path)
        if (source is None or source.reference.sha256 != mask.sha256
                or source.contract not in ('MaskPixels', 'MaskArray') or source.sidecar
                or source.reference.artifact_id not in used):
            raise ValueError('INTERCHANGE_EXPORT_MASK')
        if qualify_mask(mask.model_copy(deep=True), ExportSource(source.reference.model_copy(deep=True),
                source.path, source.contract, source.bytes, source.sidecar)) is not True:
            raise ValueError('INTERCHANGE_EXPORT_MASK_QUALIFICATION')
        owned_masks.append(mask)
        active()
    if {mask.path for mask in owned_masks} != {item.path for item in owned
            if item.contract in ('MaskPixels', 'MaskArray')}:
        raise ValueError('INTERCHANGE_EXPORT_MASK_INVENTORY')
    if not isinstance(backgrounds, tuple) or len(backgrounds) > 5000:
        raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_MAPPINGS')
    owned_backgrounds = []
    by_path = {source.path: source for source in owned}
    for value in backgrounds:
        background = BundleBackground.model_validate_json(canonical_bytes(value))
        style = require(background.style)
        source = by_path.get(background.path)
        if (style.contract != 'StyleV2' or style.reference.artifact_id not in used
                or source is None or source.contract != 'StyleBackgroundAsset'
                or source.sidecar or source.reference.sha256 != background.sha256):
            raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_MAPPING')
        used.add(source.reference.artifact_id)
        owned_backgrounds.append(background)
    if used != set(by_id):
        raise ValueError('INTERCHANGE_EXPORT_UNREFERENCED_SOURCE')
    ordered = tuple(sorted(owned, key=lambda item: item.path))
    manifest = BundleManifest(schema_version=1, renderer_version='v2.2', entries=entries,
        masks=sorted(owned_masks, key=lambda item: item.path), inventory=[BundleInventoryEntry(
            artifact_id=item.reference.artifact_id, path=item.path,
            sha256=item.reference.sha256, contract=item.contract) for item in ordered],
        backgrounds=sorted(owned_backgrounds, key=lambda item: (str(item.style.artifact_id), item.portable_asset_id)) or None)
    raw = canonical_bytes(manifest)
    records = tuple(VerifiedArchiveFile(item.path, item.bytes, item.reference.sha256) for item in ordered)
    verify_bundle_manifest(raw, (*records, VerifiedArchiveFile('manifest.json', len(raw), sha256(raw).hexdigest())))
    if len(records) + 1 > limits.files or sum(item.bytes for item in records) + len(raw) > limits.expanded_bytes:
        raise ValueError('INTERCHANGE_EXPORT_QUOTA')
    active()
    return PreparedExportBundle(raw, ordered, limits, sha256(canonical_bytes(plan)).hexdigest())


def bind_prepared_export_bundle(plan, prepared):
    """Return an owned prepared inventory tied to the entire admitted plan."""
    if not isinstance(prepared, PreparedExportBundle):
        raise ValueError('INTERCHANGE_EXPORT_PREPARED_TYPE')
    plan = InterchangePlan.model_validate_json(canonical_bytes(plan))
    if (plan.direction != 'export' or plan.format != 'photoeditor_bundle'
            or prepared.plan_sha256 != sha256(canonical_bytes(plan)).hexdigest()):
        raise ValueError('INTERCHANGE_EXPORT_PREPARED_PLAN')
    prepared.limits.validate()
    if (prepared.limits.archive_bytes > plan.max_archive_bytes
            or prepared.limits.expanded_bytes > plan.max_expanded_bytes
            or prepared.limits.files > plan.max_files):
        raise ValueError('INTERCHANGE_EXPORT_PREPARED_LIMITS')
    manifest = BundleManifest.model_validate_json(prepared.manifest_jcs)
    if canonical_bytes(manifest) != prepared.manifest_jcs:
        raise ValueError('INTERCHANGE_EXPORT_PREPARED_CANONICAL')
    sources = tuple(ExportSource(ArtifactRef.model_validate_json(canonical_bytes(item.reference)),
        item.path, item.contract, item.bytes, item.sidecar) for item in prepared.sources)
    by_path = {item.path: item for item in sources}
    inventory = {item.path: item for item in manifest.inventory}
    if len(by_path) != len(sources) or set(by_path) != set(inventory):
        raise ValueError('INTERCHANGE_EXPORT_PREPARED_INVENTORY')
    for source in sources:
        listed = inventory[source.path]
        if (source.reference.artifact_id != listed.artifact_id or source.reference.sha256 != listed.sha256
                or source.contract != listed.contract or type(source.sidecar) is not bool
                or type(source.bytes) is not int or source.bytes < 1):
            raise ValueError('INTERCHANGE_EXPORT_PREPARED_INVENTORY')
    _verify_export_compression_roles(manifest, sources)
    ordered = sorted(plan.entries, key=lambda entry: entry.path)
    if len(manifest.entries) != len(ordered):
        raise ValueError('INTERCHANGE_EXPORT_PREPARED_ENTRIES')
    for expected, actual in zip(ordered, manifest.entries):
        if (actual.entry_id != expected.entry_id or actual.external_id != expected.external_id
                or actual.image_path != expected.path or actual.image_sha256 != expected.content.sha256
                or actual.recipe_sha256 != expected.recipe.sha256 or actual.recipe_path is None
                or actual.rating != expected.rating or actual.pick != expected.pick or actual.crop != expected.crop
                or actual.label != expected.label):
            raise ValueError('INTERCHANGE_EXPORT_PREPARED_ENTRIES')
        image, recipe = by_path.get(actual.image_path), by_path.get(actual.recipe_path)
        if (image is None or recipe is None or image.reference != expected.content or image.sidecar
                or recipe.reference != expected.recipe or recipe.contract != 'EditingRecipeV3' or not recipe.sidecar):
            raise ValueError('INTERCHANGE_EXPORT_PREPARED_ENTRIES')
    records = tuple(VerifiedArchiveFile(item.path, item.bytes, item.reference.sha256) for item in sources)
    verify_bundle_manifest(prepared.manifest_jcs, (*records, VerifiedArchiveFile('manifest.json',
        len(prepared.manifest_jcs), sha256(prepared.manifest_jcs).hexdigest())))
    if len(records)+1 > prepared.limits.files or sum(item.bytes for item in records)+len(prepared.manifest_jcs) > prepared.limits.expanded_bytes:
        raise ValueError('INTERCHANGE_EXPORT_PREPARED_LIMITS')
    return PreparedExportBundle(bytes(prepared.manifest_jcs), sources, prepared.limits, prepared.plan_sha256)
