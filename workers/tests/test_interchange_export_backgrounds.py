from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.v2.interchange_export_backgrounds import resolve_export_background_pins
from test_interchange_export_inventory import inventory_fixture


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
