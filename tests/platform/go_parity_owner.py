"""One repository-confined Go parity lane. Never starts Go itself."""
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path
import fcntl
import os
import shutil
import signal
import subprocess
import time
import stat
import tempfile

MIN_FREE_BYTES = 16 << 30
LOCK_NAME = 'pending-full38-exclusive.lock'
MODULE = 'module photoeditor/backend'
_QUARANTINED_LOCK_FDS = []

class GoParityCleanupIncomplete(RuntimeError):
    pass


def repository_root(anchor):
    anchor = Path(anchor).absolute()
    if anchor != anchor.resolve():
        raise RuntimeError('GO_PARITY_SOURCE_ALIAS')
    for candidate in anchor.parents:
        module = candidate / 'backend/go.mod'
        if (candidate / '.git').exists() and module.is_file():
            if module.read_text().splitlines()[0] != MODULE:
                raise RuntimeError('GO_PARITY_REPOSITORY_MODULE')
            return candidate
    raise RuntimeError('GO_PARITY_REAL_REPOSITORY_REQUIRED')


def _directory(root, path):
    """Open/create owned directories relative to held no-follow descriptors."""
    path = Path(os.path.abspath(path))
    if not path.is_relative_to(root) or path == root:
        raise RuntimeError('GO_PARITY_CACHE_OUTSIDE_REPOSITORY')
    fd = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        for part in path.relative_to(root).parts:
            try:
                os.mkdir(part, 0o700, dir_fd=fd)
            except FileExistsError:
                pass
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
            os.close(fd)
            fd = child
            info = os.fstat(fd)
            if info.st_uid != os.getuid() or info.st_mode & 0o022:
                raise RuntimeError('GO_PARITY_CACHE_OWNER_OR_MODE')
    finally:
        os.close(fd)
    if path != path.resolve():
        raise RuntimeError('GO_PARITY_CACHE_ALIAS')
    return path


def _lock_identity(fd, path):
    actual = os.fstat(fd)
    current = os.lstat(path)
    if (not stat.S_ISREG(actual.st_mode) or not stat.S_ISREG(current.st_mode)
            or actual.st_uid != os.getuid() or current.st_uid != os.getuid()
            or actual.st_nlink != 1 or current.st_nlink != 1
            or actual.st_mode & 0o077
            or (actual.st_dev, actual.st_ino) != (current.st_dev, current.st_ino)):
        raise RuntimeError('GO_PARITY_EXACT_OWNER_LOCK_REQUIRED')


