"""Probe cleanup needs positive stopped evidence and a fresh namespace read."""
from contextlib import contextmanager
from types import SimpleNamespace
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import backup_restore_operator as m


class Control:
    def __init__(self):self.statements=[]
    @contextmanager
    def transaction(self):yield
    def execute(self,*args):self.statements.append(args)


class Projection:
    def __init__(self,state=False,new_writer=False,service='rabbitmq'):
        self.state=state;self.new_writer=new_writer;self.service=service;self.stopped=False
    def run(self,tool,args):
        assert tool=='docker'
        if args[0]=='ps':
            if '--all' in args:return b'aaaaaaaaaaaa\n'
            return b'bbbbbbbbbbbb\n' if self.new_writer else b''
        if args[0]=='stop':self.stopped=True;return b''
        if args[0]=='inspect':
            return canonical_bytes([{'Config':{'Labels':{'com.docker.compose.project':'isolated',
                'com.docker.compose.service':self.service}},'State':{'Running':self.state if self.stopped else True}}])
        raise AssertionError('unexpected Docker mutation')


@pytest.mark.parametrize('state,new_writer,service,refused',[
    (False,False,'rabbitmq',False),(None,False,'rabbitmq',True),
    (True,False,'rabbitmq',True),(False,True,'rabbitmq',True),
    (False,False,'unregistered-writer',True)])
def test_cleanup_refuses_unproven_stop_and_always_disables_database(monkeypatch,state,new_writer,service,refused):
    projection=Projection(state,new_writer,service)
    monkeypatch.setattr(m,'HostCommands',lambda *a,**k:projection)
    probe=SimpleNamespace(commands=SimpleNamespace(tools={}),project='isolated')
    connection=Control()
    if refused:
        with pytest.raises(ValueError,match='WRITERS_NOT_STOPPED'):
            m.stop_isolated_probe_phase({'database':'restored'},probe,connection)
    else:m.stop_isolated_probe_phase({'database':'restored'},probe,connection)
    assert len(connection.statements)==2
    assert 'ALLOW_CONNECTIONS false' in str(connection.statements[0][0])
    assert connection.statements[1][1]==('restored',)
