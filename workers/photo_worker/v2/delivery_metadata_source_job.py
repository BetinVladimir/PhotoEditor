"""API-only original metadata preparation subprocess; never publishes objects."""
from pathlib import Path
import sys
import time
from typing import Literal
from uuid import UUID
from pydantic import Field
from photo_worker.contracts.base import Contract,ArtifactRef,canonical_bytes
from photo_worker.contracts.metadata import MetadataDocument
from photo_worker.operations.session.contracts import VersionBinding
from .delivery_metadata_original_reader import MetadataOriginalScope,DeliveryMetadataOriginalReader
from .delivery_metadata_projection import project_delivery_metadata
from .adoption import ArtifactRef as EvidenceRef,AdoptionInvalid
from .delivery_metadata_raw_tool import RAW_METADATA_FILETYPES


class SourceScope(Contract):
    tenant_id: UUID
    project_id: UUID
    actor_id: UUID
    session_id: UUID
    binding: VersionBinding
    original: ArtifactRef
    original_bytes: int=Field(ge=1,le=2*1024**3)
    original_mime: Literal['image/jpeg','image/png','image/tiff',
                           'image/x-adobe-dng','image/x-canon-cr2','image/x-nikon-nef',
                           'image/x-sony-arw','image/x-fuji-raf','image/x-olympus-orf',
                           'image/x-panasonic-rw2']


class SourceJob(Contract):
    schema_version: Literal[1]
    scope: SourceScope
    metadata_id: UUID
    policy: Literal['copyright','approved_exif']
    approved_fields: list[str]=Field(max_length=7)
    timeout_seconds: int=Field(ge=1,le=600)


def decode_job(raw):
    if not isinstance(raw,bytes) or not 2<=len(raw)<=8192:raise ValueError('METADATA_SOURCE_JOB_INVALID')
    job=SourceJob.model_validate_json(raw)
    if canonical_bytes(job)!=raw:raise ValueError('METADATA_SOURCE_JOB_CANONICAL')
    s=job.scope
    scope=MetadataOriginalScope(s.tenant_id,s.project_id,s.binding.asset_version_id,
        EvidenceRef(s.binding.source.artifact_id,s.binding.source.sha256),s.actor_id,s.session_id,
        EvidenceRef(s.original.artifact_id,s.original.sha256))
    if job.metadata_id.int==0 or job.metadata_id in (scope.original.artifact_id,scope.source.artifact_id):
        raise ValueError('METADATA_SOURCE_JOB_ALIAS')
    project_delivery_metadata(MetadataDocument(),job.policy,approved_fields=tuple(job.approved_fields))
    if job.approved_fields!=sorted(job.approved_fields) or job.policy!='approved_exif' and job.approved_fields:
        raise ValueError('METADATA_SOURCE_JOB_APPROVAL')
    return job,scope


def source_installation(getenv):
    root=Path(getenv('PHOTO_METADATA_SOURCE_SCRATCH_ROOT',''))
    if not root.is_absolute() or root.resolve(strict=True)!=root or not root.is_dir():raise ValueError('METADATA_SOURCE_SCRATCH_INVALID')
    def integer(key,maximum):
        raw=getenv(key,'')
        if not isinstance(raw,str) or not raw.isascii() or not raw.isdigit() or len(raw)>16:raise ValueError('METADATA_SOURCE_RESOURCE_INVALID')
        value=int(raw)
        if not 1<=value<=maximum:raise ValueError('METADATA_SOURCE_RESOURCE_INVALID')
        return value
    size=integer('PHOTO_METADATA_SOURCE_MAX_BYTES',2*1024**3)
    ram=integer('PHOTO_METADATA_SOURCE_RAM_BYTES',2**53-1)
    scratch=integer('PHOTO_METADATA_SOURCE_SCRATCH_BYTES',2**53-1)
    if ram<256*1024**2+size*4 or scratch<size+8*1024**2:raise ValueError('METADATA_SOURCE_RESOURCE_UNDERSIZED')
    return root,size


