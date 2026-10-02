"""Typed V2 closure preparation rejects forged and unreachable outputs."""
from copy import deepcopy
from hashlib import sha256
import json
from pathlib import Path
from uuid import uuid4

import pytest
from psycopg import errors

from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.adoption import (
    AdoptionInvalid, ArtifactEvidence, ArtifactRef, prepare_v2_adoption,
)
from photo_worker.v2.adoption_store import PostgresObjectEvidenceReader
from photo_worker.v2.adoption_service import AdoptionCoordinator


ROOT = Path(__file__).resolve().parents[2]
FIXTURES = json.loads((ROOT / "docs/implementation-v2/contracts/structural-fixtures.json").read_text())["fixtures"]


class Reader:
    def __init__(self):
        self.rows = {}
        self.body = {}
        self.uploads = set()

    def add(self, ref, *, body=None, role=None, source="output", media="application/json"):
        raw = canonical_bytes(body) if body is not None else None
        self.rows[ref] = ArtifactEvidence(ref, media, len(raw) if raw is not None else 32, role, source)
        if raw is not None:
            self.body[ref] = raw
        if source == "output":
            self.uploads.add(ref)

    def evidence(self, ref):
        if ref not in self.rows:
            raise AdoptionInvalid("UNDECLARED_ARTIFACT")
        return self.rows[ref]

    def read_json(self, ref, max_bytes):
        return self.body[ref]

    def staged_outputs(self):
        return set(self.uploads)


def scene_case():
    reader = Reader()
    source = ArtifactRef(uuid4(), "a" * 64)
    reader.add(source, source="input", media="image/tiff")
    correction = deepcopy(FIXTURES["CorrectionPlan"])
    correction["items"][0]["binding"]["source"] = {
        "artifact_id": str(source.artifact_id), "sha256": source.sha256}
    correction_ref = ArtifactRef(uuid4(), sha256(canonical_bytes(correction)).hexdigest())
    reader.add(correction_ref, body=correction, role="correction_plan")
    scene = deepcopy(FIXTURES["ScenePlan"])
    scene["groups"][0]["corrections"] = {
        "artifact_id": str(correction_ref.artifact_id), "sha256": correction_ref.sha256}
    root = ArtifactRef(uuid4(), sha256(canonical_bytes(scene)).hexdigest())
    reader.add(root, body=scene, role="document")
    return reader, root, correction_ref


def test_session_describe_face_closure_uses_declared_source():
    reader = Reader()
    source = ArtifactRef(uuid4(), "a" * 64)
    reader.add(source, source="input", media="image/tiff")
    faces = deepcopy(FIXTURES["FaceObservations"])
    faces["binding"]["source"] = {"artifact_id": str(source.artifact_id), "sha256": source.sha256}
    faces_ref = ArtifactRef(uuid4(), sha256(canonical_bytes(faces)).hexdigest())
    reader.add(faces_ref, body=faces, role="faces")
    shard = deepcopy(FIXTURES["DescriptorShard"])
    shard["frames"][0]["binding"]["source"] = {
        "artifact_id": str(source.artifact_id), "sha256": source.sha256}
    shard["frames"][0]["faces"] = {"artifact_id": str(faces_ref.artifact_id),
                                   "sha256": faces_ref.sha256}
    root = ArtifactRef(uuid4(), sha256(canonical_bytes(shard)).hexdigest())
    reader.add(root, body=shard, role="document")
    prepared = prepare_v2_adoption(task_id=uuid4(), operation="session.describe", version="1",
                                   root=root, reader=reader)
    assert [row["artifact_id"] for row in prepared.inventory] == [
        str(root.artifact_id), str(source.artifact_id), str(faces_ref.artifact_id)]


def prepare(reader, root):
    return prepare_v2_adoption(task_id=uuid4(), operation="session.plan", version="1",
                               root=root, reader=reader)


