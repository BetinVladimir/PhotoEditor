import copy
import hashlib
import json
from uuid import UUID, uuid4, uuid5

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.operations.delivery.assembly import assemble_delivery, assemble_signed_delivery
from test_delivery_assembly_matrix import matrix_fixture, pin_matrix


def signed_fixture():
    tenant, actor, parent, policy_id = (uuid4() for _ in range(4))
    value, slots = matrix_fixture(items=1, omission=False)
    value['policy'].update(provenance='required', signing_policy_id=str(policy_id),
                           signing_policy_sha='a' * 64)
    assembly_ref, assembly_raw, shards = pin_matrix(value, slots)
    entries = []
    for slot in slots:
        key = str(uuid5(parent, f"{slot['ordinal']}:{slot['preset_id']}"))
        workflow = uuid5(tenant, f'provenance:{actor}:{key}')
        entries.append({'ordinal': slot['ordinal'], 'preset_id': slot['preset_id'],
                        'export_id': slot['export_id'], 'export': slot['export'],
                        'signing_key': key, 'task_id': str(uuid5(workflow, 'provenance.sign:0')),
                        'result_id': str(uuid4()),
                        'signed': {'artifact_id': str(uuid4()), 'sha256': 'b' * 64},
                        'manifest': {'artifact_id': str(uuid4()), 'sha256': 'c' * 64},
                        'validation_sha': 'd' * 64})
    closure_raw = canonical_bytes({'schema_version': 1, 'tenant_id': str(tenant),
        'actor_id': str(actor), 'parent_id': str(parent), 'policy_id': str(policy_id),
        'policy_sha': 'a' * 64, 'entries': entries})
    closure_ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(closure_raw).hexdigest())
    envelope_raw = canonical_bytes({'schema_version': 2,
        'assembly': assembly_ref.model_dump(mode='json'),
        'signing_closure': closure_ref.model_dump(mode='json')})
    envelope_ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(envelope_raw).hexdigest())
    documents = {assembly_ref.artifact_id: assembly_raw, closure_ref.artifact_id: closure_raw, **shards}
    return tenant, actor, parent, slots, entries, assembly_ref, assembly_raw, envelope_ref, envelope_raw, documents


def test_signed_assembly_requires_exact_ready_slot_closure():
    tenant, actor, parent, slots, entries, assembly_ref, assembly_raw, envelope_ref, envelope_raw, documents = signed_fixture()
    manifest = assemble_signed_delivery(envelope_ref, envelope_raw,
        lambda ref: documents[ref.artifact_id], tenant, actor, parent)
    assert manifest.schema_version == 2 and manifest.ready_count == len(slots)
    assert manifest.assembly == assembly_ref and manifest.signing_closure.artifact_id in documents
    with pytest.raises(ValueError, match='DELIVERY_PROVENANCE_UNAVAILABLE'):
        assemble_delivery(assembly_ref, assembly_raw, lambda ref: documents[ref.artifact_id])
    with pytest.raises(ValueError, match='DELIVERY_SIGNED_SCOPE_INVALID'):
        assemble_signed_delivery(envelope_ref, envelope_raw,
            lambda ref: documents[ref.artifact_id], uuid4(), actor, parent)


def test_signed_assembly_rejects_binary_alias_with_a_slot_document():
    tenant, actor, parent, _, _, _, assembly_raw, envelope_ref, envelope_raw, documents = signed_fixture()
    closure = copy.deepcopy(json.loads(next(raw for raw in documents.values() if b'"entries"' in raw)))
    shard_id = json.loads(assembly_raw)['shards'][0]['artifact_id']
    closure['entries'][0]['signed']['artifact_id'] = shard_id
    changed_raw = canonical_bytes(closure)
    envelope = json.loads(envelope_raw)
    envelope['signing_closure']['sha256'] = hashlib.sha256(changed_raw).hexdigest()
    changed_envelope_raw = canonical_bytes(envelope)
    changed_envelope_ref = ArtifactRef(artifact_id=envelope_ref.artifact_id,
        sha256=hashlib.sha256(changed_envelope_raw).hexdigest())
    documents[UUID(envelope['signing_closure']['artifact_id'])] = changed_raw
    with pytest.raises(ValueError, match='DELIVERY_SIGNED_ENVELOPE_ALIAS'):
        assemble_signed_delivery(changed_envelope_ref, changed_envelope_raw,
            lambda ref: documents[ref.artifact_id], tenant, actor, parent)


@pytest.mark.parametrize('change', ['missing', 'reordered', 'other_export', 'other_task', 'extra'])
def test_signed_assembly_rejects_complete_rehashed_substitution(change):
    tenant, actor, parent, slots, entries, _, _, envelope_ref, envelope_raw, documents = signed_fixture()
    closure = copy.deepcopy(json.loads(next(raw for raw in documents.values() if b'"entries"' in raw)))
    if change == 'missing': closure['entries'].pop()
    elif change == 'reordered': closure['entries'].reverse()
    elif change == 'other_export': closure['entries'][0]['export_id'] = str(uuid4())
    elif change == 'other_task': closure['entries'][0]['task_id'] = str(uuid4())
    elif change == 'extra': closure['entries'].append(copy.deepcopy(closure['entries'][-1]))
    changed_raw = canonical_bytes(closure)
    envelope = json.loads(envelope_raw)
    envelope['signing_closure']['sha256'] = hashlib.sha256(changed_raw).hexdigest()
    changed_envelope_raw = canonical_bytes(envelope)
    changed_envelope_ref = ArtifactRef(artifact_id=envelope_ref.artifact_id,
        sha256=hashlib.sha256(changed_envelope_raw).hexdigest())
    documents[UUID(envelope['signing_closure']['artifact_id'])] = changed_raw
    with pytest.raises(ValueError):
        assemble_signed_delivery(changed_envelope_ref, changed_envelope_raw,
            lambda ref: documents[ref.artifact_id], tenant, actor, parent)
