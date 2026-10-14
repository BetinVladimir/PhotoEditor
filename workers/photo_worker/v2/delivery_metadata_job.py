"""Single scheduler metadata adoption job. SQL establishes all content authority."""
from dataclasses import dataclass
from pathlib import Path
import re
import sys
import time
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes,parse_json
from .delivery_metadata_reader import MetadataResultScope
from .adoption import AdoptionInvalid


_ERROR_CODE = re.compile(r'[A-Z][A-Z0-9_]{0,79}\Z')


@dataclass(frozen=True)
class MetadataAdoptionInstallation:
    scratch_root: Path
    max_bytes: int
    max_pixels: int
    ram_bytes: int
    scratch_bytes: int


def metadata_adoption_installation(getenv):
    root=Path(getenv('PHOTO_METADATA_ADOPTION_SCRATCH_ROOT',''))
    if not root.is_absolute() or root.resolve(strict=True)!=root or not root.is_dir():
        raise ValueError('METADATA_ADOPTION_SCRATCH_INVALID')
    def integer(key,maximum):
        raw=getenv(key,'')
        if not isinstance(raw,str) or not raw.isascii() or not raw.isdigit() or len(raw)>16:
            raise ValueError('METADATA_ADOPTION_RESOURCE_INVALID')
        value=int(raw)
        if not 1<=value<=maximum:raise ValueError('METADATA_ADOPTION_RESOURCE_INVALID')
        return value
    size=integer('PHOTO_METADATA_ADOPTION_MAX_BYTES',2*1024**3)
    pixels=integer('PHOTO_METADATA_ADOPTION_MAX_PIXELS',200000000)
    ram=integer('PHOTO_METADATA_ADOPTION_RAM_BYTES',2**53-1)
    scratch=integer('PHOTO_METADATA_ADOPTION_SCRATCH_BYTES',2**53-1)
    if ram<256*1024**2+pixels*64+size*4 or scratch<size*2+8*1024**2:
        raise ValueError('METADATA_ADOPTION_RESOURCE_UNDERSIZED')
    return MetadataAdoptionInstallation(root,size,pixels,ram,scratch)


def decode_job(raw):
    if not isinstance(raw,bytes) or not 2<=len(raw)<=4096:
        raise ValueError('METADATA_ADOPTION_JOB_INVALID')
    value=parse_json(raw,4096)
    if (not isinstance(value,dict) or set(value)!={'schema_version','tenant_id','task_id','attempt','fence','result_id','timeout_seconds'}
            or canonical_bytes(value)!=raw or type(value['schema_version']) is not int or value['schema_version']!=1
            or type(value['attempt']) is not int or not 1<=value['attempt']<=2147483647
            or type(value['fence']) is not int or not 1<=value['fence']<=2**53-1
            or type(value['timeout_seconds']) is not int or not 1<=value['timeout_seconds']<=600):
        raise ValueError('METADATA_ADOPTION_JOB_INVALID')
    ids=[]
    for field in ('tenant_id','task_id','result_id'):
        if not isinstance(value[field],str):raise ValueError('METADATA_ADOPTION_JOB_INVALID')
        identifier=UUID(value[field])
        if identifier.int==0 or str(identifier)!=value[field]:raise ValueError('METADATA_ADOPTION_JOB_INVALID')
        ids.append(identifier)
    return MetadataResultScope(ids[0],ids[1],value['attempt'],value['fence'],ids[2]),value['timeout_seconds']


def run_job(raw,*,getenv=None):
    import os
    from urllib.parse import urlsplit
    scope,seconds=decode_job(raw)
    getenv=os.environ.get if getenv is None else getenv
    installation=metadata_adoption_installation(getenv)
    from .service import AdopterConfig
    config=AdopterConfig.from_env({key:getenv(key,'') for key in
        ('DATABASE_URL','S3_ENDPOINT','S3_BUCKET','S3_ACCESS_KEY','S3_SECRET_KEY','APP_ENV')})
    import psycopg
    from minio import Minio
    import certifi
    import urllib3
    from .adoption import AdoptionInvalid
    from .delivery_metadata_service import adopt_current_delivery_metadata
    deadline=time.monotonic()+seconds
    def active():
        if time.monotonic()>=deadline:raise TimeoutError('METADATA_ADOPTION_JOB_EXPIRED')
    endpoint=urlsplit(config.endpoint)
    objects=Minio(endpoint.netloc,access_key=config.access_key,secret_key=config.secret_key,
        secure=endpoint.scheme=='https',http_client=urllib3.PoolManager(
            timeout=urllib3.Timeout(connect=5,read=10),retries=False,
            cert_reqs='CERT_REQUIRED',ca_certs=certifi.where()))
    options='-c role=photo_scheduler -c statement_timeout=30000 -c lock_timeout=3000'
    with psycopg.connect(config.database_url,autocommit=True,connect_timeout=5,options=options) as connection:
        active();row=connection.execute('SELECT current_user').fetchone()
        if row is None or row[0]!='photo_scheduler':raise AdoptionInvalid('METADATA_ADOPTION_ROLE_INVALID')
        adopted=adopt_current_delivery_metadata(connection,objects,scope=scope,
            allowed_bucket=config.bucket,scratch_root=installation.scratch_root,
            max_bytes=installation.max_bytes,max_pixels=installation.max_pixels,check_active=active)
    # Commit may have succeeded even if this final deadline check fails. Retry
    # must reconstruct current SQL state; a process exit is never adoption proof.
    active()
    return {'schema_version':1,'adopted':adopted is True}


def main():
    try:
        response=run_job(sys.stdin.buffer.read(4097))
        sys.stdout.buffer.write(canonical_bytes(response))
    except AdoptionInvalid as error:
        code=str(error)
        if _ERROR_CODE.fullmatch(code) is None:
            return 1
        sys.stdout.buffer.write(canonical_bytes({'schema_version':1,'adopted':False,'error_code':code}))
    except Exception:
        # DB/object failures may embed credentials. Do not print job/config/logs.
        return 1
    return 0


if __name__=='__main__':raise SystemExit(main())