def test_session_plan_closure_exact_hash_roles_and_canonical_inventory():
    reader, root, correction = scene_case()
    task = uuid4()
    result = prepare_v2_adoption(task_id=task, operation="session.plan", version="1",
                                 root=root, reader=reader)
    assert len(result.inventory) == 3
    assert result.inventory[0]["artifact_id"] == str(root.artifact_id)
    assert result.inventory[1]["artifact_id"] == str(correction.artifact_id)
    assert result.inventory[2]["ordinal"] == 2
    assert result.digest == sha256(canonical_bytes(list(result.inventory))).hexdigest()
    assert result.inventory[0]["purpose"] == sha256(f"tasks/{task}/$".encode()).hexdigest()


def test_forged_nested_ref_and_unreachable_staged_output_are_rejected():
    reader, root, correction = scene_case()
    del reader.rows[correction]
    with pytest.raises(AdoptionInvalid, match="UNDECLARED_ARTIFACT"):
        prepare(reader, root)
    reader, root, _ = scene_case()
    extra = ArtifactRef(uuid4(), "b" * 64)
    reader.add(extra, body={"unused": True}, role="correction_plan")
    with pytest.raises(AdoptionInvalid, match="STAGED_OUTPUT_CLOSURE_MISMATCH"):
        prepare(reader, root)


def test_tampered_document_and_wrong_role_are_rejected():
    reader, root, _ = scene_case()
    reader.body[root] += b" "
    with pytest.raises(AdoptionInvalid, match="ARTIFACT_BYTES_MISMATCH"):
        prepare(reader, root)
    reader, root, correction = scene_case()
    evidence = reader.rows[correction]
    reader.rows[correction] = ArtifactEvidence(correction, evidence.media_type,
                                               evidence.bytes, "document", "output")
    with pytest.raises(AdoptionInvalid, match="OUTPUT_ROLE_MISMATCH"):
        prepare(reader, root)


def test_schema_invalid_root_is_rejected_after_hash_verification():
    reader, root, _ = scene_case()
    malformed = {"schema_version": 1, "groups": []}
    raw = canonical_bytes(malformed)
    replacement = ArtifactRef(root.artifact_id, sha256(raw).hexdigest())
    reader.rows[replacement] = ArtifactEvidence(replacement, "application/json", len(raw),
                                                 "document", "output")
    reader.body[replacement] = raw
    reader.uploads.remove(root)
    reader.uploads.add(replacement)
    with pytest.raises(AdoptionInvalid, match="ARTIFACT_SCHEMA_INVALID"):
        prepare(reader, replacement)


def test_evidence_adapter_pins_object_version_and_rejects_pending_upload():
    class Cursor:
        def __init__(self, row):
            self.row = row

        def fetchone(self):
            return self.row

        def fetchall(self):
            return self.row

    class Connection:
        autocommit = True
        state = "ready"

        def execute(self, query, params):
            if "v2_adoption_artifact" in query:
                return Cursor(("bucket", "key", "immutable-v7", digest, len(body),
                               "application/json", "document", "output"))
            return Cursor([(ref.artifact_id, ref.sha256, self.state)])

    class Response:
        def __init__(self):
            self.remaining = body

        def read(self, count):
            count = min(count, 3)
            part, self.remaining = self.remaining[:count], self.remaining[count:]
            return part

        def close(self):
            pass

    class Objects:
        def get_object(self, bucket_name, object_name, *, version_id):
            assert (bucket_name, object_name, version_id) == ("bucket", "key", "immutable-v7")
            return Response()

    body = b'{"schema_version":1}'
    digest = sha256(body).hexdigest()
    ref = ArtifactRef(uuid4(), digest)
    evidence = PostgresObjectEvidenceReader(Connection(), Objects(), tenant_id=uuid4(),
                                            task_id=uuid4(), attempt=1, fence=1,
                                            allowed_bucket="bucket")
    assert evidence.evidence(ref).source == "output"
    assert evidence.read_json(ref, 100) == body
    assert evidence.staged_outputs() == {ref}
    evidence.connection.state = "pending"
    with pytest.raises(AdoptionInvalid, match="STAGED_OUTPUT_INCOMPLETE"):
        evidence.staged_outputs()
    connection = Connection()
    connection.autocommit = False
    with pytest.raises(ValueError, match="autocommit"):
        PostgresObjectEvidenceReader(connection, Objects(), tenant_id=uuid4(),
                                     task_id=uuid4(), attempt=1, fence=1,
                                     allowed_bucket="bucket")


