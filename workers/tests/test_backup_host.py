"""Authored source scenarios; no production backup is invoked by these tests."""
from hashlib import sha256
import os
from pathlib import Path
import pytest

from photo_worker.runtime.backup_host import ComposeMaintenance, HostCommands, PinnedTool
from photo_worker.runtime.backup_operator import validate_tree, file_digest


def installation(tmp_path):
    compose=tmp_path/'compose.yaml';compose.write_text('services: {}\n');compose.chmod(0o600)
    tools={name:PinnedTool('/not/executed', '0'*64) for name in ('docker','compose','pgbackrest','age','tar')}
    commands=HostCommands(tools,check_active=lambda:True)
    return ComposeMaintenance(commands,str(compose),sha256(compose.read_bytes()).hexdigest(),'photoeditor')


def test_unknown_service_refuses_before_mutation(tmp_path):
    installed=installation(tmp_path);calls=[]
    def command(*args):
        calls.append(args)
        return b'postgres\nminio\nforeign-writer\n'
    installed.command=command
    with pytest.raises(ValueError,match='UNKNOWN_RUNNING_SERVICE'):installed.pause()
    assert calls==[('ps','--status','running','--services')]


def test_stop_failure_never_compensates_with_start(tmp_path):
    installed=installation(tmp_path);calls=[]
    def command(*args):
        calls.append(args)
        if args[0]=='stop':raise RuntimeError('ambiguous stop')
        return b'postgres\nminio\napi\nscheduler\n'
    installed.command=command
    with pytest.raises(RuntimeError):installed.pause()
    assert not any(args[0]=='start' for args in calls)


def test_operator_revocation_before_command():
    tools={name:PinnedTool('/not/executed','0'*64) for name in ('docker','compose','pgbackrest','age','tar')}
    commands=HostCommands(tools,check_active=lambda:False)
    with pytest.raises(RuntimeError,match='OPERATOR_INACTIVE'):commands.run('docker',['version'])


def test_volume_refuses_links_without_reading_target(tmp_path):
    root=tmp_path/'minio';root.mkdir();(root/'link').symlink_to('/etc/passwd')
    with pytest.raises(ValueError,match='VOLUME_ENTRY'):validate_tree(root,lambda:None)


def test_bounded_archive_digest(tmp_path):
    path=tmp_path/'archive';path.write_bytes(b'12345')
    with pytest.raises(ValueError,match='ARCHIVE_LIMIT'):file_digest(path,lambda:None,4)
    assert file_digest(path,lambda:None,5)==(5,sha256(b'12345').hexdigest())


def test_compose_pin_rechecked_before_each_call(tmp_path):
    installed=installation(tmp_path)
    Path(installed.file).write_text('services: {new: {}}\n')
    with pytest.raises(ValueError,match='COMPOSE_CHANGED'):installed.running()


def test_fast_exited_process_still_checks_stderr_limit(monkeypatch):
    tools={name:PinnedTool('/not/executed','0'*64) for name in ('docker','compose','pgbackrest','age','tar')}
    commands=HostCommands(tools,check_active=lambda:True,maximum_output=4)
    monkeypatch.setattr(PinnedTool,'verify',lambda self:None)
    class AlreadyExited:
        returncode=0
        def __init__(self,*args,**kwargs):
            kwargs['stderr'].write(b'oversized');kwargs['stderr'].flush()
        def poll(self):return 0
    monkeypatch.setattr('photo_worker.runtime.backup_host.subprocess.Popen',AlreadyExited)
    with pytest.raises(ValueError,match='OUTPUT_LIMIT'):commands.run('docker',['version'])


def test_fast_age_output_path_limit_is_checked(monkeypatch,tmp_path):
    tools={name:PinnedTool('/not/executed','0'*64) for name in ('docker','compose','pgbackrest','age','tar')}
    commands=HostCommands(tools,check_active=lambda:True)
    output=tmp_path/'ciphertext'
    monkeypatch.setattr(PinnedTool,'verify',lambda self:None)
    class AlreadyExited:
        returncode=0
        def __init__(self,*args,**kwargs):output.write_bytes(b'oversized')
        def poll(self):return 0
    monkeypatch.setattr('photo_worker.runtime.backup_host.subprocess.Popen',AlreadyExited)
    with pytest.raises(ValueError,match='OUTPUT_LIMIT'):
        commands.run('age',['--encrypt'],output_path=output,maximum_path_bytes=4)
