"""Bounded ordinary object transport for leased semantic tasks."""
import re
from urllib.parse import urlsplit


def ordinary_scene_objects(development,getenv):
    endpoint=getenv('S3_ENDPOINT','');bucket=getenv('S3_BUCKET','')
    access=getenv('S3_ACCESS_KEY','');secret=getenv('S3_SECRET_KEY','')
    parsed=urlsplit(endpoint)
    if (parsed.scheme not in ('https','http') or not parsed.hostname or parsed.username is not None
            or parsed.password is not None or parsed.path not in ('','/') or parsed.query or parsed.fragment
            or parsed.scheme=='http' and not development
            or not re.fullmatch(r'[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]',bucket) or '..' in bucket
            or not access or not secret):raise ValueError('SEMANTIC_OBJECT_CONFIGURATION_INVALID')
    parsed.port
    from minio import Minio
    import certifi
    import urllib3
    from .semantic_objects import SemanticObjectReader
    http=urllib3.PoolManager(timeout=urllib3.Timeout(connect=5,read=10),retries=False,
        cert_reqs='CERT_REQUIRED',ca_certs=certifi.where())
    return SemanticObjectReader(Minio(parsed.netloc,access_key=access,secret_key=secret,
        secure=parsed.scheme=='https',region=getenv('S3_REGION','') or None,http_client=http)),bucket