def test_coordinator_finishes_object_validation_before_commit_transaction(monkeypatch):
    reader, root, _ = scene_case()
    marker = {"prepared": False, "inside": False}
    original_staged_outputs = reader.staged_outputs

    def staged_outputs():
        marker["prepared"] = True
        return original_staged_outputs()

    reader.staged_outputs = staged_outputs
    monkeypatch.setattr("photo_worker.v2.adoption_service.PostgresObjectEvidenceReader",
                        lambda *args, **kwargs: reader)
    monkeypatch.setattr("photo_worker.v2.adoption_service.time.sleep", lambda seconds: None)

    class Cursor:
        def __init__(self, row):
            self.row = row

        def fetchone(self):
            return self.row

    class ReadConnection:
        autocommit = True

        def execute(self, query, params):
            return Cursor((1, 1, uuid4(), "session.plan", "1", root.artifact_id, root.sha256))

    class WriteConnection:
        calls = 0

        def transaction(self):
            class Tx:
                def __enter__(self):
                    assert marker["prepared"]
                    marker["inside"] = True

                def __exit__(self, *args):
                    marker["inside"] = False
            return Tx()

        def execute(self, query, params):
            assert marker["inside"] and "adopt_v2_plan" in query
            assert params[5].obj[0]["artifact_id"] == str(root.artifact_id)
            self.calls += 1
            if self.calls == 1:
                raise errors.SerializationFailure("retry")
            return Cursor((True,))

    writer = WriteConnection()
    assert AdoptionCoordinator(ReadConnection(), writer, object(), "bucket").adopt(
        uuid4(), uuid4())
    assert writer.calls == 2
    assert not marker["inside"]


@pytest.mark.parametrize('mode',['require_float32','normalize_uint16',None,'automatic'])
def test_layer_coordinator_uses_generation_bound_sql_alpha_policy(monkeypatch,mode):
    from contextlib import nullcontext
    from types import SimpleNamespace
    from photo_worker.v2.adoption_service import AdoptionContext
    tenant,task,result=uuid4(),uuid4(),uuid4()
    root=ArtifactRef(uuid4(),'a'*64);params_ref=ArtifactRef(uuid4(),'b'*64)
    calls=[]
    class Cursor:
        def __init__(self,row):self.row=row
        def fetchone(self):return self.row
    class Read:
        autocommit=True
        def execute(self,query,args):
            assert args==(tenant,task,3,7,result)
            if 'v25_layer_adoption_params' in query:return Cursor((params_ref.artifact_id,params_ref.sha256))
            assert 'v25_layer_adoption_alpha_policy' in query
            return Cursor((mode,))
    class Write:
        def transaction(self):return nullcontext()
        def execute(self,query,args):
            calls.append('commit');assert 'adopt_v2_plan' in query
            return Cursor((True,))
    monkeypatch.setattr('photo_worker.v2.adoption_service.PostgresObjectEvidenceReader',lambda *args,**kwargs:object())
    def prepare(**kwargs):
        calls.append('prepare')
        assert kwargs['layer_alpha_mode']==mode and kwargs['params_ref']==params_ref
        return SimpleNamespace(inventory=(),digest='c'*64)
    monkeypatch.setattr('photo_worker.v2.adoption_service.prepare_v2_adoption',prepare)
    coordinator=AdoptionCoordinator(Read(),Write(),object(),'bucket')
    monkeypatch.setattr(coordinator,'context',lambda *args:AdoptionContext(3,7,result,'layer.compose','1',root))
    if mode in ('require_float32','normalize_uint16'):
        assert coordinator.adopt(tenant,task) is True
        assert calls==['prepare','commit']
    else:
        with pytest.raises(AdoptionInvalid,match='LAYER_ADOPTION_ALPHA_POLICY_STALE'):coordinator.adopt(tenant,task)
        assert calls==[]
