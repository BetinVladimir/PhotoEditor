"""Resolve portable backgrounds only from authenticated snapshot/frozen parents."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes, canonical_hash, parse_json
from photo_worker.contracts.style import StyleV2
from photo_worker.contracts.override import OverrideV1
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import InputSnapshot
from .interchange_plan import InterchangePlan
from .render_recipe import decode_frozen_render_recipe


def resolve_export_background_pins(plan, reader, *, active=lambda: None):
    active()
    plan = InterchangePlan.model_validate_json(canonical_bytes(plan))
    if plan.direction != 'export':
        raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_DIRECTION')
    snapshots = [entry for entry in reader.entries.values() if entry.contract == 'InputSnapshot']
    if len(snapshots) != 1:
        raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_SNAPSHOT')
    reference = snapshots[0].ref
    raw = reader.read_json(reference, 8 << 20)
    snapshot = InputSnapshot.model_validate_json(raw)
    if (canonical_bytes(snapshot) != raw or sha256(raw).hexdigest() != reference.sha256
            or snapshot.snapshot_digest != plan.snapshot_digest
            or canonical_hash(snapshot.model_dump(mode='json', exclude={'snapshot_digest'})) != plan.snapshot_digest):
        raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_SNAPSHOT_PIN')
    items = {item.binding.asset_version_id: item for item in snapshot.items}
    if (len(items) != len(snapshot.items) or len(items) != len(plan.entries)
            or [item.ordinal for item in snapshot.items] != list(range(len(snapshot.items)))):
        raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_SNAPSHOT_ITEMS')
    pins = []
    for entry in plan.entries:
        active()
        item = items.pop(entry.binding.asset_version_id, None)
        if item is None or item.binding != entry.binding or item.working_image != entry.binding.source:
            raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_BINDING')
        declared = reader.entries.get((item.recipe.artifact_id, item.recipe.sha256))
        if declared is None or declared.contract != 'FrozenRenderRecipe':
            raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_FROZEN_CONTRACT')
        frozen_raw = reader.read_json(item.recipe, 8 << 20)
        frozen = decode_frozen_render_recipe(frozen_raw)
        if (sha256(frozen_raw).hexdigest() != item.recipe.sha256
                or frozen.scalars.source != entry.binding
                or frozen.recipe_sha256 != entry.recipe.sha256 or frozen.learning is not None):
            raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_FROZEN_PIN')
        style_ref = frozen.scalars.base_style
        declared = reader.entries.get((style_ref.artifact_id, style_ref.sha256))
        if declared is None or declared.contract != 'StyleV2':
            raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_STYLE_CONTRACT')
        style_raw = reader.read_json(style_ref, 8 << 20)
        if sha256(style_raw).hexdigest() != style_ref.sha256:
            raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_STYLE_PIN')
        style = StyleV2.from_bytes(style_raw)
        if canonical_bytes(style) != style_raw:
            raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_STYLE_CANONICAL')
        background = style.extensions.background
        if frozen.recipe.overrides:
            source_raw = reader.read_json(entry.binding.source, 1 << 20)
            if sha256(source_raw).hexdigest() != entry.binding.source.sha256 or canonical_bytes(parse_json(source_raw)) != source_raw:
                raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_SOURCE_CHANGED')
            source = WorkingImage.model_validate_json(source_raw)
            if canonical_hash(source.canvas) != entry.binding.canvas_hash:
                raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_CANVAS_CHANGED')
            for override_ref in frozen.recipe.overrides:
                declared = reader.entries.get((override_ref.artifact_id, override_ref.sha256))
                if declared is None or declared.contract != 'Override':
                    raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_OVERRIDE_CONTRACT')
                override_raw = reader.read_json(override_ref, 8 << 20)
                if sha256(override_raw).hexdigest() != override_ref.sha256:
                    raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_OVERRIDE_PIN')
                override = OverrideV1.model_validate_json(override_raw)
                if (canonical_bytes(parse_json(override_raw, max_bytes=8 << 20)) != override_raw
                        or override.input_sha256 != source.canvas.source_sha256):
                    raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_OVERRIDE_SOURCE')
                # Each non-null extensions module replaces the whole preceding
                # module. A replacement with no background clears the old one.
                if override.modules.extensions is not None:
                    background = override.modules.extensions.background
                active()
        pin = frozen.background_asset
        if background is not None and background.mode == 'asset':
            if pin is None or pin.portable_asset_id != background.asset_id:
                raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_PORTABLE_ID')
            declared = reader.entries.get((pin.source.artifact_id, pin.source.sha256))
            if declared is None or declared.contract != 'StyleBackgroundAsset':
                raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_SOURCE_CONTRACT')
            evidence = reader.evidence(pin.source)
            if evidence.bytes != pin.expected_bytes or evidence.media_type != pin.media_type:
                raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_SOURCE_PIN')
            pins.append((entry.entry_id, pin.model_copy(deep=True)))
        elif pin is not None:
            raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_STALE_PIN')
    reader.recheck();active()
    return tuple(pins)


def qualify_export_background_sources(plan, reader, scratch, *, active=lambda: None):
    """Resolve and physically verify sources; retain portable identity receipts.

    The returned tuple is preparation evidence, not a completed archive manifest.
    The bundle mapping/import codec must preserve each pin's portable_asset_id.
    """
    from photo_worker.operations.interchange.export_images import qualify_export_background
    from .interchange_export_prepare import ExportSource
    pins = resolve_export_background_pins(plan, reader, active=active)
    sources = {}
    receipts = []
    for entry_id, pin in pins:
        active()
        key = pin.source.artifact_id
        source = ExportSource(pin.source.model_copy(deep=True),
            f'dependencies/{key}.bin', 'StyleBackgroundAsset', pin.expected_bytes, False)
        previous = sources.get(key)
        if previous is not None and previous != source:
            raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_ALIAS')
        if previous is None:
            evidence = qualify_export_background(pin, reader, scratch, active=active)
            if (evidence.reference != pin.source or evidence.bytes != pin.expected_bytes
                    or evidence.media_type != pin.media_type):
                raise ValueError('INTERCHANGE_EXPORT_BACKGROUND_PHYSICAL_RECEIPT')
            sources[key] = source
        receipts.append((entry_id, canonical_bytes(pin)))
    reader.recheck();active()
    return (tuple(sorted(sources.values(), key=lambda source: source.path)), tuple(receipts))
