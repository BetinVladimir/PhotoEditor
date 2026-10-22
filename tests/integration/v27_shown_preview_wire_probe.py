"""Real preview/copy workers over mTLS; imported qualification is not photo quality."""
import json
import os
from pathlib import Path
from uuid import UUID
import psycopg
from minio import Minio
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.wire import TaskCommand
from photo_worker.runtime.database import Lease,TaskDatabase
from photo_worker.runtime.executor import ExecutionContext
from photo_worker.runtime.gateway import client_tls
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts


def main(runtime_dsn):
    admin=os.environ['PHOTO_TEST_DATABASE_URL']
    task=UUID(os.environ['PHOTO_TEST_V27_GATEWAY_TASK'])
    with psycopg.connect(admin) as db:
        row=db.execute('SELECT worker_id,attempt,fencing_token,lease_until,deadline,execution_profile,input_digest,dispatch_command FROM photo.tasks WHERE id=%s',(task,)).fetchone()
    command=TaskCommand.model_validate_json(canonical_bytes(row[7]))
    assert command.task_id==task and command.attempt==row[1]
    lease=Lease(command,row[0],row[2],row[3],row[4],row[5],row[6]);ctx=ExecutionContext(lease)
    directory=Path(os.environ['PHOTO_TEST_V27_CERT_DIR'])
    tls=client_tls(directory/'ca.pem',directory/'client.pem',directory/'client.key')
    scratch=directory/'shown-scratch';scratch.mkdir()
    config=ArtifactConfig(scratch,os.environ['PHOTO_TEST_V27_GATEWAY_ORIGIN'],tls,1024**3,8*1024**2,1024**3)
    database=TaskDatabase(runtime_dsn)
    objects=Minio(os.environ['PHOTO_TEST_MINIO_URL'].removeprefix('http://'),access_key=os.environ['PHOTO_TEST_MINIO_ACCESS'],secret_key=os.environ['PHOTO_TEST_MINIO_SECRET'],secure=False)
    bucket=os.environ['PHOTO_TEST_MINIO_BUCKET']
    from photo_worker.color.icc import ICCRegistry
    icc=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc')
    options=dict(database=database,objects=objects,bucket=bucket,icc_registry=icc,
        **{key:row[5][key] for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')})
    if command.operation=='semantic.preview_local_exposure':
        from photo_worker.operations.semantic.preview_installation import sql_exposure_preview_factory
        from photo_worker.operations.semantic.preview_adapter import execute_preview_task,PreviewPresentationPins
        from hashlib import sha256
        root=Path(__file__).resolve().parents[2]/'resources/icc'
        lock=sha256((root/'profiles.lock.json').read_bytes()).hexdigest()
        srgb=sha256((root/'srgb_v1.icc').read_bytes()).hexdigest()
        factory=sql_exposure_preview_factory(**options,presentation=PreviewPresentationPins(root,lock,srgb))
        execute=execute_preview_task
        roles=frozenset({'pixels','working_image','preview_evidence','before','after','affected','protected','preview_presentation'})
    elif command.operation=='semantic.adopt_shown_exposure':
        from photo_worker.operations.semantic.preview_adopt_installation import sql_shown_exposure_factory
        from photo_worker.operations.semantic.preview_adopt_adapter import execute_shown_exposure_task
        factory=sql_shown_exposure_factory(**options);execute=execute_shown_exposure_task
        roles=frozenset({'pixels','working_image'})
    elif command.operation=='semantic.preview_remove_object':
        from photo_worker.operations.semantic.preview_removal_installation import sql_removal_preview_factory
        from photo_worker.operations.semantic.preview_removal_adapter import execute_removal_preview_task
        from photo_worker.operations.semantic.preview_adapter import PreviewPresentationPins
        from removal_capture_provider import controlled_pool
        from hashlib import sha256
        root=Path(__file__).resolve().parents[2]/'resources/icc'
        pins=PreviewPresentationPins(root,sha256((root/'profiles.lock.json').read_bytes()).hexdigest(),icc.resolve('srgb_v1').sha256)
        if os.environ.get('PHOTO_TEST_V27_REMOVAL_PROVIDER')!='controlled':
            raise AssertionError('installed provider requires separately qualified fixture; no fallback')
        backend=row[5]['backend']
        if backend!='cpu':raise AssertionError('controlled provider is CPU-only')
        pool=controlled_pool(row[5]['model_lock_sha'])
        factory=sql_removal_preview_factory(**options,backend=backend,presentation=pins)
        def execute(ctx,artifacts,installation):
            return execute_removal_preview_task(ctx,artifacts,installation,pool,backend)
        roles=frozenset({'pixels','working_image','preview_evidence','before','after','affected','protected','preview_presentation'})
    elif command.operation=='semantic.adopt_shown_removal':
        from photo_worker.operations.semantic.preview_removal_adopt_installation import sql_shown_removal_factory
        from photo_worker.operations.semantic.preview_removal_adopt_adapter import execute_shown_removal_task
        factory=sql_shown_removal_factory(**options);execute=execute_shown_removal_task
        roles=frozenset({'pixels','working_image'})
    elif command.operation=='semantic.preview_background_cleanup':
        from photo_worker.operations.semantic.preview_background_cleanup_installation import sql_background_cleanup_preview_factory
        from photo_worker.operations.semantic.preview_background_cleanup_adapter import execute_background_cleanup_preview_task
        from photo_worker.operations.semantic.preview_adapter import PreviewPresentationPins
        from removal_capture_provider import controlled_pool
        from hashlib import sha256
        root=Path(__file__).resolve().parents[2]/'resources/icc'
        pins=PreviewPresentationPins(root,sha256((root/'profiles.lock.json').read_bytes()).hexdigest(),icc.resolve('srgb_v1').sha256)
        if os.environ.get('PHOTO_TEST_V27_BACKGROUND_CLEANUP_PROVIDER')!='controlled':
            raise AssertionError('installed provider requires separately qualified fixture; no fallback')
        backend=row[5]['backend']
        if backend!='cpu':raise AssertionError('controlled provider is CPU-only')
        pool=controlled_pool(row[5]['model_lock_sha'])
        factory=sql_background_cleanup_preview_factory(**options,backend=backend,presentation=pins)
        def execute(ctx,artifacts,installation):
            return execute_background_cleanup_preview_task(ctx,artifacts,installation,pool,backend)
        roles=frozenset({'pixels','working_image','preview_evidence','before','after','affected','protected','preview_presentation'})
    elif command.operation=='semantic.adopt_shown_background_cleanup':
        from photo_worker.operations.semantic.preview_background_cleanup_adopt_installation import sql_shown_background_cleanup_factory
        from photo_worker.operations.semantic.preview_background_cleanup_adopt_adapter import execute_shown_background_cleanup_task
        factory=sql_shown_background_cleanup_factory(**options);execute=execute_shown_background_cleanup_task
        roles=frozenset({'pixels','working_image'})
    elif command.operation=='semantic.adopt_shown_retouch_region':
        from photo_worker.operations.semantic.preview_retouch_adopt_installation import sql_shown_retouch_factory
        from photo_worker.operations.semantic.preview_retouch_adopt_adapter import execute_shown_retouch_task
        factory=sql_shown_retouch_factory(**options);execute=execute_shown_retouch_task
        roles=frozenset({'pixels','working_image'})
    else:
        raise AssertionError('unsupported shown wire operation')
    with TaskArtifacts.create(ctx,config,roles) as artifacts:
        result=execute(ctx,artifacts,factory(ctx,artifacts))
        assert len(result.outputs)==1 and result.bytes_written>0
    assert not artifacts.scratch.directory.exists()
    payload=canonical_bytes(dict(outputs=[ref.model_dump(mode='json') for ref in result.outputs],metrics=dict(duration_ms=0,peak_memory_bytes=0,bytes_written=result.bytes_written),review_required=result.review_required))
    committed=database.commit_success(lease,payload)
    assert committed[0]=='committed'
    assert database.commit_success(lease,payload)==('replayed',*committed[1:])
    if command.operation in ('semantic.preview_local_exposure','semantic.preview_remove_object','semantic.preview_background_cleanup'):
        # Preview execution is finalized by the Go scheduler lifecycle, not the
        # shown-copy adopter. The subsequent real API read verifies physical
        # bytes and seals its distinct immutable photographer-facing receipt.
        with psycopg.connect(admin) as read:
            receipt='v27_preapply_preview_receipt' if command.operation=='semantic.preview_local_exposure' else ('v27_removal_preview_receipt' if command.operation=='semantic.preview_remove_object' else 'v27_background_cleanup_preview_receipt')
            assert read.execute('SELECT '+receipt+' IS NULL FROM photo.runs WHERE tenant_id=%s AND id=%s',
                (command.tenant_id,command.run_id)).fetchone()==(True,)
        print('V27_SHOWN_WORKER_WIRE_OK')
        print('V27_WORKER_WIRE_OK')
        return
    from photo_worker.v2.service import Adopter
    from photo_worker.operations.semantic.exposure_color import WORKING_PROFILE_SHA
    adoption=directory/'shown-adoption';adoption.mkdir()
    from shown_scheduler_login import scheduler_login
    with scheduler_login(admin) as scheduler_dsn, psycopg.connect(scheduler_dsn,autocommit=True) as read,psycopg.connect(scheduler_dsn) as write:
        read.execute('SET ROLE photo_scheduler');write.execute('SET ROLE photo_scheduler');write.commit()
        coordinator=Adopter(read,write,objects,bucket,layer_scratch_root=adoption,
            layer_scratch_bytes=1024**3,exposure_working_icc_sha=WORKING_PROFILE_SHA,
            shown_exposure_enabled=command.operation=='semantic.adopt_shown_exposure',
            shown_retouch_enabled=command.operation=='semantic.adopt_shown_retouch_region',
            shown_removal_enabled=command.operation=='semantic.adopt_shown_removal',
            shown_background_cleanup_enabled=command.operation=='semantic.adopt_shown_background_cleanup').coordinator
        assert coordinator.adopt(command.tenant_id,task) is True
        assert coordinator.adopt(command.tenant_id,task) is False
        # Mounted Go scheduler recovery finalizes and settles the shown child.
    print('V27_SHOWN_WORKER_WIRE_OK')
    print('V27_WORKER_WIRE_OK')


if __name__=='__main__':
    from wire_runtime_login import run_worker_probe
    run_worker_probe(main,os.environ['PHOTO_TEST_DATABASE_URL'])
