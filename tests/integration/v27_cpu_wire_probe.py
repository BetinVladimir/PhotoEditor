"""Owned integration subprocess: real Python worker readers against Go mTLS gateway."""
import os
import json
from pathlib import Path
from uuid import UUID,uuid4
import psycopg
from minio import Minio
from photo_worker.contracts.wire import TaskCommand
from photo_worker.runtime.database import Lease,TaskDatabase
from photo_worker.runtime.executor import ExecutionContext
from photo_worker.runtime.gateway import client_tls
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts


from photo_worker.operations.semantic.assembly_installation import sql_semantic_assembly_factory
from photo_worker.operations.semantic.assemble_adapter import execute_assembly
from photo_worker.contracts.semantic import SemanticPlan
from photo_worker.contracts.base import canonical_bytes
from hashlib import sha256


def main(runtime_dsn):
    dsn=os.environ['PHOTO_TEST_DATABASE_URL']
    with psycopg.connect(dsn) as db:
        row=db.execute('SELECT tenant_id,run_id,id,worker_id,attempt,fencing_token,lease_until,deadline,execution_profile,input_digest,inputs,params_artifact_id FROM photo.tasks WHERE id=%s',(UUID(os.environ['PHOTO_TEST_V27_GATEWAY_TASK']),)).fetchone()
        timeout=db.execute("SELECT timeout_seconds FROM photo.operation_registry WHERE operation='v2.semantic.assemble' AND version='1'").fetchone()[0]
        sha=db.execute('SELECT sha256 FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(row[0],row[11])).fetchone()[0]
    command=TaskCommand.model_validate_json(json.dumps(dict(schema_version=1,event_id=str(uuid4()),tenant_id=str(row[0]),run_id=str(row[1]),task_id=str(row[2]),attempt=row[4],operation='v2.semantic.assemble',operation_version='1',resource_class='cpu',correlation_id=str(uuid4()),inputs=row[10],params=dict(artifact_id=str(row[11]),sha256=sha),model=None,timeout_seconds=timeout,traceparent='')))
    lease=Lease(command,row[3],row[5],row[6],row[7],row[8],row[9]);ctx=ExecutionContext(lease)
    directory=Path(os.environ['PHOTO_TEST_V27_CERT_DIR'])
    tls=client_tls(directory/'ca.pem',directory/'client.pem',directory/'client.key')
    scratch=directory/'scratch';scratch.mkdir()
    config=ArtifactConfig(scratch,os.environ['PHOTO_TEST_V27_GATEWAY_ORIGIN'],tls,1024**3,8*1024**2,1024**3)
    database=TaskDatabase(runtime_dsn)
    objects=Minio(os.environ['PHOTO_TEST_MINIO_URL'].removeprefix('http://'),access_key=os.environ['PHOTO_TEST_MINIO_ACCESS'],secret_key=os.environ['PHOTO_TEST_MINIO_SECRET'],secure=False)
    factory=sql_semantic_assembly_factory(database=database,objects=objects,bucket=os.environ['PHOTO_TEST_MINIO_BUCKET'])
    with TaskArtifacts.create(ctx,config,frozenset({'document'})) as artifacts:
        installed=factory(ctx,artifacts)
        result=execute_assembly(ctx,artifacts,load_snapshot=installed.load_snapshot,verify_provenance=installed.verify_provenance)
        assert result.review_required and len(result.outputs)==1
        with psycopg.connect(dsn) as verification:
            stored=verification.execute('SELECT bucket,object_key,object_version,sha256,verified_at,output_role FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(command.tenant_id,result.outputs[0].artifact_id)).fetchone()
        assert stored[4] is not None and stored[5]=='document'
        response=objects.get_object(stored[0],stored[1],version_id=stored[2])
        try:body=response.read()
        finally:response.close();response.release_conn()
        assert sha256(body).hexdigest()==stored[3]==result.outputs[0].sha256
        plan=SemanticPlan.model_validate_json(body)
        assert len(plan.candidates)==1 and plan.candidates[0].state==os.environ.get('PHOTO_TEST_V27_CPU_STATE','unsupported')
    assert not artifacts.scratch.directory.exists()
    payload=canonical_bytes(dict(outputs=[ref.model_dump(mode='json') for ref in result.outputs],metrics=dict(duration_ms=0,peak_memory_bytes=0,bytes_written=result.bytes_written),review_required=True))
    committed=database.commit_success(lease,payload)
    assert committed[0]=='committed' and database.commit_success(lease,payload)==('replayed',*committed[1:])
    with psycopg.connect(dsn,autocommit=True) as read,psycopg.connect(dsn) as write:
        read.execute('SET ROLE photo_scheduler')
        write.execute('SET ROLE photo_scheduler');write.commit()
        from photo_worker.v2.service import Adopter
        coordinator=Adopter(read,write,objects,os.environ['PHOTO_TEST_MINIO_BUCKET']).coordinator
        from photo_worker.v2.adoption import AdoptionInvalid
        with psycopg.connect(dsn) as owner:
            session=owner.execute("SELECT (config_snapshot->'v27_semantic'->>'auth_session_id')::uuid FROM photo.runs WHERE tenant_id=%s AND id=%s",(command.tenant_id,command.run_id)).fetchone()[0]
            owner.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(session,));owner.commit()
        try:
            try:coordinator.adopt(command.tenant_id,command.task_id)
            except AdoptionInvalid as error:
                assert str(error)=='SEMANTIC_SNAPSHOT_SCOPE_STALE'
            else:raise AssertionError('Revoked snapshot session must reject plan')
            with psycopg.connect(dsn) as verification:
                assert verification.execute("SELECT count(*) FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(%s,%s)",(command.tenant_id,command.task_id,committed[1])).fetchone()[0]==0
        finally:
            with psycopg.connect(dsn) as owner:
                owner.execute('UPDATE photo.sessions SET revoked_at=NULL WHERE id=%s',(session,));owner.commit()
        # Deterministic interleaving: all real object preparation has completed
        # when coordinator issues its final SQL. Only inject the session revoke.
        from types import SimpleNamespace
        write_calls=[]
        def revoke_before_commit(statement,args):
            assert 'SELECT photo.adopt_v2_plan' in statement
            write_calls.append(statement)
            with psycopg.connect(dsn) as owner:
                owner.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(session,));owner.commit()
            return write.execute(statement,args)
        coordinator.write_connection=SimpleNamespace(transaction=write.transaction,execute=revoke_before_commit)
        try:
            assert coordinator.adopt(command.tenant_id,command.task_id) is False
            assert len(write_calls)==1
            with psycopg.connect(dsn) as verification:
                assert verification.execute("SELECT count(*) FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(%s,%s)",(command.tenant_id,command.task_id,committed[1])).fetchone()[0]==0
        finally:
            coordinator.write_connection=write
            with psycopg.connect(dsn) as owner:
                owner.execute('UPDATE photo.sessions SET revoked_at=NULL WHERE id=%s',(session,));owner.commit()
        assert coordinator.adopt(command.tenant_id,command.task_id) is True
        assert coordinator.adopt(command.tenant_id,command.task_id) is False
        with psycopg.connect(dsn) as owner:
            policy=owner.execute("SELECT (config_snapshot->'v27_semantic'->'qualification'->>'policy_id')::uuid FROM photo.runs WHERE tenant_id=%s AND id=%s",(command.tenant_id,command.run_id)).fetchone()[0]
            owner.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(policy,));owner.commit()
        try:
            assert read.execute('SELECT photo.v27_semantic_adoption_qualification_ready(%s,%s,%s,%s,%s)',(command.tenant_id,command.task_id,command.attempt,lease.fence,committed[1])).fetchone()[0] is False
            assert coordinator.adopt(command.tenant_id,command.task_id) is False
            with psycopg.connect(dsn) as verification:
                assert verification.execute("SELECT count(*) FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(%s,%s)",(command.tenant_id,command.task_id,committed[1])).fetchone()[0]==1
        finally:
            with psycopg.connect(dsn) as owner:
                owner.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=%s',(policy,));owner.commit()
        assert coordinator.adopt(command.tenant_id,command.task_id) is False
    with psycopg.connect(dsn) as verification:
        assert verification.execute("SELECT count(*) FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id(%s,%s)",(command.tenant_id,command.task_id,committed[1])).fetchone()[0]==1
    print('V27_WORKER_WIRE_OK')


if __name__ == '__main__':
    from wire_runtime_login import run_worker_probe
    run_worker_probe(main, os.environ['PHOTO_TEST_DATABASE_URL'])
