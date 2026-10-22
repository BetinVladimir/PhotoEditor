"""Typed ordinary constructors and actual CLI forwarding; no physical qualification.

Host attestation is an explicit double; no model execution or broker loop occurs.
The independent physical result/adopter tail remains a separate scenario.
"""
from pathlib import Path
from types import SimpleNamespace
import importlib
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.runtime import semantic_scene_config as worker
from photo_worker.v2 import semantic_scene_config as adoption
from photo_worker.operations.semantic.scene_runtime import SemanticSceneRuntime,component_paths
from photo_worker.models.lock import ModelLock

@pytest.fixture
def installation(monkeypatch,tmp_path):
    # An empty model inventory is legitimate at this constructor boundary; task
    # model/qualification admission is deliberately not claimed by this fixture.
    payload=dict(schema_version=1,models=[])
    model=ModelLock(**payload,lock_id=canonical_hash(payload))
    path=tmp_path/'models.json';path.write_bytes(canonical_bytes(model))
    runtime=SemanticSceneRuntime(schema_version=1,algorithm='farneback_depth_plane_v1',
        python_version='fixture',implementation='CPython',machine='fixture',
        packages={n:'fixture' for n in ('numpy','pydantic','rfc8785','opencv-python-headless')},
        components={n:'a'*64 for n in component_paths()})
    lock=tmp_path/'runtime.json';lock.write_bytes(canonical_bytes(runtime))
    env=dict(PHOTO_SEMANTIC_TRANSFER_ENABLED='true',
        PHOTO_SEMANTIC_TRANSFER_MODEL_LOCK_SHA=model.lock_id,
        PHOTO_SEMANTIC_TRANSFER_RUNTIME_LOCK_SHA=canonical_hash(runtime),
        PHOTO_SEMANTIC_TRANSFER_DEVICE_PROFILE_SHA='c'*64,
        PHOTO_SEMANTIC_TRANSFER_DATASET_SHA='d'*64,
        PHOTO_SEMANTIC_TRANSFER_RUNTIME_LOCK=str(lock),
        PHOTO_SEMANTIC_TRANSFER_ADOPTION_RAM_BYTES=str(8*1024**3),
        S3_ENDPOINT='http://127.0.0.1:9000',S3_BUCKET='scene-fixture',
        S3_ACCESS_KEY='fixture-access',S3_SECRET_KEY='fixture-secret')
    observations=[]
    def attestation(raw,expected_sha,*,check_active):
        check_active();actual=SemanticSceneRuntime.model_validate_json(raw)
        assert actual==runtime and canonical_hash(actual)==expected_sha
        observations.append(expected_sha);check_active()
    monkeypatch.setattr(worker,'verify_scene_runtime',attestation)
    config=SimpleNamespace(backend='cuda',resource_class='gpu',development=True,
        model_root=tmp_path,model_lock=path,database_url='postgresql://fixture.invalid/photo',
        icc_root=Path(__file__).resolve().parents[2]/'resources/icc',
        layer_scratch_root=tmp_path,layer_scratch_bytes=1024**3,capacity_policy=None)
    return config,env,observations,path


def test_actual_typed_worker_and_adopter_constructors(installation):
    config,env,observations,_=installation
    factory=worker.semantic_scene_from_environment(config,env.get)
    verifier,ram=adoption.semantic_scene_adopter_from_environment(config,env.get)
    assert callable(factory) and ram==8*1024**3 and len(observations)==2
    assert verifier.measurements['runtime_lock_sha']==env['PHOTO_SEMANTIC_TRANSFER_RUNTIME_LOCK_SHA']
    assert verifier.scratch_root==config.layer_scratch_root
    with pytest.raises(ValueError,match='RESULT_SCOPE_REQUIRED'):verifier()


def test_actual_constructor_rejects_changed_model_lock(installation):
    config,env,_,path=installation
    path.write_bytes(b'{"schema_version":1,"models":[],"lock_id":"'+b'f'*64+b'"}')
    with pytest.raises(ValueError):worker.semantic_scene_from_environment(config,env.get)


