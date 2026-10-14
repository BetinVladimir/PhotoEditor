"""Native closure membership must authorize nested inputs before inference."""
from hashlib import sha256
from io import BytesIO
from uuid import uuid4
import importlib
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.contract_lock import SCHEMAS_SHA256
from photo_worker.v2.input_closure import InputClosureEntry, InputClosureChunk, InputClosureManifest, V2InputIndex

@pytest.mark.parametrize('operation', ['person_mask', 'face_detect', 'face_landmarks'])
@pytest.mark.parametrize('membership', ['allowed', 'missing', 'wrong_sha', 'legacy'])
@pytest.mark.parametrize('alpha_encoding', ['uint16', 'float32', 'nan', 'above_one', 'negative', 'float64', 'truncated'])
def test_nested_native_input_membership(tmp_path, operation, membership, alpha_encoding):
    fixture = importlib.import_module('test_' + operation + '_adapter')
    a = fixture.Artifacts(tmp_path, alpha=True) if operation == 'person_mask' else fixture.Artifacts(tmp_path)
    if operation == 'face_landmarks':
        path = tmp_path / 'alpha.npy'
        alpha = np.full((100, 200), 65535, np.uint16); alpha[:, :20] = 0
        np.save(path, alpha, allow_pickle=False)
        ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(path.read_bytes()).hexdigest())
        a.paths[ref.artifact_id] = path
        a.image.alpha = ref
        a.image_ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(canonical_bytes(a.image)).hexdigest())
        a.faces.image = a.image_ref
        a.raw['image'] = a.image_ref.model_dump(mode='json')
    if alpha_encoding != 'uint16':
        path = a.paths[a.image.alpha.artifact_id]
        values = np.load(path, allow_pickle=False).astype(np.float32) / 65535
        values[values > 0] = .5
        if alpha_encoding == 'nan': values[-1, -1] = np.nan
        if alpha_encoding == 'above_one': values[-1, -1] = 1.01
        if alpha_encoding == 'negative': values[-1, -1] = -.01
        if alpha_encoding == 'float64': values = values.astype(np.float64)
        np.save(path, values, allow_pickle=False)
        if alpha_encoding == 'truncated': path.write_bytes(path.read_bytes()[:-1])
        a.image.alpha = ArtifactRef(artifact_id=a.image.alpha.artifact_id, sha256=sha256(path.read_bytes()).hexdigest())
        a.image_ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(canonical_bytes(a.image)).hexdigest())
        a.raw['image'] = a.image_ref.model_dump(mode='json')
        if hasattr(a, 'faces'): a.faces.image = a.image_ref
    original_alpha = a.paths[a.image.alpha.artifact_id].read_bytes()
    ctx = fixture.context(a)
    ctx.lease.command.params = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
    ctx.lease.command.inputs[:] = [a.image_ref, a.image.pixels]
    if membership == 'legacy' and hasattr(a, 'faces_ref'): ctx.lease.command.inputs.append(a.faces_ref)
    task = uuid4(); pool = fixture.Pool()
    (tmp_path / 'scratch').mkdir()
    with ScratchSpace(tmp_path / 'scratch', uuid4(), task, 1, 64 * 1024**2, min_free_bytes=0) as scratch:
        a.scratch = scratch; index = None
        if membership != 'legacy':
            refs = [a.image_ref, a.image.pixels]
            if hasattr(a, 'faces_ref'): refs.append(a.faces_ref)
            if membership != 'missing':
                refs.append(a.image.alpha if membership == 'allowed' else ArtifactRef(artifact_id=a.image.alpha.artifact_id, sha256='0' * 64))
            entries = sorted([InputClosureEntry(ref=r, contract='image/tiff') for r in refs], key=lambda e: e.ref.artifact_id.bytes)
            bodies = {}
            def put(value):
                raw = canonical_bytes(value); ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest()); bodies[ref.artifact_id] = raw; return ref
            chunk = put(InputClosureChunk(schema_version=1, entries=entries))
            digest = sha256(canonical_bytes([e.model_dump(mode='json') for e in entries])).hexdigest()
            manifest = put(InputClosureManifest(schema_version=1, task_id=task, entry_count=len(entries), entries_sha=digest, chunks=[chunk]))
            ctx.lease.command.inputs.append(manifest)
            profile = dict(renderer_version='v2.2', contracts_sha=SCHEMAS_SHA256, v2_input_manifest=manifest.model_dump(mode='json'), v2_entry_count=len(entries), v2_entries_sha=digest)
            index = V2InputIndex.build(task_id=task, inputs=ctx.lease.command.inputs, params=ctx.lease.command.params, execution_profile=profile, scratch=scratch, open_stream=lambda r: BytesIO(bodies[r.artifact_id]), check_active=ctx.check_active)
        authority = TaskArtifacts(ctx, None, scratch, index)
        a.require_declared_input = authority.require_declared_input
        try:
            if membership == 'allowed' and alpha_encoding in ('uint16', 'float32'):
                result = fixture.execute(ctx, a, pool, 'cpu' if operation == 'face_detect' else 'cuda')
                assert result.outputs
                if operation == 'person_mask': assert np.all(a.arrays['person_mask'][:, :2] == 0)
                if operation == 'face_detect': assert a.staged[0][1].alpha_excluded_fraction == pytest.approx(2 / 24)
                if operation == 'face_landmarks': assert len(pool.engine.inputs) == 1
            else:
                with pytest.raises(OperationFailure) as exc:
                    fixture.execute(ctx, a, pool, 'cpu' if operation == 'face_detect' else 'cuda')
                assert exc.value.error.code == ('MODEL_OUTPUT_INVALID' if membership == 'allowed' else 'INPUT_INVALID')
                assert not a.staged
                if operation == 'face_landmarks': assert not pool.engine.inputs
                else: assert pool.engine.input is None
            assert a.paths[a.image.alpha.artifact_id].read_bytes() == original_alpha
        finally:
            if index is not None: index.close()
