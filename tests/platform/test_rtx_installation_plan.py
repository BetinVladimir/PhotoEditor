"""Operator wiring scenarios only; no Docker/build/network/qualification."""
import importlib.util
import json
from pathlib import Path
import pytest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('rtx_installation', ROOT / 'deploy/rtx_installation.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


def fixture(tmp_path):
    value = json.loads((ROOT / 'deploy/env/rtx-images.lock.example.json').read_bytes())
    for name in ('uv_image', 'cuda_runtime_image', 'cuda_python_base_image', 'worker_image'):
        value[name] = 'registry.example/' + name + '@sha256:' + 'a' * 64
    path = tmp_path / 'images.json'
    path.write_text(json.dumps(value))
    return path, value


@pytest.mark.parametrize('phase,key', [('base', 'cuda_runtime_image'), ('worker', 'cuda_python_base_image')])
def test_build_plan_immutable_inputs_and_provenance(tmp_path, phase, key):
    path, value = fixture(tmp_path)
    args, env = module.command(module.read_lock(path), phase,
        output_tag='registry.example/photo:test', metadata_file=tmp_path / 'build-metadata.json')
    assert env == {}
    assert args[:3] == ['docker', 'buildx', 'build']
    assert '--push' in args and '--provenance=mode=max' in args
    assert args[args.index('--platform') + 1] == 'linux/amd64'
    assert key.upper() + '=' + value[key] in args
    assert 'UV_IMAGE=' + value['uv_image'] in args


@pytest.mark.parametrize('change', [{'worker_image': 'photo:latest'}, {'cuda_runtime_image': 'photo@sha256:abc'},
    {'python_version': '3.11.13'}, {'platform': 'linux/arm64'}, {'cuda_major': True}, {'extra': 1}])
def test_invalid_or_mutable_contract_refused(tmp_path, change):
    path, value = fixture(tmp_path)
    path.write_text(json.dumps({**value, **change}))
    with pytest.raises(ValueError):
        module.read_lock(path)


def test_unknown_pin_is_not_invented_or_fetched(tmp_path):
    path, value = fixture(tmp_path)
    value['cuda_python_base_image'] = None
    path.write_text(json.dumps(value))
    with pytest.raises(ValueError, match='unknown'):
        module.command(module.read_lock(path), 'worker', output_tag='photo:test', metadata_file=tmp_path / 'metadata.json')


def test_start_uses_exact_locked_worker_image_and_existing_env(tmp_path):
    path, value = fixture(tmp_path)
    environment = tmp_path / 'operator.env'
    environment.write_text('PHOTO_RTX_WORKER_IMAGE=mutable:latest\n')
    args, env = module.command(module.read_lock(path), 'start', env_file=environment)
    assert args[-2:] == ['up', '--detach']
    assert env == {'PHOTO_RTX_WORKER_IMAGE': value['worker_image']}
    assert '--env-file' in args


def test_duplicate_lock_keys_are_refused(tmp_path):
    path, _ = fixture(tmp_path)
    path.write_text('{"schema_version":1,"schema_version":1}')
    with pytest.raises(ValueError, match='Duplicate'):
        module.read_lock(path)


@pytest.mark.parametrize('phase', ['base', 'worker'])
def test_output_tag_and_metadata_are_explicit(tmp_path, phase):
    path, _ = fixture(tmp_path)
    with pytest.raises(ValueError):
        module.command(module.read_lock(path), phase, output_tag='--injected', metadata_file=tmp_path / 'metadata.json')
    with pytest.raises(ValueError):
        module.command(module.read_lock(path), phase, output_tag='photo:test', metadata_file='relative.json')


def test_oversized_lock_refused_before_json_parsing(tmp_path):
    path=tmp_path/'oversized.json'
    path.write_bytes(b' '*16385)
    with pytest.raises(ValueError,match='exceeds limit'):
        module.read_lock(path)


def test_exact_boundary_lock_is_not_classified_as_oversized(tmp_path):
    path,value=fixture(tmp_path)
    raw=json.dumps(value).encode()
    path.write_bytes(raw+b' '*(16384-len(raw)))
    assert module.read_lock(path)==value
