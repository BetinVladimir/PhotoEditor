"""Pinned host tools and fail-closed Compose maintenance for operator backup.

This module is never imported by an HTTP worker. Commands have fixed argument
vectors, private bounded output, cancellation, and no shell interpretation.
Stopping a service is deliberately not paired with an automatic finally/start:
an ambiguous capture must leave writers stopped for operator investigation.
"""
from dataclasses import dataclass
from hashlib import sha256
import os
from pathlib import Path
import signal
import stat
import subprocess
import tempfile
import time


MUTATORS = frozenset({
    'caddy', 'api', 'object-gateway', 'scheduler', 'v2-adopter', 'publisher',
    'dead-letter-worker', 'collection-worker', 'maintenance', 'mail-worker',
    'worker-media', 'worker-cpu', 'worker-archive', 'worker-gpu',
    'photographer', 'portal',
})


def private_file(path, *, maximum=16 << 20):
    path = Path(path)
    if not path.is_absolute() or path.resolve(strict=True) != path:
        raise ValueError('BACKUP_FILE_PATH')
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(descriptor)
        if (not stat.S_ISREG(info.st_mode) or info.st_nlink != 1
                or info.st_uid not in (0, os.geteuid()) or info.st_mode & 0o022
                or not 0 < info.st_size <= maximum):
            raise ValueError('BACKUP_FILE_AUTHORITY')
        with os.fdopen(descriptor, 'rb', closefd=False) as stream:
            raw = stream.read(maximum + 1)
            after = os.fstat(descriptor)
            current = path.stat(follow_symlinks=False)
            identity = lambda x:(x.st_dev,x.st_ino,x.st_size,x.st_mtime_ns,x.st_ctime_ns)
            if len(raw)!=info.st_size or len(raw)>maximum or identity(info)!=identity(after) or identity(after)!=identity(current):
                raise ValueError('BACKUP_FILE_CHANGED')
            return raw
    finally:
        os.close(descriptor)


@dataclass(frozen=True)
class PinnedTool:
    path: str
    sha256: str

    def verify(self):
        if (len(self.sha256) != 64 or any(c not in '0123456789abcdef' for c in self.sha256)
                or sha256(private_file(self.path, maximum=512 << 20)).hexdigest() != self.sha256
                or not os.access(self.path, os.X_OK)):
            raise ValueError('BACKUP_TOOL_PIN')


class HostCommands:
    def __init__(self, tools, *, check_active, timeout=7200, maximum_output=16 << 20):
        if (not callable(check_active) or type(timeout) is not int or not 1 <= timeout <= 86400
                or type(maximum_output) is not int or not 1 <= maximum_output <= 64 << 20
                or set(tools) != {'docker', 'compose', 'pgbackrest', 'age', 'tar'}
                or any(not isinstance(t, PinnedTool) for t in tools.values())):
            raise ValueError('BACKUP_TOOL_INSTALLATION')
        self.tools, self.check_active = tools, check_active
        self.timeout, self.maximum_output = timeout, maximum_output

    def active(self):
        if self.check_active() is False:
            raise RuntimeError('BACKUP_OPERATOR_INACTIVE')

    def run(self, name, arguments, *, output=None, maximum_file_bytes=None,
            output_path=None, maximum_path_bytes=None):
        self.active()
        tool = self.tools[name]
        tool.verify()
        if (not isinstance(arguments, (tuple, list))
                or any(not isinstance(x, str) or '\x00' in x for x in arguments)):
            raise ValueError('BACKUP_COMMAND_ARGUMENT')
        def check_path(final=False):
            if output_path is None:return
            if type(maximum_path_bytes) is not int or maximum_path_bytes<1:
                raise ValueError('BACKUP_COMMAND_PATH_LIMIT')
            try:info=Path(output_path).lstat()
            except FileNotFoundError:
                if final:raise ValueError('BACKUP_COMMAND_OUTPUT_MISSING')
                return
            if not stat.S_ISREG(info.st_mode) or info.st_nlink!=1 or info.st_size>maximum_path_bytes:
                raise ValueError('BACKUP_COMMAND_OUTPUT_LIMIT')
        # Never include stdout/stderr in raised errors: tool diagnostics may
        # contain credentials or secret configuration paths.
        with tempfile.TemporaryFile() as stderr, tempfile.TemporaryFile() as capture:
            target = output if output is not None else capture
            process = subprocess.Popen([tool.path, *arguments], stdin=subprocess.DEVNULL,
                stdout=target, stderr=stderr, start_new_session=True,
                env={'PATH':'/usr/bin:/bin','HOME':str(Path.home()), 'LC_ALL': 'C',
                    'DOCKER_HOST':'unix:///var/run/docker.sock'})
            started = time.monotonic()
            try:
                while process.poll() is None:
                    self.active()
                    check_path()
                    if time.monotonic() - started > self.timeout:
                        raise TimeoutError('BACKUP_COMMAND_TIMEOUT')
                    if os.fstat(stderr.fileno()).st_size > self.maximum_output:
                        raise ValueError('BACKUP_COMMAND_OUTPUT_LIMIT')
                    if output is None and os.fstat(capture.fileno()).st_size > self.maximum_output:
                        raise ValueError('BACKUP_COMMAND_OUTPUT_LIMIT')
                    if output is not None and maximum_file_bytes is not None and os.fstat(output.fileno()).st_size > maximum_file_bytes:
                        raise ValueError('BACKUP_ARCHIVE_LIMIT')
                    time.sleep(.1)
                self.active()
                check_path(final=True)
                if (os.fstat(stderr.fileno()).st_size > self.maximum_output
                        or output is None and os.fstat(capture.fileno()).st_size > self.maximum_output
                        or output is not None and maximum_file_bytes is not None and os.fstat(output.fileno()).st_size > maximum_file_bytes):
                    raise ValueError('BACKUP_COMMAND_OUTPUT_LIMIT')
                if process.returncode != 0:
                    raise RuntimeError('BACKUP_COMMAND_FAILED')
                if output is None:
                    capture.seek(0)
                    result = capture.read(self.maximum_output + 1)
                    if len(result) > self.maximum_output:
                        raise ValueError('BACKUP_COMMAND_OUTPUT_LIMIT')
                    return result
            finally:
                if process.poll() is None:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait()


