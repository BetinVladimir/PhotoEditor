from types import SimpleNamespace
import pytest
from photo_worker.operations.mask import person_nvml as nvml
from photo_worker.runtime.executor import OperationFailure

class Function:
    def __init__(self,callback):self.callback=callback
    def __call__(self,*args):return self.callback(*args)

def library(driver=b'550.54.14',init_status=0,query_status=0,shutdown_status=0):
    state=[]
    def init():state.append('init');return init_status
    def stop():state.append('shutdown');return shutdown_status
    def query(value):
        def run(buffer,size):
            assert size==80;buffer.value=value;return query_status
        return Function(run)
    return SimpleNamespace(nvmlInit_v2=Function(init),nvmlShutdown=Function(stop),nvmlSystemGetDriverVersion=query(driver),nvmlSystemGetNVMLVersion=query(b'12.550.54')),state

def test_nvml_versions_and_balanced_shutdown(monkeypatch):
    fake,state=library();monkeypatch.setattr(nvml,'load_nvml',lambda:fake)
    assert nvml.measure_nvidia_driver()==dict(driver_version='550.54.14',nvml_version='12.550.54')
    assert state==['init','shutdown']

@pytest.mark.parametrize('patch',[dict(init_status=1),dict(query_status=1),dict(shutdown_status=1),dict(driver=b''),dict(driver=b'\xff'),dict(driver=b'unsafe value')])
def test_nvml_failure_never_produces_driver_identity(monkeypatch,patch):
    fake,state=library(**patch);monkeypatch.setattr(nvml,'load_nvml',lambda:fake)
    with pytest.raises(OperationFailure) as error:nvml.measure_nvidia_driver()
    assert error.value.error.code=='PERSON_DRIVER_MEASUREMENT_UNAVAILABLE'
    assert state==(['init'] if patch.get('init_status') else ['init','shutdown'])
