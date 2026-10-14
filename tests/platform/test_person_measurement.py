from hashlib import sha256
from types import SimpleNamespace
import sys
import pytest
from photo_worker.operations.mask import person_measurement as measurement
from photo_worker.runtime.executor import OperationFailure


def test_runtime_digest_tracks_actual_component_bytes(tmp_path,monkeypatch):
    path=tmp_path/'component';path.write_bytes(b'original')
    monkeypatch.setattr(measurement,'component_paths',lambda:{'component':path})
    monkeypatch.setattr(measurement.metadata,'version',lambda name:'1.0')
    monkeypatch.setitem(sys.modules,'torch',SimpleNamespace(__config__=SimpleNamespace(show=lambda:'build')))
    first=measurement.measure_runtime()
    assert first==measurement.measure_runtime()
    path.write_bytes(b'changed')
    assert first!=measurement.measure_runtime()


def test_cpu_device_digest_tracks_thread_configuration(monkeypatch):
    threads=[2]
    monkeypatch.setitem(sys.modules,'torch',SimpleNamespace(get_num_threads=lambda:threads[0],get_num_interop_threads=lambda:1))
    first=measurement.measure_device('cpu');threads[0]=4
    assert first!=measurement.measure_device('cpu')
    with pytest.raises(OperationFailure):measurement.measure_device('unknown')


def test_installed_person_provider_verifies_actual_weight_bytes(tmp_path,monkeypatch):
    lock='a'*64;model='lraspp_mobilenet_v3_large'
    folder=tmp_path/lock/model;folder.mkdir(parents=True)
    weight=folder/'weights.bin';weight.write_bytes(b'weights')
    entry=SimpleNamespace(id=model,files=[SimpleNamespace(path='weights.bin',bytes=7,sha256=sha256(b'weights').hexdigest())])
    pool=SimpleNamespace(root=tmp_path,lock=SimpleNamespace(lock_id=lock,models=[entry]))
    monkeypatch.setattr(measurement,'measure_runtime',lambda:'b'*64)
    monkeypatch.setattr(measurement,'measure_device',lambda backend:'c'*64)
    assert measurement.installed_person_measurements(pool,'cuda')==dict(model_lock_sha=lock,runtime_lock_sha='b'*64,device_profile_sha='c'*64,backend='cuda')
    weight.write_bytes(b'changed')
    with pytest.raises(OperationFailure) as error:measurement.installed_person_measurements(pool,'cuda')
    assert error.value.error.code=='MODEL_HASH_MISMATCH'


def test_component_discovery_accepts_stable_torchvision_native(tmp_path,monkeypatch):
    import photo_worker.operations.optical.runtime_lock as optical
    common=tmp_path/'common.so';common.write_bytes(b'common')
    monkeypatch.setattr(optical,'component_paths',lambda:{name:common for name in ('python','numpy_core','pydantic_core','canonical_impl')})
    for name,native in (('torch','core.so'),('torchvision','_C_stable.so'),('cv2','cv2.abi3.so')):
        folder=tmp_path/name;folder.mkdir();init=folder/'__init__.py';init.write_text('# component')
        (folder/native).write_bytes(b'native')
        module=SimpleNamespace(__file__=str(init))
        if name=='torch':
            module._C=SimpleNamespace(__file__=str(folder/native))
            lib=folder/'lib';lib.mkdir();(lib/'libtorch.so').write_bytes(b'torch')
        monkeypatch.setitem(sys.modules,name,module)
    paths=measurement.component_paths()
    assert paths['torchvision_native__C_stable.so'].name=='_C_stable.so'
    assert 'torchvision_python___init__.py' in paths
    assert 'torchvision_library__C_stable.so' in paths


def test_cuda_profile_includes_driver_and_denies_unavailable_driver(monkeypatch):
    from photo_worker.operations.mask import person_nvml
    props=SimpleNamespace(name='synthetic-gpu',total_memory=12*1024**3,multi_processor_count=28,uuid='synthetic-uuid')
    cudnn=SimpleNamespace(version=lambda:8900,deterministic=False,benchmark=False,allow_tf32=True)
    cuda=SimpleNamespace(is_available=lambda:True,current_device=lambda:0,get_device_properties=lambda index:props,get_device_capability=lambda index:(8,6))
    torch=SimpleNamespace(cuda=cuda,version=SimpleNamespace(cuda='12.1'),backends=SimpleNamespace(cudnn=cudnn,cuda=SimpleNamespace(matmul=SimpleNamespace(allow_tf32=False))))
    monkeypatch.setitem(sys.modules,'torch',torch)
    versions=dict(driver_version='550.54.14',nvml_version='12.550.54')
    monkeypatch.setattr(person_nvml,'measure_nvidia_driver',lambda:versions)
    first=measurement.measure_device('cuda');versions['driver_version']='550.90.07'
    assert first!=measurement.measure_device('cuda')
    def unavailable():raise OperationFailure('PERSON_DRIVER_MEASUREMENT_UNAVAILABLE')
    monkeypatch.setattr(person_nvml,'measure_nvidia_driver',unavailable)
    with pytest.raises(OperationFailure):measurement.measure_device('cuda')
