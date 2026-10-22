"""Explicit retouch startup; live qualification remains SQL-owned."""
import os
import re
from urllib.parse import urlsplit
from dataclasses import dataclass
from .service import WorkerConfigurationError
from photo_worker.operations.semantic.preview_adapter import PreviewPresentationPins


@dataclass(frozen=True)
class RetouchPreviewStartup:
    installation_factory: object
    presentation: PreviewPresentationPins

    def __post_init__(self):
        if not callable(self.installation_factory) or not isinstance(self.presentation, PreviewPresentationPins):
            raise WorkerConfigurationError('Invalid retouch preview startup installation')

from .database import TaskDatabase
from photo_worker.operations.semantic.preview_retouch_installation import sql_retouch_preview_factory
from photo_worker.color.icc import ICCRegistry
from photo_worker.v2.semantic_qualification_pin import _sha


def retouch_preview_from_environment(config,getenv=os.environ.get):
    enabled=getenv('PHOTO_RETOUCH_PREVIEW_ENABLED','false')
    if enabled not in ('true','false'):
        raise WorkerConfigurationError('Invalid retouch preview enablement')
    if enabled=='false':return None
    if config.resource_class!='cpu' or config.backend!='cpu':
        raise WorkerConfigurationError('Retouch preview requires CPU worker')
    ram=getenv('PHOTO_RETOUCH_PREVIEW_MAX_RAM_BYTES','')
    if not isinstance(ram,str) or not re.fullmatch(r'[1-9][0-9]{0,15}',ram) or int(ram)>9007199254740991:
        raise WorkerConfigurationError('Retouch preview requires explicit host RAM admission limit')
    measurements={name:getenv('PHOTO_RETOUCH_PREVIEW_'+name.upper(),'') for name in
        ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')}
    if any(not _sha(value) for value in measurements.values()) or config.icc_root is None:
        raise WorkerConfigurationError('Retouch preview requires measured runtime hashes and ICC root')
    try:
        registry=ICCRegistry(config.icc_root)
        from photo_worker.operations.semantic.exposure_color import local_exposure_icc_verifier
        local_exposure_icc_verifier(registry)
    except (OSError,ValueError,TypeError,KeyError):
        raise WorkerConfigurationError('Invalid retouch preview ICC configuration') from None
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
        raise WorkerConfigurationError('Invalid retouch preview object storage configuration') from None
    from .semantic_objects import SemanticObjectReader
    from hashlib import sha256
    from pathlib import Path
    from photo_worker.operations.semantic.preview_adapter import PreviewPresentationPins
    try:
        root=Path(config.icc_root).resolve(strict=True)
        lock=root/'profiles.lock.json'
        with lock.open('rb') as stream: raw=stream.read(65537)
        if not 2<=len(raw)<=65536:raise ValueError()
        pins=PreviewPresentationPins(root,sha256(raw).hexdigest(),registry.resolve('srgb_v1').sha256)
    except (OSError,ValueError,TypeError,KeyError):
        raise WorkerConfigurationError('Invalid retouch preview presentation pins') from None
    from minio import Minio
    import certifi
    import urllib3
    # Each blocking request/read is bounded; lease authority is checked between
    # reads. Disable internal retries so the task retry policy owns recovery.
    http=urllib3.PoolManager(timeout=urllib3.Timeout(connect=5,read=10),retries=False,
        cert_reqs='CERT_REQUIRED',ca_certs=certifi.where())
    objects=Minio(parsed.netloc,access_key=access,secret_key=secret,secure=parsed.scheme=='https',region=region or None,http_client=http)
    factory=sql_retouch_preview_factory(database=TaskDatabase(config.database_url),objects=SemanticObjectReader(objects),bucket=bucket,icc_registry=registry,max_ram_bytes=int(ram),**measurements)
    return RetouchPreviewStartup(factory,pins)
