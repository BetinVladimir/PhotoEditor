"""Derive descriptor occurrence pins from exact declared export inputs."""
from copy import deepcopy
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.operations.session.contracts import InputSnapshot
from .adoption import AdoptionInvalid
from .interchange_plan import InterchangePlan
from .render_recipe import decode_frozen_render_recipe
from .interchange_descriptor_occurrences import PreparedDescriptorOccurrenceQualifier, SQLExportDescriptorAuthority


def load_export_descriptor_pins(reader, snapshot_ref, plan, *, check_active=lambda: None):
    check_active()
    snapshot_ref = ArtifactRef.model_validate_json(canonical_bytes(snapshot_ref))
    plan = InterchangePlan.model_validate_json(canonical_bytes(plan))
    if plan.direction != 'export' or plan.format != 'photoeditor_bundle':
        raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_EXPORT_PLAN')
    total = 0
    documents = {}
    identities = {}
    def read(reference, contract, maximum):
        nonlocal total
        check_active()
        if reference.artifact_id.int == 0:
            raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_INPUT_IDENTITY')
        identity = (reference.sha256, contract)
        old = identities.setdefault(reference.artifact_id, identity)
        if old != identity:
            raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_INPUT_IDENTITY')
        entry = reader.entries.get((reference.artifact_id, reference.sha256))
        if entry is None or entry.contract != contract:
            raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_DECLARED_INPUT')
        key = (reference.artifact_id, reference.sha256, contract)
        if key not in documents:
            raw = reader.read_json(reference.model_copy(deep=True), maximum)
            if (not isinstance(raw, bytes) or not 2 <= len(raw) <= maximum
                    or sha256(raw).hexdigest() != reference.sha256
                    or total + len(raw) > 128 << 20):
                raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_INPUT_BYTES')
            documents[key] = raw
            total += len(raw)
        return documents[key]
    raw = read(snapshot_ref, 'InputSnapshot', 8 << 20)
    snapshot = InputSnapshot.model_validate_json(raw)
    snapshot_wire = snapshot.model_dump(mode='json')
    digest = canonical_hash({key: snapshot_wire[key] for key in ('schema_version', 'snapshot_id', 'items')})
    if (canonical_bytes(snapshot) != raw or snapshot.snapshot_digest != digest
            or plan.snapshot_digest != digest or snapshot.snapshot_id.int == 0
            or [item.ordinal for item in snapshot.items] != list(range(len(snapshot.items)))
            or len({item.binding.asset_version_id for item in snapshot.items}) != len(snapshot.items)):
        raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_SNAPSHOT')
    if any(entry.binding is None or entry.recipe is None for entry in plan.entries):
        raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_SELECTION')
    selected = {entry.binding.asset_version_id: entry for entry in plan.entries}
    if len(selected) != len(plan.entries) or len(selected) != len(snapshot.items):
        raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_SELECTION')
    pins = []
    for item in snapshot.items:
        check_active()
        entry = selected.get(item.binding.asset_version_id)
        if entry is None or entry.binding != item.binding or item.working_image != item.binding.source:
            raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_SELECTION')
        root_raw = read(item.recipe, 'FrozenRenderRecipe', 1 << 20)
        root = decode_frozen_render_recipe(root_raw)
        if (entry.recipe is None or root.scalars.source != item.binding
                or root.recipe_sha256 != entry.recipe.sha256
                or entry.recipe.artifact_id == item.recipe.artifact_id):
            raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_FROZEN_SOURCE')
        portable_raw = read(entry.recipe, 'EditingRecipeV3', 1 << 20)
        if portable_raw != canonical_bytes(root.recipe):
            raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_PORTABLE_RECIPE')
        learning = root.learning
        if learning is None:
            continue
        descriptor_raw = read(learning.descriptor, 'DescriptorShard', 8 << 20)
        pins.append(dict(root=item.recipe.model_copy(deep=True), root_body=root_raw,
            descriptor=learning.descriptor.model_copy(deep=True), descriptor_body=descriptor_raw,
            selected_binding=item.binding.model_copy(deep=True), analysis_id=learning.analysis_id,
            selected_ordinal=learning.ordinal, model_lock_sha256=root.scalars.model_lock_sha))
    reader.recheck()
    check_active()
    return tuple(pins)


def build_sql_export_descriptor_occurrences(*, database, ctx, params, reader, snapshot_ref, plan, qualify_other):
    current = SQLExportDescriptorAuthority(database, ctx, params)
    pins = load_export_descriptor_pins(reader, snapshot_ref, plan, check_active=ctx.check_active)
    return PreparedDescriptorOccurrenceQualifier(pins=pins, qualify_current=current,
        qualify_other=qualify_other, check_active=ctx.check_active)


class SQLExportDescriptorOccurrenceFactory:
    """Bind snapshot selection and current authority to the running export lease."""
    def __init__(self, database, *, qualify_other):
        if (not callable(qualify_other)
                or not callable(getattr(database, 'interchange_export_snapshot', None))
                or not callable(getattr(database, 'interchange_export_descriptor_authority', None))):
            raise ValueError('INTERCHANGE_DESCRIPTOR_FACTORY_INSTALLATION')
        self.database, self.other = database, qualify_other

    def __call__(self, ctx, artifacts, authority, plan, reader):
        from .interchange_plan import InterchangeExportParams
        ctx.check_active()
        params = artifacts.params(InterchangeExportParams)
        # Validate the operation and exact Params before any SQL lookup.
        SQLExportDescriptorAuthority(self.database, ctx, params)
        raw = canonical_bytes(params)
        snapshot = deepcopy(self.database.interchange_export_snapshot(ctx.lease, raw))
        ctx.check_active()
        if not isinstance(snapshot, dict) or set(snapshot) != {'artifact_id', 'sha256'}:
            raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_SNAPSHOT_AUTHORITY')
        snapshot_ref = ArtifactRef.model_validate_json(canonical_bytes(snapshot))
        if snapshot_ref.artifact_id.int == 0:
            raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_SNAPSHOT_AUTHORITY')
        if plan.project_id != authority.project_id or plan.snapshot_digest != authority.snapshot_digest:
            raise AdoptionInvalid('INTERCHANGE_DESCRIPTOR_PLAN_AUTHORITY')
        def other(event):
            ctx.check_active()
            allowed = self.other(ctx, authority, deepcopy(event))
            ctx.check_active()
            return allowed is True
        return build_sql_export_descriptor_occurrences(database=self.database, ctx=ctx, params=params,
            reader=reader, snapshot_ref=snapshot_ref, plan=plan, qualify_other=other)
