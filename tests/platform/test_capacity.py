from threading import Event,Thread
from types import SimpleNamespace
from pathlib import Path
import pytest
from photo_worker.runtime.capacity import Capacity,CapacityReporter,measure_cpu


def test_capacity_measurement_keeps_disk_headroom(monkeypatch):
    import photo_worker.runtime.capacity as capacity
    monkeypatch.setattr(capacity.psutil,'virtual_memory',lambda:SimpleNamespace(available=1000))
    monkeypatch.setattr(capacity.shutil,'disk_usage',lambda root:SimpleNamespace(free=100*1024*1024))
    assert measure_cpu(Path('.'))==Capacity(1000,36*1024*1024)
    monkeypatch.setattr(capacity.shutil,'disk_usage',lambda root:SimpleNamespace(free=5))
    assert measure_cpu(Path('.')).scratch_bytes==0
    monkeypatch.setattr(capacity.psutil,'virtual_memory',lambda:SimpleNamespace(available=-1))
    with pytest.raises(ValueError):measure_cpu(Path('.'))


def test_report_failure_cancels_consumer_and_active_task():
    stop=Event();aborted=Event();calls=[]
    def report(sample):calls.append(sample);return False
    reporter=CapacityReporter(report,lambda:Capacity(100,100),aborted.set,interval_seconds=.001)
    thread=Thread(target=reporter.run,args=(stop,));thread.start();thread.join(timeout=1)
    assert not thread.is_alive() and stop.is_set() and aborted.is_set() and reporter.failed.is_set()
    assert calls==[Capacity(100,100)]


def test_shutdown_does_not_publish_another_report():
    stop=Event();stop.set();calls=[]
    reporter=CapacityReporter(lambda sample:calls.append(sample),lambda:Capacity(1,1),lambda:None)
    reporter.run(stop)
    assert not calls and not reporter.failed.is_set()


def test_mps_unified_memory_not_double_counted(monkeypatch):
    import torch
    import photo_worker.runtime.capacity as capacity
    mib=1024*1024
    monkeypatch.setattr(capacity,'measure_cpu',lambda root:Capacity(1024*mib,99))
    monkeypatch.setattr(torch.backends.mps,'is_available',lambda:True)
    monkeypatch.setattr(torch.mps,'recommended_max_memory',lambda:800*mib)
    monkeypatch.setattr(torch.mps,'driver_allocated_memory',lambda:100*mib)
    sample=capacity.measure_accelerator(Path('.'),'mps')
    assert sample.ram_bytes==480*mib and sample.vram_bytes==480*mib and sample.scratch_bytes==99
    monkeypatch.setattr(torch.mps,'driver_allocated_memory',lambda:900*mib)
    assert capacity.measure_accelerator(Path('.'),'mps').vram_bytes==0


def test_cuda_requires_single_device_and_uses_global_free(monkeypatch):
    import torch
    import photo_worker.runtime.capacity as capacity
    mib=1024*1024
    monkeypatch.setattr(capacity,'measure_cpu',lambda root:Capacity(123,456))
    monkeypatch.setattr(torch.cuda,'is_available',lambda:True)
    monkeypatch.setattr(torch.cuda,'device_count',lambda:1)
    monkeypatch.setattr(torch.cuda,'mem_get_info',lambda device:(256*mib,1024*mib))
    assert capacity.measure_accelerator(Path('.'),'cuda')==Capacity(123,456,192*mib)
    monkeypatch.setattr(torch.cuda,'device_count',lambda:2)
    with pytest.raises(ValueError,match='one visible'):capacity.measure_accelerator(Path('.'),'cuda')
    monkeypatch.setattr(torch.cuda,'device_count',lambda:1)
    monkeypatch.setattr(torch.cuda,'mem_get_info',lambda device:(200,100))
    with pytest.raises(ValueError):capacity.measure_accelerator(Path('.'),'cuda')


@pytest.mark.parametrize('response',[False,None,1,'true',{'accepted':True}])
def test_report_requires_exact_boolean_acceptance(response):
    reporter=CapacityReporter(lambda sample:response,lambda:Capacity(1,1),lambda:None)
    with pytest.raises(RuntimeError,match='registration rejected'):reporter.report_once()


def test_rejected_report_stops_before_aborting_even_if_abort_raises():
    stop=Event();events=[]
    class Tick:
        def wait(self,interval):return stop.is_set()
        def set(self):stop.set()
    def abort():
        assert stop.is_set() and reporter.failed.is_set()
        events.append('abort')
        raise RuntimeError('abort failed')
    reporter=CapacityReporter(lambda sample:'true',lambda:Capacity(1,1),abort)
    with pytest.raises(RuntimeError,match='abort failed'):reporter.run(Tick())
    assert events==['abort'] and stop.is_set() and reporter.failed.is_set()


def test_exact_true_report_is_accepted():
    samples=[]
    reporter=CapacityReporter(lambda sample:samples.append(sample) or True,lambda:Capacity(1,2,3),lambda:None)
    reporter.report_once()
    assert samples==[Capacity(1,2,3)] and not reporter.failed.is_set()
