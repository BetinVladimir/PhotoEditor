"""Actual compensation adapter projections; authored scenarios, not run."""
from types import SimpleNamespace
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import backup_cutover_operator as m


class DockerProjection:
    def __init__(self,*,running=False,project='isolated',service='api'):
        self.running=running;self.project=project;self.service=service;self.calls=[];self.stopped=False
    def run(self,tool,arguments):
        self.calls.append((tool,arguments))
        assert tool=='docker'
        if arguments[0]=='ps':return b'' if self.stopped and self.running is False else b'aaaaaaaaaaaa\n'
        if arguments[0]=='inspect':
            return canonical_bytes([{'Config':{'Labels':{'com.docker.compose.project':self.project,
                'com.docker.compose.service':self.service}},'State':{'Running':self.running if self.stopped else True}}])
        if arguments[0]=='stop':self.stopped=True;return b'aaaaaaaaaaaa\n'
        raise AssertionError('unexpected mutation')


def install(monkeypatch,projection):
    monkeypatch.setattr(m,'HostCommands',lambda *args,**kwargs:projection)
    return SimpleNamespace(tools={})


def test_compensation_stops_exact_id_then_rechecks_running_state(monkeypatch):
    projection=DockerProjection();commands=install(monkeypatch,projection)
    m._stop_approved_target(commands,'isolated')
    assert ('docker',['stop','--time','90','aaaaaaaaaaaa']) in projection.calls
    assert sum(arguments[0]=='inspect' for _,arguments in projection.calls)>=2
    assert not any('down' in arguments or '--volumes' in arguments for _,arguments in projection.calls)


@pytest.mark.parametrize('running',[True,None])
def test_stop_exit_status_does_not_prove_compensation(monkeypatch,running):
    projection=DockerProjection(running=running);commands=install(monkeypatch,projection)
    with pytest.raises(ValueError,match='COMPENSATION_UNCONFIRMED'):
        m._stop_approved_target(commands,'isolated')


def test_compensation_never_stops_other_project(monkeypatch):
    projection=DockerProjection(project='production');commands=install(monkeypatch,projection)
    with pytest.raises(ValueError,match='CLEANUP_SCOPE'):
        m._stop_approved_target(commands,'isolated')
    assert not projection.stopped


def test_unknown_target_service_cannot_count_as_confirmed_stop(monkeypatch):
    projection=DockerProjection(service='unregistered-writer');commands=install(monkeypatch,projection)
    with pytest.raises(ValueError):m._stop_approved_target(commands,'isolated')


def test_new_container_after_stop_prevents_confirmed_compensation(monkeypatch):
    class NewWriter(DockerProjection):
        def run(self,tool,arguments):
            if arguments[0]=='ps' and self.stopped:
                self.calls.append((tool,arguments))
                return b'bbbbbbbbbbbb\n'
            return super().run(tool,arguments)
    projection=NewWriter();commands=install(monkeypatch,projection)
    with pytest.raises(ValueError,match='COMPENSATION_UNCONFIRMED'):
        m._stop_approved_target(commands,'isolated')
    assert projection.stopped
