"""Attempt-scoped scratch ownership and bounded disk reservations."""
from __future__ import annotations
from dataclasses import dataclass, field
import os
from pathlib import Path
import re
import shutil
import stat
from threading import RLock
from uuid import UUID, uuid4

_NAME = re.compile(r'^[a-z][a-z0-9_-]{0,63}$')
_SUFFIXES = {'', '.bin', '.json', '.npy', '.png', '.tif', '.jpg', '.jpeg', '.webp', '.zip', '.tiff', '.nef', '.nrw', '.crw', '.cr2', '.cr3', '.arw', '.srf', '.sr2', '.mrw', '.dng', '.rw2', '.orf', '.raf', '.pef', '.srw', '.3fr', '.fff', '.iiq', '.mos', '.kdc', '.mef', '.x3f'}
_DIRECTORY = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW


class ScratchError(RuntimeError):
    pass


class DiskQuota(ScratchError):
    pass


def _directory(parent: int, name: str) -> int:
    try:
        os.mkdir(name,mode=0o700,dir_fd=parent)
    except FileExistsError:
        pass
    return os.open(name,_DIRECTORY,dir_fd=parent)


@dataclass(eq=False)
class Reservation:
    owner: ScratchSpace
    size: int
    _names: list[str] = field(default_factory=list)
    _released: bool = False

    def path(self, role: str, suffix: str = '') -> Path:
        return self.owner.path(role,self,suffix)

    def planned_path(self, role: str, suffix: str = '') -> Path:
        """Reserve a generated name for an external atomic no-overwrite writer."""
        return self.owner.path(role,self,suffix,create=False)

    def validate(self):
        """Recheck actual logical file sizes after external writers finish."""
        with self.owner._lock:
            self.owner._check(self)
            used=0
            for name in self._names:
                st=os.stat(name,dir_fd=self.owner._fd,follow_symlinks=False)
                if not stat.S_ISREG(st.st_mode) or st.st_nlink!=1:
                    raise ScratchError('Scratch file was replaced')
                used+=st.st_size
            if used>self.size:
                raise DiskQuota('Scratch reservation exceeded')

    def validate_file(self,path:Path):
        with self.owner._lock:
            self.validate()
            if path.parent!=self.owner.directory or path.name not in self._names:
                raise ScratchError('File is not owned by reservation')

    def release(self):
        self.owner._release(self)

    def __enter__(self):
        self.owner._check(self)
        return self

    def __exit__(self,*exc):
        self.release()


