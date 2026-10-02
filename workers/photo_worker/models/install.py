from __future__ import annotations
import hashlib
import os
from pathlib import Path
import shutil
import tempfile
import urllib.request
from .lock import ModelLock,ModelLockEntry,safe_url

class ModelError(ValueError):
    pass

class CheckedRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,req,fp,code,msg,headers,newurl):
        safe_url(newurl)
        return super().redirect_request(req,fp,code,msg,headers,newurl)


def file_hash(path:Path) -> str:
    digest=hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda:stream.read(1048576),b''):digest.update(chunk)
    return digest.hexdigest()


def verify_model(root:Path,entry:ModelLockEntry) -> dict[str,Path]:
    if root.is_symlink():raise ModelError('MODEL_PATH_INVALID')
    root=root.resolve();files={}
    for item in entry.files:
        candidate=root/item.path;path=candidate.resolve()
        if not path.is_relative_to(root) or candidate.is_symlink() or not path.is_file():raise ModelError('MODEL_UNAVAILABLE')
        if path.stat().st_size!=item.bytes or file_hash(path)!=item.sha256:raise ModelError('MODEL_HASH_MISMATCH')
        files[item.path]=path
    return files


def install_models(lock:ModelLock,selected_ids:list[str],root:Path,offline_bundle:Path|None=None) -> dict[str,Path]:
    if len(set(selected_ids))!=len(selected_ids):raise ModelError('DUPLICATE_MODEL_SELECTION')
    models={m.id:m for m in lock.models}
    if not set(selected_ids)<=models.keys():raise ModelError('MODEL_UNAVAILABLE')
    for mid in selected_ids:
        if not models[mid].approved_for_install:raise ModelError('MODEL_NOT_APPROVED')
    # Re-check nested mutable fields at the trust boundary.
    lock=ModelLock.model_validate(lock.model_dump(mode='python'))
    root.mkdir(parents=True,exist_ok=True);lock_root=root/lock.lock_id;lock_root.mkdir(exist_ok=True)
    if lock_root.is_symlink():raise ModelError('MODEL_PATH_INVALID')
    installed={};pending=[]
    for mid in selected_ids:
        path=lock_root/mid
        if path.exists():verify_model(path,models[mid]);installed[mid]=path
        else:pending.append(mid)
    required=sum(f.bytes for mid in pending for f in models[mid].files)
    space=os.statvfs(root)
    if space.f_bavail*space.f_frsize<required+1073741824:raise ModelError('MODEL_DISK_EXHAUSTED')
    opener=urllib.request.build_opener(CheckedRedirect())
    bundle=offline_bundle.resolve() if offline_bundle else None
    for mid in pending:
        staging=Path(tempfile.mkdtemp(prefix='.model-',dir=lock_root))
        try:
            for item in models[mid].files:
                target=staging/item.path;target.parent.mkdir(parents=True,exist_ok=True)
                if bundle is not None:
                    original=bundle/mid/item.path;source=original.resolve()
                    if not source.is_relative_to(bundle) or original.is_symlink() or not source.is_file():raise ModelError('MODEL_BUNDLE_INVALID')
                    stream=source.open('rb')
                else:
                    request=urllib.request.Request(safe_url(item.url),headers={'User-Agent':'PhotoEditor-model-installer/1'})
                    stream=opener.open(request,timeout=60)
                    safe_url(stream.geturl())
                digest=hashlib.sha256();written=0
                with stream,target.open('xb') as output:
                    while chunk:=stream.read(1048576):
                        written+=len(chunk)
                        if written>item.bytes:raise ModelError('MODEL_SIZE_MISMATCH')
                        digest.update(chunk);output.write(chunk)
                    output.flush();os.fsync(output.fileno())
                if written!=item.bytes or digest.hexdigest()!=item.sha256:raise ModelError('MODEL_HASH_MISMATCH')
                target.chmod(0o444)
            verify_model(staging,models[mid])
            final=lock_root/mid
            # A concurrent installer may have published the same immutable model.
            try:os.rename(staging,final)
            except OSError:
                if not final.is_dir():raise
                verify_model(final,models[mid])
            installed[mid]=final
        finally:
            if staging.exists():shutil.rmtree(staging)
    return installed
