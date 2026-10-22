from uuid import UUID
import pytest
from photo_worker.v2.lens_preview_runner import LensPreviewRunner
from photo_worker.v2.lens_preview_maintenance import LensPreviewReceiptMaintenance
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.service import AdopterConfig


class Coordinator:
    def __init__(self):
        self.read, self.write = object(), object()
        self.tenant, self.run = UUID(int=1), UUID(int=2)
        self.calls = []
        self.pending = [(self.run,)]
        self.outcome = 'released'

    def check_active(self):
        pass

    def _query(self, connection, tenant, sql, args, *, one=False):
        self.calls.append((connection, tenant, sql, args))
        if 'next_v25_lens_preview_tenant' in sql:
            assert tenant == ''
            return [(self.tenant,)] if args == (None,) else []
        assert tenant == self.tenant
        if 'pending_v25_lens_preview_lifecycle' in sql:
            return self.pending
        if 'maintain_v25_lens_preview' in sql:
            assert connection is self.write and args == (self.tenant, self.run)
            return (self.outcome,)
        return []


def test_lens_runtime_mount_drives_lifecycle_and_independent_phase_cursors():
    coordinator = Coordinator()
    runner = LensPreviewRunner(coordinator)
    assert runner.run_once()['released'] == 1
    assert runner.positions['lifecycle'] == [coordinator.tenant, None, None]
    assert runner.positions['receipts'] == [coordinator.tenant, None, None]
    assert runner.positions['completion'] == [coordinator.tenant, None, None]
    assert runner.run_once()['released'] == 0
    assert all(position == [None, None, None]
               for position in runner.positions.values())
    assert any('maintain_v25_lens_preview' in call[2]
               for call in coordinator.calls)


@pytest.mark.parametrize('outcome', ['none', 'cancel_requested', 'failed', 'released'])
def test_lens_lifecycle_counts_only_committed_typed_transitions(outcome):
    coordinator = Coordinator()
    coordinator.outcome = outcome
    cursor, counts = LensPreviewReceiptMaintenance(coordinator).process_lifecycle_page(
        coordinator.tenant)
    assert cursor is None
    assert sum(counts.values()) == int(outcome != 'none')
    if outcome != 'none':
        assert counts[outcome] == 1


def test_lens_lifecycle_validates_entire_page_before_mutating_any_run():
    coordinator = Coordinator()
    coordinator.pending = [(coordinator.run,), (coordinator.run,)]
    with pytest.raises(AdoptionInvalid, match='PENDING_INVALID'):
        LensPreviewReceiptMaintenance(coordinator).process_lifecycle_page(
            coordinator.tenant)
    assert not any('maintain_v25_lens_preview' in call[2]
                   for call in coordinator.calls)


def test_lens_lifecycle_failed_tenant_does_not_starve_receipt_phases():
    coordinator = Coordinator()
    coordinator.outcome = 'unexpected'
    runner = LensPreviewRunner(coordinator)
    counts = runner.run_once()
    assert counts['retry'] == 1 and counts['released'] == 0
    assert any('pending_v25_lens_preview_receipts' in call[2]
               for call in coordinator.calls)
    assert any('pending_v25_lens_preview_completion' in call[2]
               for call in coordinator.calls)


def test_lens_preview_enablement_requires_explicit_flag_and_qualified_icc():
    env = {'DATABASE_URL': 'postgresql://user@localhost/photo',
           'S3_ENDPOINT': 'https://s3.example.test', 'S3_BUCKET': 'photos',
           'S3_ACCESS_KEY': 'key', 'S3_SECRET_KEY': 'secret'}
    assert AdopterConfig.from_env(env).lens_preview_enabled is False
    env['PHOTO_ADOPTER_V25_LENS_PREVIEW_ENABLED'] = 'yes'
    with pytest.raises(ValueError, match='enablement'):
        AdopterConfig.from_env(env)
    env['PHOTO_ADOPTER_V25_LENS_PREVIEW_ENABLED'] = 'true'
    with pytest.raises(ValueError, match='ICC'):
        AdopterConfig.from_env(env)
