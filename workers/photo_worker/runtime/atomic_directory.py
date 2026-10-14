"""Small POSIX filesystem primitives for durable operator-owned directories."""
import ctypes
import errno
import os
import stat
import sys


def sync_directory(path):
    descriptor=os.open(path,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
    try:os.fsync(descriptor)
    finally:os.close(descriptor)


def directory_identity(path):
    current=os.lstat(path)
    if not stat.S_ISDIR(current.st_mode):
        raise ValueError('OPERATOR_DIRECTORY_CHANGED')
    return current.st_dev,current.st_ino


def rename_exclusive(root,pending,final):
    """Atomically publish a directory without replacing an existing name."""
    for name in (pending,final):
        if (type(name) is not str or name in ('','.','..')
                or '/' in name or '\\' in name or '\x00' in name):
            raise ValueError('OPERATOR_DIRECTORY_NAME')
    if isinstance(root,int) and (type(root) is not int or root < 3):
        raise ValueError('OPERATOR_DIRECTORY_FD')
    owned=type(root) is not int
    directory=os.open(root,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW) if owned else root
    try:
        before=os.stat(pending,dir_fd=directory,follow_symlinks=False)
        if not stat.S_ISDIR(before.st_mode):
            raise ValueError('OPERATOR_DIRECTORY_SOURCE')
        library=ctypes.CDLL(None,use_errno=True)
        if sys.platform.startswith('linux'):
            rename=getattr(library,'renameat2',None)
            flag=1  # RENAME_NOREPLACE
        elif sys.platform=='darwin':
            rename=getattr(library,'renameatx_np',None)
            flag=4  # RENAME_EXCL
        else:
            rename=None
        if rename is None:
            raise OSError(errno.ENOSYS,'exclusive directory rename unavailable')
        rename.argtypes=[ctypes.c_int,ctypes.c_char_p,ctypes.c_int,ctypes.c_char_p,ctypes.c_uint]
        rename.restype=ctypes.c_int
        if rename(directory,os.fsencode(pending),directory,os.fsencode(final),flag)!=0:
            code=ctypes.get_errno()
            raise OSError(code,os.strerror(code))
        after=os.stat(final,dir_fd=directory,follow_symlinks=False)
        if (not stat.S_ISDIR(after.st_mode)
                or (before.st_dev,before.st_ino)!=(after.st_dev,after.st_ino)):
            raise ValueError('OPERATOR_DIRECTORY_PUBLISHED_CHANGED')
    finally:
        if owned:os.close(directory)
