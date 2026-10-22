"""Installation-bound host lease shared by every restore operator phase."""
from contextlib import contextmanager
from contextvars import ContextVar
from functools import wraps
from hashlib import sha256
import fcntl
import inspect
import os
from pathlib import Path
import stat
from photo_worker.contracts.base import canonical_bytes
from .backup_host import private_file

_held=ContextVar('restore_operator_host_lease',default=None)


@contextmanager
def restore_operator_lease(settings, *, check_active):
    # Acquire every physical target in deterministic order. Sharing only MinIO
    # (or configuration/history storage) must conflict even with different PG.
    fields=('postgres_target','minio_target','config_target','deletions_target')
    paths=sorted({Path(settings[field]) for field in fields},key=str)
    if len(paths)!=4:raise ValueError('RESTORE_OPERATOR_STORAGE_IDENTITY')
    for path in paths:
        if not path.is_absolute() or path.resolve(strict=True)!=path:
            raise ValueError('RESTORE_OPERATOR_STORAGE_IDENTITY')
        if any(path in other.parents for other in paths):
            raise ValueError('RESTORE_OPERATOR_STORAGE_OVERLAP')
    expected={'schema_version':'restore-operator-installation/v1',
        'project':settings['project'],'postgres_target':settings['postgres_target'],
        'minio_target':settings['minio_target'],'config_target':settings['config_target'],
        'deletions_target':settings['deletions_target']}
    raw=canonical_bytes(expected)
    if sha256(raw).hexdigest()!=settings['operator_state_pin']:raise ValueError('RESTORE_OPERATOR_STATE_PIN')
    roots=[path.parent/('.photo-restore-'+sha256(str(path).encode()).hexdigest()) for path in paths]
    identity=(tuple(map(str,roots)),settings['operator_state_pin'])
    def validate(root,directory,lock):
        info=root.stat(follow_symlinks=False);opened=os.fstat(directory)
        if (not stat.S_ISDIR(info.st_mode) or root.resolve(strict=True)!=root
                or info.st_mode&0o077 or info.st_uid not in (0,os.geteuid())
                or (info.st_dev,info.st_ino)!=(opened.st_dev,opened.st_ino)):
            raise ValueError('RESTORE_OPERATOR_STATE_CHANGED')
        held=os.fstat(lock);named=os.stat('phase.lock',dir_fd=directory,follow_symlinks=False)
        if (not stat.S_ISREG(held.st_mode) or held.st_nlink!=1 or held.st_mode&0o077
                or held.st_uid not in (0,os.geteuid())
                or (held.st_dev,held.st_ino)!=(named.st_dev,named.st_ino)):
            raise ValueError('RESTORE_OPERATOR_LOCK_AUTHORITY')
        if private_file(root/'installation.json',maximum=16384)!=raw:
            raise ValueError('RESTORE_OPERATOR_STATE_PIN')
    nested=_held.get()
    if nested is not None and nested[0]==identity:
        for values in nested[1]:validate(*values)
        if check_active() is False:raise RuntimeError('RESTORE_OPERATOR_INACTIVE')
        yield
        return
    leases=[]
    try:
        for root in roots:
            directory=os.open(root,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
            try:lock=os.open('phase.lock',os.O_CREAT|os.O_RDWR|os.O_NOFOLLOW,0o600,dir_fd=directory)
            except BaseException:os.close(directory);raise
            leases.append((root,directory,lock))
            validate(root,directory,lock)
            fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
            validate(root,directory,lock)
        if check_active() is False:raise RuntimeError('RESTORE_OPERATOR_INACTIVE')
        for values in leases:validate(*values)
        token=_held.set((identity,tuple(leases)))
        try:yield
        finally:_held.reset(token)
    finally:
        # Closing the descriptor releases flock even if explicit unlock fails;
        # always close every acquired descriptor on partial acquisition too.
        cleanup_error=None
        for _,directory,lock in reversed(leases):
            try:fcntl.flock(lock,fcntl.LOCK_UN)
            except OSError as error:cleanup_error=error
            finally:
                try:os.close(lock)
                except OSError as error:cleanup_error=error
                finally:
                    try:os.close(directory)
                    except OSError as error:cleanup_error=error
        if cleanup_error is not None:raise cleanup_error


def restore_operator_locked(settings_key='settings', *, preflight=None):
    def decorate(function):
        signature=inspect.signature(function)
        @wraps(function)
        def run(*args,**kwargs):
            bound=signature.bind(*args,**kwargs)
            if preflight is not None:preflight()
            with restore_operator_lease(bound.arguments[settings_key],check_active=bound.arguments['check_active']):
                return function(*args,**kwargs)
        return run
    return decorate
