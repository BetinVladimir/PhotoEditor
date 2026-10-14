import subprocess
import pytest
from photo_worker.operations.metadata.process import run_metadata_tool


class Process:
    def __init__(self,waits=1,code=0):self.waits=waits;self.code=code;self.done=False;self.killed=False;self.reaped=0
    def wait(self,timeout=None):
        if timeout is None:self.reaped+=1;self.done=True;return self.code
        if self.waits:self.waits-=1;raise subprocess.TimeoutExpired('tool',timeout)
        self.done=True;return self.code
    def poll(self):return self.code if self.done else None
    def kill(self):self.killed=True;self.done=True


@pytest.mark.parametrize('code',[0,1])
def test_tool_discards_output_and_reaps(code):
    process=Process(code=code);calls=[]
    def spawn(args,**kwargs):calls.append(kwargs);return process
    result=run_metadata_tool(['tool','--','/scratch/file'],lambda:None,popen=spawn)
    assert result.returncode==code and process.reaped==1 and not process.killed
    assert calls[0]['shell'] is False
    assert calls[0]['stdout']==subprocess.DEVNULL and calls[0]['stderr']==subprocess.DEVNULL


def test_cancelled_tool_is_killed_and_reaped():
    process=Process(waits=100);checks=0
    def active():
        nonlocal checks
        checks+=1
        if checks==3:raise RuntimeError('lease lost')
    with pytest.raises(RuntimeError,match='lease lost'):
        run_metadata_tool(['tool'],active,popen=lambda *args,**kwargs:process)
    assert process.killed and process.reaped==1


def test_tool_deadline_kills_and_reaps():
    process=Process(waits=100);ticks=iter([0,0,121])
    with pytest.raises(subprocess.TimeoutExpired):
        run_metadata_tool(['tool'],lambda:None,popen=lambda *args,**kwargs:process,clock=lambda:next(ticks))
    assert process.killed and process.reaped==1


def test_cancelled_before_spawn_does_not_start_writer():
    def active():raise RuntimeError('cancelled')
    def spawn(*args,**kwargs):raise AssertionError('spawned')
    with pytest.raises(RuntimeError,match='cancelled'):run_metadata_tool(['tool'],active,popen=spawn)