def test_actual_constructor_rejects_credentialed_object_endpoint(installation):
    config,env,_,_=installation;env['S3_ENDPOINT']='http://user:secret@127.0.0.1:9000'
    with pytest.raises(ValueError,match='OBJECT_CONFIGURATION_INVALID'):
        worker.semantic_scene_from_environment(config,env.get)


def test_actual_cli_passes_configured_semantic_installations(monkeypatch,installation):
    from photo_worker import cli
    from photo_worker.runtime import service
    from photo_worker.v2 import service as adopter_service
    config,env,observations,_=installation
    for key,value in env.items():monkeypatch.setenv(key,value)
    monkeypatch.setattr(service.WorkerConfig,'from_env',lambda:config)
    monkeypatch.setattr(adopter_service.AdopterConfig,'from_env',lambda:config)
    # Disable unrelated factories only. Actual semantic constructors, typed file
    # reads and object-client configuration above are not monkeypatched.
    factories={
      'runtime.semantic_assembly_config':'semantic_assembly_from_environment',
      'runtime.exposure_config':'local_exposure_from_environment',
      'runtime.preview_config':'exposure_preview_from_environment',
      'runtime.shown_exposure_config':'shown_exposure_from_environment',
      'runtime.removal_preview_config':'removal_preview_from_environment',
      'runtime.background_cleanup_preview_config':'background_cleanup_preview_from_environment',
      'runtime.retouch_preview_config':'retouch_preview_from_environment',
      'runtime.shown_removal_config':'shown_removal_from_environment',
      'runtime.shown_background_cleanup_config':'shown_background_cleanup_from_environment',
      'runtime.shown_retouch_config':'shown_retouch_from_environment',
      'runtime.donor_config':'donor_installation_from_environment',
      'runtime.negative_config':'negative_installation_from_environment',
      'runtime.relight_config':'relight_installation_from_environment',
      'runtime.local_portrait_config':'local_portrait_installation_from_environment',
      'runtime.reflection_config':'reflection_installation_from_environment',
      'runtime.glare_config':'glare_donor_installation_from_environment',
      'runtime.perspective_config':'perspective_installation_from_environment',
      'runtime.perspective_lines_config':'perspective_lines_installation_from_environment',
      'runtime.multi_config':'multi_installation_from_environment',
      'runtime.learning_config':'learning_evaluation_from_environment',
      'runtime.native_render_config':'native_render_from_environment',
      'v2.portrait_donor_adopter_config':'donor_adopter_from_environment',
      'v2.negative_adopter_config':'negative_adopter_from_environment',
      'v2.relight_adopter_config':'relight_adopter_from_environment',
      'v2.perspective_adopter_config':'perspective_adopter_from_environment',
      'v2.perspective_lines_adopter_config':'perspective_lines_adopter_from_environment',
      'v2.multi_adopter_config':'multi_adopter_from_environment',
      'v2.interchange_import_adopter_config':'import_adopter_operator_from_environment'}
    for module,name in factories.items():
        monkeypatch.setattr(importlib.import_module('photo_worker.'+module),name,lambda *a,**kw:None)
    calls=[]
    # Execute the actual CLI -> run_worker forwarding through build_worker.
    # GPU budget hardware and broker/model construction are explicit boundaries.
    monkeypatch.setattr(service,'_install_cuda_budget',lambda actual:None)
    monkeypatch.setattr(service,'_install_metal_budget',lambda actual:None)
    def build(actual,**options):
        calls.append(('worker',actual,options));return object(),object()
    monkeypatch.setattr(service,'build_worker',build)
    monkeypatch.setattr(service,'run_built_worker',lambda *a,**kw:None)
    monkeypatch.setattr(adopter_service,'run_adopter',lambda actual,stop,**options:calls.append(('adopter',actual,options)))
    import signal
    monkeypatch.setattr(signal,'signal',lambda *a:None)
    cli.run();cli.v2_adopter()
    assert [kind for kind,_,_ in calls]==['worker','adopter']
    assert all(actual is config for _,actual,_ in calls)
    assert set(calls[0][2])=={'semantic_artifact_installation_factory'}
    assert callable(calls[0][2]['semantic_artifact_installation_factory'])
    assert set(calls[1][2])=={'semantic_scene_verifier','semantic_ram_bytes'}
    assert calls[1][2]['semantic_ram_bytes']==8*1024**3 and len(observations)==2