def prepare_job(connection,objects,job,scope,*,bucket,scratch_root,max_bytes,
                raw_executable=None,raw_tool_sha=None,check_active):
    from .delivery_metadata_original_service import prepare_authorized_metadata_original
    from .delivery_metadata_source_publication import compile_metadata_source_publication
    check_active()
    if job.scope.original_bytes>max_bytes:raise AdoptionInvalid('METADATA_SOURCE_JOB_SIZE')
    reader=DeliveryMetadataOriginalReader(connection,objects,scope=scope,allowed_bucket=bucket,check_active=check_active)
    evidence=reader.evidence(scope.original)
    if evidence.bytes!=job.scope.original_bytes or evidence.media_type!=job.scope.original_mime:
        raise AdoptionInvalid('METADATA_SOURCE_JOB_ORIGINAL_CHANGED')
    prepared=prepare_authorized_metadata_original(connection,objects,scope=scope,binding=job.scope.binding,
        metadata_id=job.metadata_id,policy=job.policy,approved_fields=tuple(job.approved_fields),
        allowed_bucket=bucket,scratch_root=scratch_root,max_bytes=max_bytes,
        raw_executable=raw_executable,raw_tool_sha=raw_tool_sha,check_active=check_active)
    # Repeat original authority after extraction and before returning publication.
    if reader.evidence(scope.original)!=evidence:raise AdoptionInvalid('METADATA_SOURCE_JOB_ORIGINAL_CHANGED')
    raw,pin=compile_metadata_source_publication(prepared,job.scope.original_mime,job.policy,
        approved_fields=tuple(job.approved_fields))
    check_active()
    return {'schema_version':1,'publication_jcs':raw.decode(),'publication_sha256':pin}


def run_job(raw,*,getenv=None):
    import os
    from urllib.parse import urlsplit
    job,scope=decode_job(raw)
    getenv=os.environ.get if getenv is None else getenv
    scratch,size=source_installation(getenv)
    if job.scope.original_bytes>size:raise AdoptionInvalid('METADATA_SOURCE_JOB_SIZE')
    raw_executable=getenv('PHOTO_METADATA_SOURCE_EXIFTOOL','') if job.scope.original_mime in RAW_METADATA_FILETYPES else None
    raw_tool_sha=getenv('PHOTO_METADATA_SOURCE_EXIFTOOL_SHA256','') if job.scope.original_mime in RAW_METADATA_FILETYPES else None
    if job.scope.original_mime in RAW_METADATA_FILETYPES and (not raw_executable or not raw_tool_sha):
        raise AdoptionInvalid('METADATA_SOURCE_RAW_TOOL_UNAVAILABLE')
    from .service import AdopterConfig
    config=AdopterConfig.from_env({key:getenv(key,'') for key in ('DATABASE_URL','S3_ENDPOINT','S3_BUCKET','S3_ACCESS_KEY','S3_SECRET_KEY','APP_ENV')})
    import psycopg
    from minio import Minio
    import urllib3
    import certifi
    deadline=time.monotonic()+job.timeout_seconds
    def active():
        if time.monotonic()>=deadline:raise TimeoutError('METADATA_SOURCE_JOB_EXPIRED')
    endpoint=urlsplit(config.endpoint)
    objects=Minio(endpoint.netloc,access_key=config.access_key,secret_key=config.secret_key,secure=endpoint.scheme=='https',
        http_client=urllib3.PoolManager(timeout=urllib3.Timeout(connect=5,read=10),retries=False,
            cert_reqs='CERT_REQUIRED',ca_certs=certifi.where()))
    with psycopg.connect(config.database_url,autocommit=True,connect_timeout=5,
            options='-c role=photo_api -c statement_timeout=30000 -c lock_timeout=3000') as connection:
        active();row=connection.execute('SELECT current_user').fetchone()
        if row is None or row[0]!='photo_api':raise AdoptionInvalid('METADATA_SOURCE_JOB_ROLE')
        return prepare_job(connection,objects,job,scope,bucket=config.bucket,scratch_root=scratch,max_bytes=size,
                           raw_executable=raw_executable,raw_tool_sha=raw_tool_sha,check_active=active)


def main():
    try:sys.stdout.buffer.write(canonical_bytes(run_job(sys.stdin.buffer.read(8193))))
    except Exception:return 1
    return 0


if __name__=='__main__':raise SystemExit(main())
