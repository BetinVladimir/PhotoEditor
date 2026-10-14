from types import SimpleNamespace
from uuid import uuid4
import pytest
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.contracts.images import Canvas, ColorSpace, TransformChain, WorkingImage
from photo_worker.operations.learning.contracts import FrozenRecipe
from photo_worker.operations.session.contracts import CorrectionVector
from photo_worker.v2.render_recipe import EditingRecipeV3, FrozenRenderRecipe
from photo_worker.v2.render_background_binding import FrozenBackgroundAsset
from photo_worker.v2.interchange_export_backgrounds import resolve_export_background_pins
from test_interchange_export_inventory import inventory_fixture
from test_interchange_export_style import style_fixture


@pytest.mark.parametrize('mode', ['missing', 'duplicate', 'digest', 'binding', 'frozen_missing', 'cancelled'])
def test_background_pins_refuse_unselected_snapshot_or_frozen_parent(mode):
    plan, _, _, _ = inventory_fixture()
    frozen = ArtifactRef(artifact_id=uuid4(), sha256='e' * 64)
    item = dict(ordinal=0, binding=plan.entries[0].binding.model_dump(mode='json'),
        working_image=plan.entries[0].binding.source.model_dump(mode='json'),
        captured_at=None, camera_hash=None, recipe=frozen.model_dump(mode='json'))
    if mode == 'binding': item['binding']['asset_version_id'] = str(uuid4())
    payload = dict(schema_version=1, snapshot_id=str(uuid4()), items=[item])
    digest = canonical_hash(payload);payload['snapshot_digest'] = digest
    plan.snapshot_digest = digest if mode != 'digest' else 'f' * 64
    raw = canonical_bytes(payload)
    reference = ArtifactRef(artifact_id=uuid4(), sha256=canonical_hash(payload))
    entries = {(reference.artifact_id, reference.sha256): SimpleNamespace(ref=reference, contract='InputSnapshot')}
    if mode == 'missing': entries.clear()
    if mode == 'duplicate':
        other = ArtifactRef(artifact_id=uuid4(), sha256=reference.sha256)
        entries[(other.artifact_id, other.sha256)] = SimpleNamespace(ref=other, contract='InputSnapshot')
    class Reader:
        calls = 0
        def __init__(self): self.entries = entries
        def read_json(self, ref, maximum):
            self.calls += 1
            assert ref == reference
            return raw
        def recheck(self): raise AssertionError('invalid graph qualified')
    reader = Reader()
    def active():
        if mode == 'cancelled': raise RuntimeError('cancelled')
    with pytest.raises((ValueError, RuntimeError)):
        resolve_export_background_pins(plan, reader, active=active)
    if mode in ('missing', 'duplicate', 'cancelled'): assert reader.calls == 0


@pytest.mark.parametrize('selection', ['style', 'override', 'cleared', 'canvas_changed',
                                        'stale_pin', 'wrong_pin'])
