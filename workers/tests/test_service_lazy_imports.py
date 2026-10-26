"""Worker classes do not load unrelated native operations at service import."""
import ast
from pathlib import Path
import subprocess
import sys
import json
from types import SimpleNamespace
import pytest


def test_public_factory_patch_points_forward_arguments_and_exceptions(monkeypatch):
    from photo_worker.runtime import service
    sentinel=object();calls=[]
    target=SimpleNamespace(asset_operations=lambda *args,**kwargs:(calls.append((args,kwargs)),sentinel)[1])
    monkeypatch.setattr('importlib.import_module',lambda name:target if name=='photo_worker.operations.asset.adapters' else pytest.fail('wrong factory module'))
    assert service.asset_operations('artifacts',icc_root='registry')is sentinel
    assert calls==[(('artifacts',),{'icc_root':'registry'})]
    error=ValueError('same instance')
    def fail(*args,**kwargs):raise error
    target.asset_operations=fail
    with pytest.raises(ValueError)as caught:service.asset_operations(None)
    assert caught.value is error
    monkeypatch.setattr(service,'asset_operations',lambda *a,**k:sentinel)
    assert service.asset_operations(None)is sentinel


def test_lazy_public_factory_names_remain_complete_and_callable():
    from photo_worker.runtime import service
    tree=ast.parse(Path(service.__file__).read_text())
    names=[n.targets[0].id for n in tree.body if isinstance(n,ast.Assign)and isinstance(n.value,ast.Call)and isinstance(n.value.func,ast.Name)and n.value.func.id=='_lazy_operation_factory']
    assert len(names)==52 and len(set(names))==52
    assert all(callable(getattr(service,name))for name in names)
    assert {'asset_operations','archive_operations','image_color_operations','image_local_operations','media_operations','quality_operations','pair_operations'}<=set(names)


def test_archive_service_and_worker_build_do_not_import_unrelated_native_dependencies():
    from photo_worker.runtime import service
    source=Path(service.__file__)
    program=r'''
import builtins,sys,importlib.util,tempfile,ssl
from pathlib import Path
from uuid import uuid4
sys.path.insert(0,sys.argv[1]);original=builtins.__import__;blocked={'torch','scipy','cv2','rawpy'}
def guard(name,*args,**kwargs):
 if name.split('.')[0]in blocked:raise AssertionError('UNRELATED_HEAVY_IMPORT')
 return original(name,*args,**kwargs)
builtins.__import__=guard
spec=importlib.util.spec_from_file_location('photo_worker.runtime.service',sys.argv[2]);m=importlib.util.module_from_spec(spec);sys.modules[spec.name]=m;spec.loader.exec_module(m)
assert not any(n.split('.')[0]in blocked for n in sys.modules)
with tempfile.TemporaryDirectory()as tmp:
 env=dict(PHOTO_WORKER_ID=str(uuid4()),PHOTO_WORKER_CLASS='archive',DATABASE_URL='postgres://fixture:fixture@example.test/db',AMQP_URL='amqps://fixture:fixture@example.test',PHOTO_SCRATCH_ROOT=str(Path(tmp).resolve()),GATEWAY_ORIGIN='https://localhost:8443',GATEWAY_CA_FILE='ca.pem',WORKER_CERT_FILE='worker.pem',WORKER_KEY_FILE='worker.key',WORKER_MAX_SCRATCH_BYTES='4096',WORKER_MAX_DOWNLOAD_BYTES='4096',WORKER_MAX_UPLOAD_BYTES='4096')
 config=m.WorkerConfig.from_env(env);m.client_tls=lambda *args:ssl.create_default_context()
 m.TaskDatabase.media_renderer_policy_ready=lambda self:True;m.TaskDatabase.task_result_fence_ready=lambda self:True
 database,transport=m.build_worker(config)
 assert [(o.name,o.resource_class,o.version)for o in transport.executor.operations.values()]==[('archive.build','archive','1')]
assert not any(n.split('.')[0]in blocked for n in sys.modules)
'''
    # The subprocess sees only explicitly selected package source, not ambient cwd.
    import photo_worker
    workers=Path(photo_worker.__file__).resolve().parent.parent
    result=subprocess.run([sys.executable,'-I','-B','-c',program,str(workers),str(source)],capture_output=True,text=True,timeout=30)
    assert result.returncode==0,result.stderr[-2000:]
