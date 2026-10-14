import hashlib
import json
from uuid import UUID, uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.delivery.assembly import assemble_signed_delivery
from photo_worker.v2.adoption import (AdoptionInvalid, ArtifactEvidence, ArtifactRef,
                                      prepare_v2_adoption)
from photo_worker.v2.delivery_signed_assembly_adoption import (
    verify_signed_delivery_assembly, verify_signed_delivery_dependency_bytes)
from test_delivery_signed_assembly_matrix import signed_fixture


class SignedReader:
    def __init__(self, documents, root):
        self.documents = documents
        self.root = root
        self.override_source = None

    def evidence(self, ref):
        raw = self.documents[ref.artifact_id]
        source = 'output' if ref == self.root else 'input'
        if self.override_source is not None and ref != self.root:
            source = self.override_source
        return ArtifactEvidence(ref, 'application/json', len(raw),
                                'document' if source == 'output' else None, source)

    def read_json(self, ref, maximum):
        assert len(self.documents[ref.artifact_id]) <= maximum
        return self.documents[ref.artifact_id]

    def staged_outputs(self):
        return {self.root}


def test_signed_assembly_adoption_requires_pinned_inputs_and_exact_result():
    tenant, actor, parent, _, _, _, _, envelope_ref, envelope_raw, documents = signed_fixture()
    params_raw = canonical_bytes({'schema_version': 1,
        'assembly': envelope_ref.model_dump(mode='json')})
    params_ref = ArtifactRef(uuid4(), hashlib.sha256(params_raw).hexdigest())
    expected = assemble_signed_delivery(envelope_ref, envelope_raw,
        lambda ref: documents[ref.artifact_id], tenant, actor, parent)
    result_raw = canonical_bytes(expected)
    root = ArtifactRef(uuid4(), hashlib.sha256(result_raw).hexdigest())
    documents.update({params_ref.artifact_id: params_raw, root.artifact_id: result_raw})
    reader = SignedReader(documents, root)
    verified = verify_signed_delivery_assembly(reader, params_ref, root, tenant, actor, parent)
    assert verified.params_jcs == params_raw and verified.result_jcs == result_raw
    reader.override_source = 'output'
    with pytest.raises(AdoptionInvalid, match='DELIVERY_SIGNED_ASSEMBLY_EVIDENCE_INVALID'):
        verify_signed_delivery_assembly(reader, params_ref, root, tenant, actor, parent)
    reader.override_source = None
    with pytest.raises(AdoptionInvalid):
        verify_signed_delivery_assembly(reader, params_ref, root, uuid4(), actor, parent)
    reader.documents[root.artifact_id] = b'{}'
    with pytest.raises(AdoptionInvalid, match='DELIVERY_SIGNED_ASSEMBLY_BYTES_INVALID'):
        verify_signed_delivery_assembly(reader, params_ref, root, tenant, actor, parent)


def test_generic_adoption_uses_signed_root_only_with_explicit_scope():
    tenant, actor, parent, _, _, _, _, envelope_ref, envelope_raw, documents = signed_fixture()
    params_raw = canonical_bytes({'schema_version': 1,
        'assembly': envelope_ref.model_dump(mode='json')})
    params_ref = ArtifactRef(uuid4(), hashlib.sha256(params_raw).hexdigest())
    signed = assemble_signed_delivery(envelope_ref, envelope_raw,
        lambda ref: documents[ref.artifact_id], tenant, actor, parent)
    result_raw = canonical_bytes(signed)
    root = ArtifactRef(uuid4(), hashlib.sha256(result_raw).hexdigest())
    documents.update({params_ref.artifact_id: params_raw, root.artifact_id: result_raw})
    reader = SignedReader(documents, root)
    task_id = uuid4()
    prepared = prepare_v2_adoption(task_id=task_id, operation='v2.delivery.assemble',
        version='1', root=root, reader=reader, params_ref=params_ref,
        delivery_signed_scope=(tenant, actor, parent))
    assert prepared.inventory[0]['artifact_id'] == str(root.artifact_id)
    with pytest.raises(AdoptionInvalid):
        prepare_v2_adoption(task_id=task_id, operation='v2.delivery.assemble',
            version='1', root=root, reader=reader, params_ref=params_ref)
    with pytest.raises(AdoptionInvalid, match='DELIVERY_SIGNED_ASSEMBLY_SCOPE_INVALID'):
        prepare_v2_adoption(task_id=task_id, operation='v2.delivery.assemble',
            version='1', root=root, reader=reader, params_ref=params_ref,
            delivery_signed_scope=(tenant, actor, UUID(int=0)))


def test_signed_dependency_bytes_require_exact_immutable_inputs():
    *_, documents = signed_fixture()
    closure = json.loads(next(raw for raw in documents.values() if b'"entries"' in raw))
    entry = closure['entries'][0]
    export_bytes, signed_bytes, manifest_bytes = b'export-image', b'signed-image', b'c2pa-manifest'
    entry['export']['sha256'] = hashlib.sha256(export_bytes).hexdigest()
    entry['signed']['sha256'] = hashlib.sha256(signed_bytes).hexdigest()
    entry['manifest']['sha256'] = hashlib.sha256(manifest_bytes).hexdigest()
    raw = canonical_bytes(closure)

    class BinaryReader:
        def __init__(self):
            self.data = {UUID(entry['export']['artifact_id']): export_bytes,
                         UUID(entry['signed']['artifact_id']): signed_bytes,
                         UUID(entry['manifest']['artifact_id']): manifest_bytes}

        def evidence(self, ref):
            media = ('application/c2pa' if ref.artifact_id == UUID(entry['manifest']['artifact_id'])
                     else 'image/jpeg')
            return ArtifactEvidence(ref, media, len(self.data[ref.artifact_id]), None, 'input')

        def iter_bytes(self, ref, maximum):
            yield self.data[ref.artifact_id]

    reader = BinaryReader()
    verify_signed_delivery_dependency_bytes(reader, raw)
    closed = []
    def cancellable_stream(ref, maximum):
        try:
            yield reader.data[ref.artifact_id]
        finally:
            closed.append(ref.artifact_id)
    reader.iter_bytes = cancellable_stream
    checks = []
    def active():
        checks.append(True)
        return len(checks) < 6
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        verify_signed_delivery_dependency_bytes(reader, raw, check_active=active)
    assert len(closed) == 1
    reader.iter_bytes = BinaryReader.iter_bytes.__get__(reader)
    reader.data[UUID(entry['export']['artifact_id'])] = b'substituted'
    with pytest.raises(AdoptionInvalid, match='DELIVERY_SIGNED_DEPENDENCY_HASH_INVALID'):
        verify_signed_delivery_dependency_bytes(reader, raw)
    reader.data[UUID(entry['export']['artifact_id'])] = export_bytes
    reader.data[UUID(entry['signed']['artifact_id'])] = b'substituted'
    with pytest.raises(AdoptionInvalid, match='DELIVERY_SIGNED_DEPENDENCY_HASH_INVALID'):
        verify_signed_delivery_dependency_bytes(reader, raw)
