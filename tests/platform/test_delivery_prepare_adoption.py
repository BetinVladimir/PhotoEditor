import json
"""Independent prepare reconstruction with explicit SQL/object evidence doubles."""
from hashlib import sha256
from dataclasses import replace
from uuid import UUID
import pytest
from photo_worker.contracts.base import ArtifactRef as WireRef, canonical_bytes
from photo_worker.operations.delivery.preparation import prepare_delivery
from photo_worker.v2.adoption import ArtifactRef, ArtifactEvidence, AdoptionInvalid
from photo_worker.v2.delivery_prepare_adoption import verify_delivery_preparation
from test_delivery_preparation import fixture, ref, uid
from test_frozen_export_preset import fixture as preset_fixture


class Reader:
    def __init__(self, objects, root):
        self.objects = {UUID(key): value for key, value in objects.items()}
        self.root = root
        self.metadata = {key: ArtifactEvidence(ArtifactRef(key, sha256(raw).hexdigest()),
            'application/json', len(raw), 'document' if key == root.artifact_id else None,
            'output' if key == root.artifact_id else 'input') for key, raw in self.objects.items()}
        self.outputs = {root}
    def evidence(self, reference): return self.metadata[reference.artifact_id]
    def read_json(self, reference, maximum):
        raw = self.objects[reference.artifact_id]
        assert len(raw) <= maximum
        return raw
    def staged_outputs(self): return self.outputs


def adoption_fixture():
    frozen, objects = fixture()
    config = canonical_bytes(preset_fixture())
    objects[uid(5)] = config
    frozen['presets'][0].update(config=ref(5, config), preset_sha=sha256(config).hexdigest())
    frozen_raw = canonical_bytes(frozen)
    frozen_ref = WireRef.model_validate_json(json.dumps(ref(6, frozen_raw)))
    params_raw = canonical_bytes({'schema_version': 1, 'input': ref(6, frozen_raw)})
    result = prepare_delivery(frozen_ref, frozen_raw, lambda reference: objects[str(reference.artifact_id)], lambda raw: None)
    result_raw = canonical_bytes(result)
    root = ArtifactRef(UUID(uid(8)), sha256(result_raw).hexdigest())
    params_ref = ArtifactRef(UUID(uid(7)), sha256(params_raw).hexdigest())
    objects.update({uid(6): frozen_raw, uid(7): params_raw, uid(8): result_raw})
    return Reader(objects, root), params_ref, root


def test_reconstructs_exact_frozen_preparation_and_owns_proof_bytes():
    reader, params, root = adoption_fixture()
    checks = []
    result = verify_delivery_preparation(reader, params, root, check_active=lambda: checks.append(True))
    assert result.params_jcs == reader.objects[params.artifact_id]
    assert result.result_jcs == reader.objects[root.artifact_id]
    reader.objects[root.artifact_id] = b'{}'
    assert len(result.result_jcs) > 2 and len(checks) > 5


@pytest.mark.parametrize('change', ['source', 'role', 'physical', 'count', 'extra_output', 'params_canonical', 'frozen_evidence'])
def test_refuses_untrusted_result_or_inputs(change):
    reader, params, root = adoption_fixture()
    if change == 'source':
        reader.metadata[root.artifact_id] = replace(reader.metadata[root.artifact_id], source='input')
    elif change == 'role':
        reader.metadata[root.artifact_id] = replace(reader.metadata[root.artifact_id], role='report')
    elif change == 'physical':
        reader.objects[root.artifact_id] = b'{}'
    elif change == 'count':
        import json
        value = json.loads(reader.objects[root.artifact_id]); value['slot_count'] = 2
        raw = canonical_bytes(value); root = ArtifactRef(root.artifact_id, sha256(raw).hexdigest())
        reader.objects[root.artifact_id] = raw
        reader.metadata[root.artifact_id] = replace(reader.metadata[root.artifact_id], ref=root, bytes=len(raw))
        reader.outputs = {root}
    elif change == 'extra_output':
        reader.outputs.add(ArtifactRef(UUID(uid(90)), 'a'*64))
    elif change == 'params_canonical':
        raw = reader.objects[params.artifact_id] + b'\n'
        params = ArtifactRef(params.artifact_id, sha256(raw).hexdigest())
        reader.objects[params.artifact_id] = raw
        reader.metadata[params.artifact_id] = replace(reader.metadata[params.artifact_id], ref=params, bytes=len(raw))
    elif change == 'frozen_evidence':
        key = UUID(uid(6)); reader.metadata[key] = replace(reader.metadata[key], source='output')
    with pytest.raises(AdoptionInvalid):
        verify_delivery_preparation(reader, params, root)


def test_cancellation_stops_before_object_access():
    reader, params, root = adoption_fixture()
    class Cancelled(Exception): pass
    def cancelled(): raise Cancelled()
    with pytest.raises(Cancelled):
        verify_delivery_preparation(reader, params, root, check_active=cancelled)
