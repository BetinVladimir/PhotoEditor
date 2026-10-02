from __future__ import annotations
from pathlib import PurePosixPath
from typing import Annotated,Literal
from urllib.parse import urlsplit
from pydantic import Field,model_validator
from photo_worker.contracts.base import Contract,SHA256,canonical_hash

MODEL_IDS=('lraspp_mobilenet_v3_large','sfd','fan2d','identity512','eye_open','glare_mask','keypointrcnn_resnet50_fpn','dinov2_vits14','segformer_b0_ade','bisenet_face19','u2net','mobilesam','depth_anything_v2_small','scunet_color_real_psnr','realesrgan_x2','realesrgan_x4','big_lama','sd15_inpaint')
DOWNLOAD_HOSTS=frozenset({'download.pytorch.org','huggingface.co','cdn-lfs.huggingface.co','cdn-lfs-us-1.huggingface.co','cas-bridge.xethub.hf.co','github.com','raw.githubusercontent.com','objects.githubusercontent.com','release-assets.githubusercontent.com'})

def safe_url(value:str) -> str:
    u=urlsplit(value)
    if u.scheme!='https' or u.hostname not in DOWNLOAD_HOSTS or u.username or u.password or u.fragment or u.port not in (None,443):
        raise ValueError('MODEL_SOURCE_DENIED')
    return value

class ModelFile(Contract):
    path: Annotated[str,Field(min_length=1,max_length=240)]
    sha256: SHA256
    bytes: Annotated[int,Field(gt=0,le=8589934592)]
    url: str

    @model_validator(mode='after')
    def safe_path(self):
        path=PurePosixPath(self.path)
        if path.is_absolute() or '..' in path.parts or str(path)!=self.path or '\\' in self.path or '\0' in self.path or self.path in ('.',''):
            raise ValueError('MODEL_PATH_INVALID')
        safe_url(self.url)
        return self

class BackendEvidence(Contract):
    status: Literal['unsupported','untested','passed']
    reason: str
    report_sha256: SHA256 | None

    @model_validator(mode='after')
    def require_evidence(self):
        if self.status=='passed' and self.report_sha256 is None:raise ValueError('Backend pass requires report hash')
        return self

class ModelLockEntry(Contract):
    id: str
    architecture: str
    weight_id: str
    repository: str
    revision: Annotated[str,Field(min_length=7,max_length=128)]
    files: Annotated[list[ModelFile],Field(min_length=1,max_length=256)]
    preprocessing_id: str
    packages_lock_hash: SHA256
    code_license: Annotated[str,Field(min_length=1)]
    weights_license: Annotated[str,Field(min_length=1)]
    notices: list[str]
    redistribution_policy: Literal['allowed','restricted','unresolved']
    approved_for_install: bool
    backends: dict[Literal['cpu','cuda','mps'],BackendEvidence]

    @model_validator(mode='after')
    def complete_entry(self):
        if self.id not in MODEL_IDS:raise ValueError('Unknown model ID')
        if len({f.path for f in self.files})!=len(self.files):raise ValueError('Duplicate weight path')
        if set(self.backends)!={'cpu','cuda','mps'}:raise ValueError('All backend states must be explicit')
        if self.approved_for_install and (self.redistribution_policy=='unresolved' or self.code_license.lower()=='unresolved' or self.weights_license.lower()=='unresolved'):
            raise ValueError('Unresolved license cannot be approved')
        return self

class ModelLock(Contract):
    schema_version: Literal[1]
    lock_id: SHA256
    models: list[ModelLockEntry]

    @model_validator(mode='after')
    def hash_lock(self):
        if len({m.id for m in self.models})!=len(self.models):raise ValueError('Duplicate model ID')
        payload=self.model_dump(mode='json',exclude={'lock_id'})
        if canonical_hash(payload)!=self.lock_id:raise ValueError('MODEL_LOCK_HASH_MISMATCH')
        return self