@contextmanager
def _exclusive(root, environ):
    path = root / 'work' / LOCK_NAME
    inherited = environ.get('PHOTO_NATIVE_OWNER_LOCK_FD')
    fd = None
    try:
        if inherited is not None:
            if not inherited.isdecimal():
                raise RuntimeError('GO_PARITY_OWNER_FD_INVALID')
            fd = os.dup(int(inherited))
            _lock_identity(fd, path)
            # An independent descriptor must observe the existing ownership.
            probe = os.open(path, os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK)
            try:
                try:
                    fcntl.flock(probe, fcntl.LOCK_EX | fcntl.LOCK_NB)
                except BlockingIOError:
                    pass
                else:
                    fcntl.flock(probe, fcntl.LOCK_UN)
                    raise RuntimeError('GO_PARITY_INHERITED_LOCK_NOT_HELD')
            finally:
                os.close(probe)
            # Also prove this specific open-file description owns it.
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        else:
            fd = os.open(path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
            _lock_identity(fd, path)
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError as error:
                raise RuntimeError('GO_PARITY_EXCLUSIVE_LANE_BUSY') from error
        try:
            yield
            _lock_identity(fd, path)
        except GoParityCleanupIncomplete:
            # A cleanup failure may not release a lane with unproven descendants.
            _QUARANTINED_LOCK_FDS.append(fd)
            fd = None
            raise
    finally:
        # Never LOCK_UN an inherited shared open-file description.
        if fd is not None:
            os.close(fd)


@dataclass(frozen=True)
class GoParityOwner:
    backend: Path
    temporary: Path
    env: dict
    outer_group: int | None = None

    def run(self, command, *, timeout, extra_env=None):
        """Own one exact session/process group through terminal drain."""
        env = dict(self.env)
        if extra_env:
            if any(not key.startswith('PHOTO_') or key in ('PHOTO_NATIVE_OWNER_LOCK_FD', 'PHOTO_PARITY_OUTER_GROUP') for key in extra_env):
                raise RuntimeError('GO_PARITY_EXTRA_ENV_NOT_TEST_DATA')
            env.update(extra_env)
        if self.outer_group is not None:
            _validate_outer_group(self.outer_group)
        process = subprocess.Popen(command, cwd=self.backend, env=env,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
                                   start_new_session=self.outer_group is None)
        try:
            stdout, stderr = process.communicate(timeout=timeout)
            if self.outer_group is not None:
                return subprocess.CompletedProcess(command, process.returncode, stdout, stderr)
            if _group_exists(process.pid):
                _stop_group(process)
                raise RuntimeError('GO_PARITY_DESCENDANTS_AFTER_PARENT_EXIT')
            return subprocess.CompletedProcess(command, process.returncode, stdout, stderr)
        except BaseException as original:
            if self.outer_group is not None:
                _fatal_outer(self.outer_group)
            try:
                _stop_group(process)
            except BaseException as cleanup:
                raise GoParityCleanupIncomplete('GO_PARITY_PROCESS_GROUP_CLEANUP_UNPROVEN') from cleanup
            raise


def _validate_outer_group(group):
    # go_parity_owner separately attests the exact inherited canonical lock FD.
    if type(group) is not int or group <= 1 or os.getpgrp() != group or os.getsid(0) != group:
        raise RuntimeError('GO_PARITY_EXACT_OUTER_GROUP_REQUIRED')


def _fatal_outer(group):
    _validate_outer_group(group)
    # Never return to pytest with potentially live descendants. The outer owner
    # alone drains this shared group and proves absence before lane release.
    try:
        os.killpg(group, signal.SIGTERM)
    finally:
        os._exit(125)


def _group_exists(pgid):
    try:
        os.killpg(pgid, 0)
        return True
    except ProcessLookupError:
        return False


def _stop_group(process):
    if _group_exists(process.pid):
        try:os.killpg(process.pid, signal.SIGTERM)
        except ProcessLookupError:pass
    try:
        process.communicate(timeout=2)
    except subprocess.TimeoutExpired:
        pass
    if _group_exists(process.pid):
        try:os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:pass
    process.communicate(timeout=2)
    deadline = time.monotonic() + 3
    while _group_exists(process.pid) and time.monotonic() < deadline:
        time.sleep(.01)
    if _group_exists(process.pid):
        raise GoParityCleanupIncomplete('GO_PARITY_PROCESS_GROUP_STILL_PRESENT')


@contextmanager
def go_parity_owner(anchor, extra_env=None):
    root = repository_root(anchor)
    source = Path(anchor).absolute().parents[2]
    if not source.is_relative_to(root) or (source/'backend/go.mod').read_text().splitlines()[0] != MODULE:
        raise RuntimeError('GO_PARITY_SELECTED_SOURCE_REQUIRED')
    # This check precedes all cache creation; no blanket test skip.
    if shutil.disk_usage(root).free < MIN_FREE_BYTES:
        raise RuntimeError('GO_PARITY_DISK_BELOW_16_GIB')
    env = dict(os.environ)
    if extra_env:
        if any(not key.startswith('PHOTO_') or key in ('PHOTO_NATIVE_OWNER_LOCK_FD', 'PHOTO_PARITY_OUTER_GROUP') for key in extra_env):
            raise RuntimeError('GO_PARITY_EXTRA_ENV_NOT_TEST_DATA')
        env.update(extra_env)
    _directory(root, root/'work')
    with _exclusive(root, env):
        outer_group = None
        selected_group = env.get('PHOTO_PARITY_OUTER_GROUP')
        if env.get('PHOTO_NATIVE_OWNER_LOCK_FD') is not None and selected_group is None:
            raise RuntimeError('GO_PARITY_EXACT_OUTER_GROUP_REQUIRED')
        if selected_group is not None:
            if env.get('PHOTO_NATIVE_OWNER_LOCK_FD') is None or not selected_group.isdecimal():
                raise RuntimeError('GO_PARITY_EXACT_OUTER_GROUP_REQUIRED')
            outer_group = int(selected_group)
            _validate_outer_group(outer_group)
        cache = {}
        for key, default in [('GOCACHE','work/go-cache'), ('GOPATH','work/go-path'), ('GOMODCACHE','.cache/go-mod')]:
            requested = Path(env.get(key, str(root/default)))
            if not requested.is_absolute() or '..' in requested.parts:
                raise RuntimeError('GO_PARITY_CACHE_PATH_INVALID')
            cache[key] = str(_directory(root, requested))
        # Ambient external TMPDIR is not a cache approval. Use private repo temp.
        temp_root = _directory(root, root/'work/tmp')
        directory = tempfile.mkdtemp(prefix='go-parity-', dir=temp_root)
        temporary = Path(directory)
        cleanup_proven = True
        try:
            env.update(cache, TMPDIR=directory, GOTMPDIR=directory, GOENV='off', GOWORK='off',
                       GOTOOLCHAIN='local', GOPROXY='off', GOSUMDB='off', GOMAXPROCS='2', GOFLAGS='-p=1')
            env.pop('PHOTO_NATIVE_OWNER_LOCK_FD', None)
            env.pop('PHOTO_PARITY_OUTER_GROUP', None)
            if shutil.disk_usage(root).free < MIN_FREE_BYTES:
                raise RuntimeError('GO_PARITY_DISK_BELOW_16_GIB')
            yield GoParityOwner(source/'backend', temporary, env, outer_group)
        except GoParityCleanupIncomplete:
            cleanup_proven = False
            raise
        finally:
            if cleanup_proven:
                shutil.rmtree(temporary)
