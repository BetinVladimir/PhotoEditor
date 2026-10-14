from uuid import uuid4

from photo_worker.v2.restoration_preview_runner import RestorationPreviewRunner
from photo_worker.v2.service import AdopterConfig
import pytest


class _Coordinator:
    def __init__(self):
        self.read = object()
        self.write = object()
        self.calls = []
        self.tenant, self.run, self.task = uuid4(), uuid4(), uuid4()

    def check_active(self):
        pass

    def _query(self, connection, tenant, sql, args):
        self.calls.append((tenant, sql, args))
        if 'receipt_tenant' in sql:
            return [(self.tenant,)] if args == (None,) else []
        if 'completion' in sql:
            return []
        return [(self.run, 'after', 'preview2560', self.task)]

    def adopt(self, tenant, run, side, variant):
        assert (tenant, run, side, variant) == (
            self.tenant, self.run, 'after', 'preview2560')
        return True


def test_restoration_preview_runner_advances_tenant_and_scope():
    coordinator = _Coordinator()
    runner = RestorationPreviewRunner(coordinator)
    assert runner.run_once()['adopted'] == 1
    assert runner.after_tenant == coordinator.tenant
    assert runner.tenant is None and runner.after_task is None
    assert runner.run_once()['adopted'] == 0
    assert runner.after_tenant is None
    assert any(call[0] == coordinator.tenant and 'pending_v28' in call[1]
               for call in coordinator.calls)


def test_restoration_preview_config_requires_explicit_flag_and_icc():
    env = {'DATABASE_URL': 'postgresql://user@localhost/photo',
           'S3_ENDPOINT': 'https://s3.example.test', 'S3_BUCKET': 'photos',
           'S3_ACCESS_KEY': 'key', 'S3_SECRET_KEY': 'secret'}
    assert AdopterConfig.from_env(env).restoration_preview_enabled is False
    env['PHOTO_ADOPTER_V28_RESTORATION_PREVIEW_ENABLED'] = 'yes'
    with pytest.raises(ValueError, match='enablement'):
        AdopterConfig.from_env(env)
    env['PHOTO_ADOPTER_V28_RESTORATION_PREVIEW_ENABLED'] = 'true'
    with pytest.raises(ValueError, match='ICC'):
        AdopterConfig.from_env(env)
