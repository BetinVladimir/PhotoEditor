"""Actual person adapter, lease, mTLS gateway and immutable outputs.

LRASPP inference and installed GPU identities are deterministic test doubles.
This proves runtime plumbing/content binding, never scientific GPU qualification.
"""
from contextlib import contextmanager
from hashlib import sha256
from io import BytesIO
import json
import os
from pathlib import Path
from types import SimpleNamespace
from uuid import UUID
import numpy as np
from PIL import Image
import psycopg
from minio import Minio
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.person import PersonReport
from photo_worker.contracts.wire import TaskCommand
from photo_worker.runtime.database import Lease, TaskDatabase
from photo_worker.runtime.executor import ExecutionContext
from photo_worker.runtime.gateway import client_tls
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.operations.mask.person import execute
from photo_worker.operations.mask.person_authority import person_runtime_authority


class FixtureEngine:
    def infer(self, value):
        assert value.dtype == np.float32 and np.isfinite(value).all()
        logits = np.full((21, *value.shape[:2]), -20, np.float32)
        logits[15] = 20
        if os.environ.get('PHOTO_TEST_V27_ORIGINAL_PERSON') == '1':
            # Explicit deterministic spatial test double, not real ML evidence.
            logits.fill(-1000)
            logits[0] = 20
            logits[15, value.shape[0]//2:, :] = 40
        return logits


class FixturePool:
    lock = SimpleNamespace(lock_id='c' * 64)
    @contextmanager
    def acquire(self, model_id, backend):
        assert model_id == 'lraspp_mobilenet_v3_large' and backend == 'cuda'
        yield FixtureEngine()


def main(runtime_dsn):
    dsn = os.environ['PHOTO_TEST_DATABASE_URL']
    task = UUID(os.environ['PHOTO_TEST_V27_GATEWAY_TASK'])
    with psycopg.connect(dsn) as db:
        row = db.execute('SELECT worker_id,attempt,fencing_token,lease_until,deadline,execution_profile,input_digest,dispatch_command FROM photo.tasks WHERE id=%s', (task,)).fetchone()
    command = TaskCommand.model_validate_json(json.dumps(row[7]))
    assert command.task_id == task and command.operation == 'mask.person' and command.attempt == row[1]
    lease = Lease(command, row[0], row[2], row[3], row[4], row[5], row[6]); ctx = ExecutionContext(lease)
    directory = Path(os.environ['PHOTO_TEST_V27_CERT_DIR'])
    tls = client_tls(directory/'ca.pem', directory/'client.pem', directory/'client.key')
    scratch = directory/'scratch'; scratch.mkdir()
    config = ArtifactConfig(scratch, os.environ['PHOTO_TEST_V27_GATEWAY_ORIGIN'], tls, 1024**3, 8*1024**2, 1024**3)
    roles = ('person_mask', 'person_mask_png', 'person_mask_ref', 'person_report')
    installed = dict(model_lock_sha='c'*64, runtime_lock_sha='b'*64, device_profile_sha='c'*64, backend='cuda')
    authority = person_runtime_authority(lambda: dict(installed), **installed)
    with TaskArtifacts.create(ctx, config, frozenset(roles)) as artifacts:
        result = execute(ctx, artifacts, FixturePool(), 'cuda', authority)
        assert len(result.outputs) == 4
    assert not artifacts.scratch.directory.exists()
    objects = Minio(os.environ['PHOTO_TEST_MINIO_URL'].removeprefix('http://'), access_key=os.environ['PHOTO_TEST_MINIO_ACCESS'], secret_key=os.environ['PHOTO_TEST_MINIO_SECRET'], secure=False)
    bodies = []
    with psycopg.connect(dsn) as db:
        for ref, role in zip(result.outputs, roles):
            stored = db.execute('SELECT bucket,object_key,object_version,sha256,output_role,verified_at,bytes FROM photo.artifacts WHERE tenant_id=%s AND id=%s', (command.tenant_id, ref.artifact_id)).fetchone()
            assert stored[4] == role and stored[5] is not None and stored[2] not in ('', 'null', None)
            response = objects.get_object(stored[0], stored[1], version_id=stored[2])
            try: body = response.read()
            finally: response.close(); response.release_conn()
            assert len(body) == stored[6] and sha256(body).hexdigest() == stored[3] == ref.sha256
            bodies.append(body)
    pixels = np.load(BytesIO(bodies[0]), allow_pickle=False)
    assert pixels.dtype == np.float32 and pixels.shape == (300,400)
    if os.environ.get('PHOTO_TEST_V27_ORIGINAL_PERSON') == '1':
        assert np.all(pixels[:10,:10] == 0) and pixels[250,200] > .99
    else:
        assert np.all(pixels > .99)
    with Image.open(BytesIO(bodies[1])) as png:
        assert np.array_equal(np.asarray(png), np.rint(pixels*65535).astype(np.uint16))
    mask = MaskRef.model_validate_json(bodies[2]); report = PersonReport.model_validate_json(bodies[3])
    assert mask.artifact == report.mask == result.outputs[0] and mask.selector == 'person' and not mask.protected
    assert report.image == command.inputs[1] or report.image in command.inputs
    assert (0 < report.person_fraction < 1 if os.environ.get('PHOTO_TEST_V27_ORIGINAL_PERSON') == '1' else report.person_fraction == 1)
    assert report.backend == 'cuda' and report.model_lock_id == 'c'*64
    # Exact target-output guard is independently exercised before canonical commit.
    with psycopg.connect(dsn) as db:
        target=db.execute("SELECT r.config_snapshot ? 'v27_target_person_prepare' FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.id=%s",(task,)).fetchone()[0]
        if target:
            from psycopg.types.json import Jsonb
            refs=[ref.model_dump(mode='json') for ref in result.outputs]
            query='SELECT photo.semantic_target_person_result_ready(%s,%s,%s,%s,%s)'
            def ready(outputs,attempt=lease.command.attempt,fence=lease.fence):
                return db.execute(query,(command.tenant_id,task,attempt,fence,Jsonb(outputs))).fetchone()[0]
            assert ready(refs)
            assert not ready(refs,attempt=lease.command.attempt+1)
            assert not ready(refs,fence=lease.fence+1)
            for altered in [refs[::-1],refs[:-1],refs+[refs[0]],[refs[0]]*4]:
                assert not ready(altered)
            assert db.execute('SELECT count(*) FROM photo.task_results WHERE task_id=%s',(task,)).fetchone()==(0,)
    database = TaskDatabase(runtime_dsn)
    payload = canonical_bytes(dict(outputs=[ref.model_dump(mode='json') for ref in result.outputs], metrics=dict(duration_ms=0, peak_memory_bytes=0, bytes_written=result.bytes_written), review_required=result.review_required))
    committed = database.commit_success(lease, payload)
    assert committed[0] == 'committed', committed
    assert database.commit_success(lease, payload) == ('replayed', *committed[1:])
    with psycopg.connect(dsn) as db:
        assert db.execute('SELECT state,canonical_result_id FROM photo.tasks WHERE id=%s', (task,)).fetchone() == ('succeeded', committed[1])
        assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s', (task,)).fetchone() == (0,)
        assert db.execute("SELECT count(*) FROM photo.resource_reservations WHERE task_id=%s AND state='reserved'", (task,)).fetchone() == (0,)
    print('V27_PERSON_ADAPTER_NEW_OUTPUTS_COMMITTED_OK')
    print('V27_WORKER_WIRE_OK')


if __name__ == '__main__':
    from wire_runtime_login import run_worker_probe
    run_worker_probe(main, os.environ['PHOTO_TEST_DATABASE_URL'])