class ScratchSpace:
    """Root is operator-owned; callers supply only typed IDs and numeric attempt.

    A pre-existing attempt directory is never adopted or erased. Files are opened
    exclusively relative to held directory descriptors. Reservation release
    deletes its files before returning budget; retaining files after release
    cannot bypass the per-attempt limit. SQL admission remains the global budget.
    """
    def __init__(self, root: Path, tenant: UUID, task: UUID, attempt: int,
                 budget_bytes: int, *, min_free_bytes: int = 64*1024*1024):
        if not isinstance(tenant,UUID) or not isinstance(task,UUID):
            raise ValueError('Scratch identity requires UUIDs')
        if type(attempt) is not int or not 1<=attempt<=2**31-1:
            raise ValueError('Invalid scratch attempt')
        if type(budget_bytes) is not int or budget_bytes<0 or type(min_free_bytes) is not int or min_free_bytes<0:
            raise ValueError('Invalid scratch budget')
        root=Path(root).absolute()
        if root.resolve(strict=True)!=root:
            raise ScratchError('Scratch root must not contain symbolic links')
        if not shutil.rmtree.avoids_symlink_attacks:
            raise ScratchError('Descriptor-safe cleanup is unavailable')
        self._lock=RLock()
        self._closed=False
        self._reserved=0
        self._reservations:set[Reservation]=set()
        self.budget_bytes=budget_bytes
        self.min_free_bytes=min_free_bytes
        self._parent=-1
        self._fd=-1
        self._name=str(attempt)
        root_fd=os.open(root,_DIRECTORY)
        tenant_fd=-1
        created=False
        try:
            tenant_fd=_directory(root_fd,str(tenant))
            self._parent=_directory(tenant_fd,str(task))
            os.mkdir(self._name,mode=0o700,dir_fd=self._parent)
            created=True
            self._fd=os.open(self._name,_DIRECTORY,dir_fd=self._parent)
        except BaseException:
            if created:
                try:os.rmdir(self._name,dir_fd=self._parent)
                except OSError:pass
            if self._parent>=0:os.close(self._parent)
            raise
        finally:
            if tenant_fd>=0:os.close(tenant_fd)
            os.close(root_fd)
        self.directory=root/str(tenant)/str(task)/self._name
        st=os.fstat(self._fd)
        self._identity=(st.st_dev,st.st_ino)

    def _check(self,reservation:Reservation|None=None):
        if self._closed:
            raise ScratchError('Scratch space is closed')
        st=os.stat(self._name,dir_fd=self._parent,follow_symlinks=False)
        if (st.st_dev,st.st_ino)!=self._identity:
            raise ScratchError('Scratch directory was replaced')
        if reservation is not None and (reservation.owner is not self or reservation._released or reservation not in self._reservations):
            raise ScratchError('Inactive scratch reservation')

    def reserve(self,size:int)->Reservation:
        with self._lock:
            self._check()
            if type(size) is not int or size<0:
                raise ValueError('Reservation must be a nonnegative byte count')
            if self._reserved+size>self.budget_bytes:
                raise DiskQuota('Attempt scratch budget exceeded')
            fs=os.fstatvfs(self._fd)
            available=fs.f_bavail*fs.f_frsize
            # Existing reservations may still be sparse/unwritten. Account for
            # their full estimate, conservatively in addition to measured use.
            if available<self.min_free_bytes+self._reserved+size:
                raise DiskQuota('Insufficient scratch disk headroom')
            reservation=Reservation(self,size)
            self._reserved+=size
            self._reservations.add(reservation)
            return reservation

    def path(self,role:str,reservation:Reservation,suffix:str='',*,create:bool=True)->Path:
        if not _NAME.fullmatch(role) or suffix not in _SUFFIXES:
            raise ValueError('Invalid scratch file role or suffix')
        with self._lock:
            self._check(reservation)
            name=f'{role}-{uuid4().hex}{suffix}'
            if create:
                fd=os.open(name,os.O_CREAT|os.O_EXCL|os.O_WRONLY|os.O_NOFOLLOW,0o600,dir_fd=self._fd)
                os.close(fd)
            reservation._names.append(name)
            return self.directory/name

    def open_writer(self,path:Path,reservation:Reservation):
        with self._lock:
            self._check(reservation)
            if path.parent!=self.directory or path.name not in reservation._names:
                raise ScratchError('File is not owned by reservation')
            fd=os.open(path.name,os.O_WRONLY|os.O_NOFOLLOW|os.O_NONBLOCK,dir_fd=self._fd)
            metadata=os.fstat(fd)
            if not stat.S_ISREG(metadata.st_mode) or metadata.st_nlink!=1:
                os.close(fd)
                raise ScratchError('Scratch file was replaced')
            os.ftruncate(fd,0)
            return os.fdopen(fd,'wb')

    def _release(self,reservation:Reservation):
        with self._lock:
            if reservation._released:
                return
            self._check(reservation)
            for name in reservation._names:
                try:os.unlink(name,dir_fd=self._fd)
                except FileNotFoundError:pass
            reservation._released=True
            self._reserved-=reservation.size
            self._reservations.remove(reservation)

    def cleanup(self):
        with self._lock:
            if self._closed:return
            try:
                self._check()
                shutil.rmtree(self._name,dir_fd=self._parent)
            finally:
                self._closed=True
                for reservation in self._reservations:reservation._released=True
                self._reservations.clear()
                os.close(self._fd)
                os.close(self._parent)

    def __enter__(self):return self
    def __exit__(self,*exc):self.cleanup()
