"""Execute the actual CPU defect adapter through the real mTLS gateway."""
import json
import os
from pathlib import Path
from uuid import UUID
from io import BytesIO
from hashlib import sha256
import numpy as np
from PIL import Image
import psycopg
from minio import Minio
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.wire import TaskCommand
from photo_worker.contracts.defects import DefectCandidates, DefectProposeReport
from photo_worker.runtime.database import Lease, TaskDatabase
from photo_worker.runtime.executor import ExecutionContext
from photo_worker.runtime.gateway import client_tls
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.operations.defects.propose import execute


def main():
    dsn = os.environ['PHOTO_TEST_DATABASE_URL']
    task = UUID(os.environ['PHOTO_TEST_V27_GATEWAY_TASK'])
    with psycopg.connect(dsn) as db:
        row = db.execute('SELECT worker_id,attempt,fencing_token,lease_until,deadline,execution_profile,input_digest,dispatch_command FROM photo.tasks WHERE id=%s', (task,)).fetchone()
    command = TaskCommand.model_validate_json(json.dumps(row[7]))
    assert command.task_id == task and command.operation == 'defects.propose' and command.attempt == row[1]
    lease = Lease(command, row[0], row[2], row[3], row[4], row[5], row[6])
    ctx = ExecutionContext(lease)
    directory = Path(os.environ['PHOTO_TEST_V27_CERT_DIR'])
    tls = client_tls(directory/'ca.pem', directory/'client.pem', directory/'client.key')
    scratch = directory/'scratch'; scratch.mkdir()
    config = ArtifactConfig(scratch, os.environ['PHOTO_TEST_V27_GATEWAY_ORIGIN'], tls, 32*1024**3, 8*1024**2, 1024**3)
    roles = ('defect_labels', 'defect_protection', 'defect_labels_png', 'defect_candidates', 'defect_propose_report')
    with TaskArtifacts.create(ctx, config, frozenset(roles)) as artifacts:
        result = execute(ctx, artifacts)
        assert len(result.outputs) == 5
    assert not artifacts.scratch.directory.exists()
    objects = Minio(os.environ['PHOTO_TEST_MINIO_URL'].removeprefix('http://'), access_key=os.environ['PHOTO_TEST_MINIO_ACCESS'], secret_key=os.environ['PHOTO_TEST_MINIO_SECRET'], secure=False)
    bodies = []
    with psycopg.connect(dsn) as db:
        for ref, role in zip(result.outputs, roles):
            stored = db.execute('SELECT bucket,object_key,object_version,sha256,output_role,verified_at FROM photo.artifacts WHERE tenant_id=%s AND id=%s', (command.tenant_id, ref.artifact_id)).fetchone()
            assert stored[4] == role and stored[5] is not None and stored[2] not in ('', 'null', None)
            response = objects.get_object(stored[0], stored[1], version_id=stored[2])
            try: body = response.read()
            finally: response.close(); response.release_conn()
            assert sha256(body).hexdigest() == stored[3] == ref.sha256
            bodies.append(body)
    labels = np.load(BytesIO(bodies[0]), allow_pickle=False)
    protected = np.load(BytesIO(bodies[1]), allow_pickle=False)
    with Image.open(BytesIO(bodies[2])) as png:
        assert np.array_equal(np.asarray(png), labels)
    assert not np.any(labels[protected > 0])
    proposal = DefectCandidates.model_validate_json(bodies[3])
    report = DefectProposeReport.model_validate_json(bodies[4])
    assert proposal.label_map == result.outputs[0]
    assert report.protection.artifact == result.outputs[1]
    assert report.label_png == result.outputs[2] and report.proposal == result.outputs[3]
    with psycopg.connect(dsn) as db:
        target=db.execute("SELECT r.config_snapshot ? 'v27_target_defect_prepare' FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.id=%s",(task,)).fetchone()[0]
        if target:
            from psycopg.types.json import Jsonb
            refs=[ref.model_dump(mode='json') for ref in result.outputs]
            def ready(outputs,attempt=command.attempt,fence=lease.fence):
                return db.execute('SELECT photo.semantic_target_defect_result_ready(%s,%s,%s,%s,%s)',(command.tenant_id,task,attempt,fence,Jsonb(outputs))).fetchone()[0]
            assert ready(refs)
            assert not ready(refs,attempt=command.attempt+1) and not ready(refs,fence=lease.fence+1)
            for altered in [[],refs+refs,list(reversed(refs)),[dict(refs[0],sha256='0'*64)]+refs[1:], [refs[0]]*5]:
                assert not ready(altered)
            assert db.execute('SELECT count(*) FROM photo.task_results WHERE task_id=%s',(task,)).fetchone()==(0,)
    database = TaskDatabase(dsn)
    payload = canonical_bytes(dict(outputs=[ref.model_dump(mode='json') for ref in result.outputs], metrics=dict(duration_ms=0,peak_memory_bytes=0,bytes_written=result.bytes_written),review_required=result.review_required))
    committed = database.commit_success(lease, payload)
    assert committed[0] == 'committed'
    assert database.commit_success(lease,payload) == ('replayed', *committed[1:])
    with psycopg.connect(dsn) as db:
        assert db.execute('SELECT state,canonical_result_id FROM photo.tasks WHERE id=%s',(task,)).fetchone()==('succeeded',committed[1])
        assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s',(task,)).fetchone()==(0,)
        assert db.execute("SELECT count(*) FROM photo.resource_reservations WHERE task_id=%s AND state='reserved'",(task,)).fetchone()==(0,)
    print('V27_WORKER_WIRE_OK')


if __name__ == '__main__':
    main()
