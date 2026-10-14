"""Explicit CPU assembly startup; upstream operation admission remains SQL-owned."""
import os
import re
from urllib.parse import urlsplit
from .service import WorkerConfigurationError
from .database import TaskDatabase
from photo_worker.operations.semantic.assembly_installation import sql_semantic_assembly_factory


def semantic_assembly_from_environment(config,getenv=os.environ.get):
    enabled=getenv('PHOTO_SEMANTIC_ASSEMBLY_ENABLED','false')
    if enabled not in ('true','false'):
        raise WorkerConfigurationError('Invalid semantic assembly enablement')
    if enabled=='false':return None
    if config.resource_class!='cpu' or config.backend!='cpu':
        raise WorkerConfigurationError('Semantic assembly requires CPU worker')
    endpoint=getenv('S3_ENDPOINT','');bucket=getenv('S3_BUCKET','')
    access=getenv('S3_ACCESS_KEY','');secret=getenv('S3_SECRET_KEY','')
    region=getenv('S3_REGION','')
    try:
        parsed=urlsplit(endpoint)
        if (parsed.scheme not in ('https','http') or not parsed.hostname or parsed.username is not None
                or parsed.password is not None or parsed.path not in ('','/') or parsed.query or parsed.fragment
                or (parsed.scheme=='http' and not config.development)):
            raise ValueError()
        parsed.port
        if (not re.fullmatch(r'[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]',bucket) or '..' in bucket
                or not access or not secret):raise ValueError()
    except (ValueError,TypeError):
        raise WorkerConfigurationError('Invalid semantic assembly object storage configuration') from None
    from minio import Minio
    import certifi
    import urllib3
    # Each blocking request/read is bounded; lease authority is checked between
    # reads. Disable internal retries so the task retry policy owns recovery.
    http=urllib3.PoolManager(timeout=urllib3.Timeout(connect=5,read=10),retries=False,
        cert_reqs='CERT_REQUIRED',ca_certs=certifi.where())
    objects=Minio(parsed.netloc,access_key=access,secret_key=secret,secure=parsed.scheme=='https',region=region or None,http_client=http)
    from .semantic_objects import SemanticObjectReader
    return sql_semantic_assembly_factory(database=TaskDatabase(config.database_url),objects=SemanticObjectReader(objects),bucket=bucket)