class ComposeMaintenance:
    def __init__(self, commands, compose_file, compose_sha256, project, rendered_sha256=None):
        if (not isinstance(commands, HostCommands) or not isinstance(project, str)
                or not 1 <= len(project) <= 63 or any(c not in 'abcdefghijklmnopqrstuvwxyz0123456789_-' for c in project)
                or sha256(private_file(compose_file)).hexdigest() != compose_sha256):
            raise ValueError('BACKUP_COMPOSE_INSTALLATION')
        self.commands, self.file, self.pin, self.project = commands, compose_file, compose_sha256, project
        self.rendered_pin = rendered_sha256
        self.stopped = ()

    def command(self, *arguments):
        if sha256(private_file(self.file)).hexdigest() != self.pin:
            raise ValueError('BACKUP_COMPOSE_CHANGED')
        prefix = ['--project-name', self.project, '--file', self.file]
        if self.rendered_pin is not None:
            from photo_worker.contracts.base import parse_json, canonical_bytes
            raw = self.commands.run('compose', [*prefix,'config','--format','json'])
            value = parse_json(raw,max_bytes=16<<20)
            import re
            if (sha256(canonical_bytes(value)).hexdigest()!=self.rendered_pin
                    or any(not isinstance(s.get('image'),str) or re.fullmatch(r'.+@sha256:[0-9a-f]{64}',s['image']) is None
                        for s in value.get('services',{}).values())):
                raise ValueError('BACKUP_COMPOSE_RENDERED_PIN')
        return self.commands.run('compose', [*prefix,*arguments])

    def running(self):
        data = self.command('ps', '--status', 'running', '--services').decode('utf-8')
        services = data.splitlines()
        if len(services) != len(set(services)) or any(not x for x in services):
            raise ValueError('BACKUP_COMPOSE_PROJECTION')
        return frozenset(services)

    def pause(self):
        before = self.running()
        if before - MUTATORS - {'postgres', 'rabbitmq', 'minio'}:
            raise ValueError('BACKUP_UNKNOWN_RUNNING_SERVICE')
        if not {'postgres', 'minio'}.issubset(before):
            raise ValueError('BACKUP_STORAGE_NOT_RUNNING')
        self.stopped = tuple(sorted(before & MUTATORS))
        if self.stopped:
            self.command('stop', '--timeout', '90', *self.stopped)
        self.assert_paused()

    def assert_paused(self):
        self.commands.active()
        if self.running() - {'postgres', 'rabbitmq', 'minio'}:
            raise ValueError('BACKUP_WRITERS_NOT_PAUSED')

    def stop_objects(self):
        self.assert_paused()
        self.command('stop', '--timeout', '90', 'minio')
        if 'minio' in self.running():
            raise ValueError('BACKUP_OBJECTS_NOT_STOPPED')

    def resume_after_publication(self):
        self.assert_paused()
        self.command('start', 'minio')
        if 'minio' not in self.running():
            raise ValueError('BACKUP_OBJECTS_NOT_RUNNING')
        if self.stopped:
            self.command('start', *self.stopped)
