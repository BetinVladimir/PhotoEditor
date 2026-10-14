"""Region build pins cover region orchestration and encoded presentation."""
from importlib import metadata
from pathlib import Path
import platform
import sys

import pytest

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.runtime.executor import OperationFailure
from photo_worker.operations.region import runtime_lock as runtime


def lock():
    return runtime.RegionRuntimeLock(schema_version=1,
        python_version='.'.join(map(str, sys.version_info[:3])),
        implementation=platform.python_implementation(), machine=platform.machine(),
        packages={name: metadata.version(name) for name in ('numpy', 'pydantic', 'rfc8785', 'Pillow')},
        components=runtime.installed_component_hashes())


def test_actual_region_lock_roundtrip_and_inventory(tmp_path):
    pinned = lock()
    path = tmp_path / 'region.lock.json'
    path.write_bytes(canonical_bytes(pinned))
    assert runtime.load_region_runtime_lock(path, canonical_hash(pinned)) == pinned
    assert {'region_adapter', 'region_presentation', 'region_adoption', 'region_runtime_lock',
        'render_authored', 'render_composition', 'pillow_imaging', 'pillow_imagingcms',
        'icc_registry'} <= pinned.components.keys()


@pytest.mark.parametrize('component', ['region_adapter', 'region_adoption', 'pillow_imagingcms'])
def test_region_rejects_changed_installed_component(monkeypatch, component):
    pinned = lock()
    changed = dict(pinned.components)
    changed[component] = '0' * 64
    monkeypatch.setattr(runtime, 'installed_component_hashes', lambda: changed)
    with pytest.raises(OperationFailure, match='RUNTIME_BYTES_MISMATCH'):
        runtime.verify_installed_runtime(pinned, canonical_hash(pinned))


@pytest.mark.parametrize('kind', ['symlink', 'hardlink', 'noncanonical', 'wrong_pin'])
def test_region_lock_rejects_untrusted_file_identity(tmp_path, kind):
    pinned = lock()
    path = tmp_path / 'region.lock.json'
    path.write_bytes(canonical_bytes(pinned))
    sha = canonical_hash(pinned)
    if kind == 'symlink':
        target = tmp_path / 'linked.json'
        target.symlink_to(path)
        path = target
    elif kind == 'hardlink':
        import os
        os.link(path, tmp_path / 'second.json')
    elif kind == 'noncanonical':
        path.write_bytes(b' ' + path.read_bytes())
    else:
        sha = '0' * 64
    with pytest.raises(OperationFailure, match='RUNTIME_LOCK_MISMATCH'):
        runtime.load_region_runtime_lock(path, sha)


def test_cli_verifies_actual_pin_without_claiming_qualification(tmp_path):
    import json
    from typer.testing import CliRunner
    from photo_worker.cli import app
    pinned = lock()
    path = tmp_path / 'region.lock.json'
    path.write_bytes(canonical_bytes(pinned))
    runner = CliRunner()
    result = runner.invoke(app, ['verify-region-runtime', str(path), canonical_hash(pinned)])
    assert result.exit_code == 0, result.output
    facts = json.loads(result.output)
    assert facts == dict(valid=True, sha256=canonical_hash(pinned),
        components=len(pinned.components), qualification='not_measured')
    invalid = runner.invoke(app, ['verify-region-runtime', str(path), '0' * 64])
    assert invalid.exit_code == 2
    assert 'Region runtime verification failed' in invalid.output
