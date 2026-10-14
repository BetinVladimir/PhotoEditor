"""Single configured replacement adoption job. Capability arrives on stdin only."""
from pathlib import Path
import sys
import time
from urllib.parse import urlsplit
from uuid import UUID

from photo_worker.contracts.base import canonical_bytes,parse_json
from .adoption import AdoptionInvalid,ArtifactRef
from .adoption_service import AdoptionContext
from .perspective_replacement_detection_bridge import ReplacementDetectionAdoptionBridge


def decode_job(raw):
    if not isinstance(raw,bytes) or not 2<=len(raw)<=4*1024*1024:
        raise ValueError('REPLACEMENT_ADOPTION_JOB_INVALID')
    value=parse_json(raw,4*1024*1024)
    if (not isinstance(value,dict) or set(value)!={'schema_version','socket','token','tenant_id','run_id','task_id',
            'descriptor_jcs','attempt','fence','result_id','root','params','timeout_seconds'}
            or canonical_bytes(value)!=raw or type(value['schema_version']) is not int or value['schema_version']!=1
            or type(value['attempt']) is not int or not 1<=value['attempt']<=6
            or type(value['fence']) is not int or not 1<=value['fence']<=2**53-1
            or type(value['timeout_seconds']) is not int or not 1<=value['timeout_seconds']<=600):
        raise ValueError('REPLACEMENT_ADOPTION_JOB_INVALID')
    for key in ('tenant_id','run_id','task_id','result_id'):
        if UUID(value[key]).int==0:raise ValueError('REPLACEMENT_ADOPTION_JOB_INVALID')
    descriptor=value['descriptor_jcs'].encode()
    if not 2<=len(descriptor)<=3*1024*1024:raise ValueError('REPLACEMENT_ADOPTION_JOB_INVALID')
    d=parse_json(descriptor,3*1024*1024)
    if (canonical_bytes(d)!=descriptor or d['tenant_id']!=value['tenant_id'] or d['run_id']!=value['run_id']
            or d['task_id']!=value['task_id'] or d['kind']!='v29_perspective_replacement_detection_v1'):
        raise ValueError('REPLACEMENT_ADOPTION_JOB_INVALID')
    ArtifactRef.parse(value['root']);ArtifactRef.parse(value['params'])
    return value


def run_job(raw,*,getenv=None):
    import os
    from .service import AdopterConfig
    from .perspective_lines_adopter_config import perspective_lines_adopter_from_environment
    from .adoption_store import PostgresObjectEvidenceReader
    import psycopg
    from minio import Minio
    import certifi
    import urllib3
    job=decode_job(raw)
    getenv=os.environ.get if getenv is None else getenv
    keys=('DATABASE_URL','S3_ENDPOINT','S3_BUCKET','S3_ACCESS_KEY','S3_SECRET_KEY','APP_ENV')
    config=AdopterConfig.from_env({key:getenv(key,'') for key in keys})
    installation=perspective_lines_adopter_from_environment(getenv)
    if installation is None:raise ValueError('REPLACEMENT_ADOPTION_INSTALLATION_REQUIRED')
    deadline=time.monotonic()+job['timeout_seconds']
    def check_active():
        if time.monotonic()>=deadline:raise TimeoutError('REPLACEMENT_ADOPTION_JOB_EXPIRED')
    tenant,run,task=(UUID(job[key]) for key in ('tenant_id','run_id','task_id'))
    bridge=ReplacementDetectionAdoptionBridge(Path(job['socket']),job['token'],tenant,run,task,check_active=check_active)
    if bridge.was_adopted():return {'schema_version':1,'recovered_existing':True}
    endpoint=urlsplit(config.endpoint)
    objects=Minio(endpoint.netloc,access_key=config.access_key,secret_key=config.secret_key,secure=endpoint.scheme=='https',
        http_client=urllib3.PoolManager(timeout=urllib3.Timeout(connect=5,read=10),retries=False,
            cert_reqs='CERT_REQUIRED',ca_certs=certifi.where()))
    context=AdoptionContext(job['attempt'],job['fence'],UUID(job['result_id']),'optical.perspective_lines','1',ArtifactRef.parse(job['root']))
    options='-c role=photo_scheduler -c statement_timeout=15000 -c lock_timeout=3000'
    with psycopg.connect(config.database_url,autocommit=True,connect_timeout=5,options=options) as connection:
        row=connection.execute('SELECT current_user').fetchone()
        if row is None or row[0]!='photo_scheduler':raise AdoptionInvalid('REPLACEMENT_ADOPTION_ROLE_INVALID')
        reader=PostgresObjectEvidenceReader(connection,objects,tenant_id=tenant,task_id=task,
            attempt=context.attempt,fence=context.fence,allowed_bucket=config.bucket)
        recovered=bridge.replay_and_adopt(reader,context,ArtifactRef.parse(job['params']),job['descriptor_jcs'].encode(),installation=installation)
    check_active()
    return {'schema_version':1,'recovered_existing':recovered}


def main():
    try:
        raw=sys.stdin.buffer.read(4*1024*1024+1)
        response=run_job(raw)
        sys.stdout.buffer.write(canonical_bytes(response))
    except Exception:
        # Never print DB/object-store credentials or job capabilities.
        return 1
    return 0


if __name__=='__main__':
    raise SystemExit(main())