def test_background_pins_follow_frozen_override_selection(selection):
    plan, _, _, _ = inventory_fixture()
    def pin(value):
        raw = canonical_bytes(value)
        return ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest()), raw
    style_value = style_fixture()
    style_value['extensions']['background'] = dict(mode='asset', asset_id='original',
        blur_strength=0, protect_subject=True)
    style, style_raw = pin(style_value)
    source = WorkingImage(pixels=ArtifactRef(artifact_id=uuid4(), sha256='d' * 64),
        canvas=Canvas(width=32, height=24, orientation=1, space_id='canonical', source_sha256='a' * 64),
        color=ColorSpace(profile_id='work_rec2020_gamma24_v1',
            icc_sha256=style_value['color']['icc_sha256'], encoding='gamma24'),
        dtype='float32', alpha=None, transform_chain=TransformChain.create([]))
    source_ref, source_raw = pin(source)
    binding = plan.entries[0].binding
    binding.source = source_ref
    binding.canvas_hash = 'f' * 64 if selection == 'canvas_changed' else canonical_hash(source.canvas)
    replacement = dict(mode='asset', asset_id='replacement', blur_strength=0, protect_subject=True)
    override_value = dict(schema_version=1, input_sha256='a' * 64,
        modules={'extensions': {'background': replacement if selection in ('override', 'wrong_pin') else None}}, revision=1)
    if selection == 'style':
        override_value['modules'] = {'strength': 0.7}
    override, override_raw = pin(override_value)
    recipe = EditingRecipeV3(schema_version=3, source=binding, base_style=style,
        session_corrections=None, learning_version_id=None, overrides=[override], layer_stack=None,
        renderer_version='v2.2', parameter_delta=CorrectionVector(
            ev=0, log_r=0, log_g=0, log_b=0, saturation_delta=0, retouch_delta=0))
    recipe_ref, _ = pin(recipe)
    plan.entries[0].recipe = recipe_ref
    background_source = ArtifactRef(artifact_id=uuid4(), sha256='e' * 64)
    selected_id = 'replacement' if selection == 'override' else 'original'
    background_pin = None if selection == 'cleared' else FrozenBackgroundAsset(schema_version=1,
        portable_asset_id=selected_id, style_version_id=uuid4(), style=style,
        source=background_source, expected_bytes=16, media_type='image/png', suffix='.png',
        assume_profile=None, work_icc_sha='b' * 64)
    frozen = FrozenRenderRecipe(schema_version=1, recipe=recipe,
        recipe_sha256=canonical_hash(recipe), scalars=FrozenRecipe(schema_version=1,
            source=binding, base_style=style, effective_delta=recipe.parameter_delta,
            renderer_version='v2.2', icc_lock_sha='b' * 64, model_lock_sha='c' * 64),
        learning=None, background_asset=background_pin)
    frozen_ref, frozen_raw = pin(frozen)
    item = dict(ordinal=0, binding=binding.model_dump(mode='json'),
        working_image=source_ref.model_dump(mode='json'), captured_at=None, camera_hash=None,
        recipe=frozen_ref.model_dump(mode='json'))
    snapshot_value = dict(schema_version=1, snapshot_id=str(uuid4()), items=[item])
    digest = canonical_hash(snapshot_value)
    snapshot_value['snapshot_digest'] = digest
    plan.snapshot_digest = digest
    snapshot_ref, snapshot_raw = pin(snapshot_value)
    bodies = {snapshot_ref.artifact_id: snapshot_raw, frozen_ref.artifact_id: frozen_raw,
        style.artifact_id: style_raw, source_ref.artifact_id: source_raw,
        override.artifact_id: override_raw}
    contracts = ((snapshot_ref, 'InputSnapshot'), (frozen_ref, 'FrozenRenderRecipe'),
        (style, 'StyleV2'), (source_ref, 'WorkingImage'), (override, 'Override'),
        (background_source, 'StyleBackgroundAsset'))
    class Reader:
        entries = {(ref.artifact_id, ref.sha256): SimpleNamespace(ref=ref, contract=contract)
                   for ref, contract in contracts}
        rechecks = 0
        def read_json(self, ref, maximum):
            body = bodies[ref.artifact_id]
            assert len(body) <= maximum
            return body
        def evidence(self, ref):
            assert ref == background_source
            return SimpleNamespace(bytes=16, media_type='image/png')
        def recheck(self): self.rechecks += 1
    reader = Reader()
    if selection == 'canvas_changed':
        with pytest.raises(ValueError, match='BACKGROUND_CANVAS_CHANGED'):
            resolve_export_background_pins(plan, reader)
    elif selection == 'stale_pin':
        with pytest.raises(ValueError, match='BACKGROUND_STALE_PIN'):
            resolve_export_background_pins(plan, reader)
    elif selection == 'wrong_pin':
        with pytest.raises(ValueError, match='BACKGROUND_PORTABLE_ID'):
            resolve_export_background_pins(plan, reader)
    else:
        pins = resolve_export_background_pins(plan, reader)
        assert pins == (() if selection == 'cleared' else ((plan.entries[0].entry_id, background_pin),))
        assert reader.rechecks == 1
