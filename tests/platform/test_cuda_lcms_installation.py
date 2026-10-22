"""Authored build installer contracts. No Docker/network/native loading here."""
import base64
import hashlib
import importlib.util
from pathlib import Path
from types import SimpleNamespace
import pytest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('cuda_lcms_installation', ROOT / 'deploy/images/worker/install_pillow_lcms.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class Member:
    def __init__(self, name, data):
        self.name = name
        self.size = len(data)
        self.hash = SimpleNamespace(mode='sha256', value=base64.urlsafe_b64encode(hashlib.sha256(data).digest()).decode().rstrip('='))

    def __str__(self):
        return self.name


def distribution(tmp_path, names=('pillow.libs/liblcms2-locked.so.2',)):
    data = b'fixture only; never load as a native library'
    members = [Member(name, data) for name in names]
    for member in members:
        target = tmp_path / member.name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
    return SimpleNamespace(files=members, version='locked-fixture', locate_file=lambda member: tmp_path / member.name)


def test_installs_only_record_verified_wheel_member_and_records_actual_probe(tmp_path):
    dist = distribution(tmp_path / 'site')
    called = []
    def probe(path):
        called.append(path)
        return 2180  # Fixture evidence, not an assertion about the locked wheel.
    manifest = module.install(tmp_path / 'native', dist, probe)
    assert called == [(tmp_path / 'site' / dist.files[0].name).resolve()]
    assert (tmp_path / 'native/liblcms2.so').resolve() == called[0]
    assert manifest['lcms_encoded_version'] == 2180
    assert manifest['library_sha256'] == hashlib.sha256(called[0].read_bytes()).hexdigest()
    assert manifest['provider'] == 'locked_pillow_wheel'


@pytest.mark.parametrize('names', [(), ('PIL/.dylibs/liblcms2.dylib',), ('pillow.libs/liblcms2-a.so.2', 'pillow.libs/liblcms2-b.so.2')])
def test_missing_or_ambiguous_vendor_library_refused(tmp_path, names):
    with pytest.raises(ValueError, match='MEMBER_REQUIRED'):
        module.wheel_library(distribution(tmp_path, names))


def test_corrupt_library_refused_before_probe_or_install(tmp_path):
    dist = distribution(tmp_path / 'site')
    path = dist.locate_file(dist.files[0])
    data = path.read_bytes()
    path.write_bytes(b'x' + data[1:])
    with pytest.raises(ValueError, match='RECORD_MISMATCH'):
        module.install(tmp_path / 'native', dist, lambda _: pytest.fail('must not load unverified library'))
    assert not (tmp_path / 'native').exists()


def test_absent_record_hash_and_native_abi_refusal_do_not_install(tmp_path):
    dist = distribution(tmp_path / 'site')
    dist.files[0].hash = None
    with pytest.raises(ValueError, match='RECORD_REQUIRED'):
        module.wheel_library(dist)
    dist = distribution(tmp_path / 'site')
    def reject(_):
        raise OSError('missing LCMS symbol')
    with pytest.raises(OSError, match='missing LCMS symbol'):
        module.install(tmp_path / 'native', dist, reject)
    assert not (tmp_path / 'native').exists()


def test_cuda_recipe_pins_vendor_library_without_unhashed_system_packages():
    recipe = (ROOT / 'deploy/images/worker/Dockerfile.cuda').read_text()
    assert 'RUN workers/.venv/bin/python /opt/photo/install_pillow_lcms.py' in recipe
    assert 'ENV LCMS2_LIBRARY=/opt/photo/native/liblcms2.so' in recipe
    assert 'apt-get' not in recipe
    assert 'uv sync --frozen' in recipe and '--extra media' in recipe
